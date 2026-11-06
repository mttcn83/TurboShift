// Package benchmark imports MangoHud logs, stores run summaries locally and
// compares configurations with an exact permutation test. It never produces
// numbers that were not measured.
package benchmark

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mttcn83/turboshift/internal/stats"
)

// Parsing limits keep a corrupt or hostile file from exhausting memory.
const (
	maxFrames   = 5_000_000
	maxLineSize = 1 << 20
)

// SysInfo is the metadata MangoHud writes before the per-frame rows.
type SysInfo struct {
	OS, CPU, GPU, Kernel, Driver, CPUSched string
	RAMKB                                  uint64
}

// Frames is the parsed per-frame data. Slices are index-aligned; optional
// columns are nil when absent from the log.
type Frames struct {
	Info      SysInfo
	FPS       []float64
	FrameMs   []float64 // frametime, milliseconds (verified from MangoHud source)
	ElapsedS  []float64 // seconds since logging started (log is in nanoseconds)
	CPULoad   []float64
	GPULoad   []float64
	CPUTempC  []float64
	GPUTempC  []float64
	GPUPowerW []float64
	CPUPowerW []float64
}

var ErrNotMangoHud = errors.New("not a MangoHud log (no 'fps,frametime,...' header row)")

// ParseMangoHud reads a full MangoHud CSV log. It supports both the plain
// 3-line header layout and the log_versioning layout, by locating the
// per-frame header row rather than counting lines.
func ParseMangoHud(r io.Reader) (*Frames, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), maxLineSize)

	var info SysInfo
	var cols map[string]int
	var prev, prevprev string
	lineNo := 0
	f := &Frames{}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		lineNo++
		if line == "" {
			continue
		}
		if cols == nil {
			if strings.HasPrefix(line, "fps,frametime") {
				cols = map[string]int{}
				for i, c := range strings.Split(line, ",") {
					cols[strings.TrimSpace(c)] = i
				}
				// The two lines before the frame header are "os,cpu,..." and its values.
				if strings.HasPrefix(prevprev, "os,cpu") {
					info = parseSysInfo(strings.Split(prevprev, ","), splitCSVLoose(prev, len(strings.Split(prevprev, ","))))
				}
				continue
			}
			if strings.HasPrefix(line, "---") {
				continue // log_versioning section separators
			}
			prevprev, prev = prev, line
			if lineNo > 50 {
				return nil, ErrNotMangoHud
			}
			continue
		}
		if len(f.FPS) >= maxFrames {
			return nil, fmt.Errorf("log has more than %d frames; trim it first", maxFrames)
		}
		fields := strings.Split(line, ",")
		if len(fields) != len(cols) {
			continue // torn or foreign row: a partial line must never count as a frame
		}
		get := func(name string) (float64, bool) {
			i, ok := cols[name]
			if !ok || i >= len(fields) {
				return 0, false
			}
			v, err := strconv.ParseFloat(strings.TrimSpace(fields[i]), 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return 0, false
			}
			return v, true
		}
		fps, ok1 := get("fps")
		ft, ok2 := get("frametime")
		if !ok1 || !ok2 || ft <= 0 {
			continue // a torn last line or an idle/paused frame: skip, never invent
		}
		f.FPS = append(f.FPS, fps)
		f.FrameMs = append(f.FrameMs, ft)
		if el, ok := get("elapsed"); ok {
			f.ElapsedS = append(f.ElapsedS, el/1e9)
		} else {
			// Fallback: accumulate frametimes.
			var last float64
			if n := len(f.ElapsedS); n > 0 {
				last = f.ElapsedS[n-1]
			}
			f.ElapsedS = append(f.ElapsedS, last+ft/1000)
		}
		appendOpt(&f.CPULoad, get, "cpu_load")
		appendOpt(&f.GPULoad, get, "gpu_load")
		appendOpt(&f.CPUTempC, get, "cpu_temp")
		appendOpt(&f.GPUTempC, get, "gpu_temp")
		appendOpt(&f.GPUPowerW, get, "gpu_power")
		appendOpt(&f.CPUPowerW, get, "cpu_power")
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading log: %w", err)
	}
	if cols == nil {
		return nil, ErrNotMangoHud
	}
	if len(f.FPS) == 0 {
		return nil, errors.New("the log has a header but no usable frames")
	}
	f.Info = info
	return f, nil
}

func appendOpt(dst *[]float64, get func(string) (float64, bool), name string) {
	if v, ok := get(name); ok {
		*dst = append(*dst, v)
	}
}

func splitCSVLoose(s string, n int) []string {
	p := strings.Split(s, ",")
	if len(p) > n && n > 1 { // a comma inside the CPU/GPU name: rejoin the middle
		extra := len(p) - n
		mid := strings.Join(p[1:2+extra], ",")
		p = append([]string{p[0], mid}, p[2+extra:]...)
	}
	return p
}

func parseSysInfo(hdr, val []string) SysInfo {
	var s SysInfo
	for i, h := range hdr {
		if i >= len(val) {
			break
		}
		v := strings.TrimSpace(val[i])
		switch strings.TrimSpace(h) {
		case "os":
			s.OS = v
		case "cpu":
			s.CPU = v
		case "gpu":
			s.GPU = v
		case "kernel":
			s.Kernel = v
		case "driver":
			s.Driver = v
		case "cpuscheduler":
			s.CPUSched = v
		case "ram":
			s.RAMKB, _ = strconv.ParseUint(v, 10, 64)
		}
	}
	return s
}

var logTimeRe = regexp.MustCompile(`_(\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2})(?:_summary)?\.csv$`)

// LogStartTime extracts the start time encoded in MangoHud's file name
// (<program>_YYYY-MM-DD_HH-MM-SS.csv), in local time.
func LogStartTime(name string) (time.Time, bool) {
	m := logTimeRe.FindStringSubmatch(name)
	if m == nil {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02_15-04-05", m[1], time.Local)
	return t, err == nil
}

// IsMangoHudLogName reports whether a file name looks like a full MangoHud
// log (not a *_summary.csv).
func IsMangoHudLogName(name string) bool {
	return logTimeRe.MatchString(name) && !strings.HasSuffix(name, "_summary.csv")
}

// percentileFPS is the "N% low": the FPS equivalent of the (100-N)th
// percentile frame time, i.e. the frame rate you drop to in your worst N% of
// frames.
func percentileFPS(frameMs []float64, lowPct float64) float64 {
	return 1000 / stats.Percentile(frameMs, 100-lowPct)
}
