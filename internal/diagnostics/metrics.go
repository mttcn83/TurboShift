package diagnostics

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/platform"
)

// GPUSample is live GPU telemetry. Available=false carries the reason, e.g. a
// sleeping dGPU that we deliberately did not wake.
type GPUSample struct {
	Name      string
	Available bool
	Note      string
	LoadPct   float64
	VRAMUsed  float64 // MiB
	VRAMTotal float64 // MiB
	TempC     float64
	PowerW    float64
}

// Metrics is one live sample. Rates need a previous sample, so the first
// Sample call after NewSampler returns zero rates (HasRates=false).
type Metrics struct {
	HasRates    bool
	CPUPct      float64
	CPUMHz      float64 // average over cores, 0 if unknown
	TempC       float64 // 0 if unknown
	MemUsedPct  float64
	SwapUsedPct float64
	NetRxBps    float64
	NetTxBps    float64
	NetIface    string
	GPUs        []GPUSample
}

// Sampler reads /proc and /sys cheaply (a few small files per tick). It holds
// the previous counters; it is not safe for concurrent use.
type Sampler struct {
	FS  platform.FS
	Run platform.Runner
	// GPUs and NetIface come from the detection report.
	GPUs     []detect.GPU
	NetIface string

	prevAt              time.Time
	prevIdle, prevTotal uint64
	prevRx, prevTx      uint64
	havePrev            bool
}

// ParseProcStat returns idle and total jiffies from the aggregate "cpu" line.
func ParseProcStat(s string) (idle, total uint64, ok bool) {
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var v [10]uint64
		for i := 1; i < len(f) && i <= 10; i++ {
			v[i-1], _ = strconv.ParseUint(f[i], 10, 64)
		}
		// user nice system idle iowait irq softirq steal guest guest_nice
		// guest time is already counted inside user/nice, so exclude it.
		for i := 0; i < 8; i++ {
			total += v[i]
		}
		return v[3] + v[4], total, true
	}
	return 0, 0, false
}

// ParseNetDev returns rx/tx byte counters per interface from /proc/net/dev.
func ParseNetDev(s string) map[string][2]uint64 {
	out := map[string][2]uint64{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(rest)
		if len(f) < 9 {
			continue
		}
		rx, _ := strconv.ParseUint(f[0], 10, 64)
		tx, _ := strconv.ParseUint(f[8], 10, 64)
		out[strings.TrimSpace(name)] = [2]uint64{rx, tx}
	}
	return out
}

func (sm *Sampler) Sample(ctx context.Context, now time.Time) Metrics {
	var m Metrics

	if b, err := sm.FS.ReadFile("/proc/stat"); err == nil {
		if idle, total, ok := ParseProcStat(string(b)); ok {
			if sm.havePrev && total > sm.prevTotal {
				dt := float64(total - sm.prevTotal)
				m.CPUPct = (1 - float64(idle-sm.prevIdle)/dt) * 100
				m.HasRates = true
			}
			sm.prevIdle, sm.prevTotal = idle, total
		}
	}
	if b, err := sm.FS.ReadFile("/proc/meminfo"); err == nil {
		kv := map[string]uint64{}
		for _, line := range strings.Split(string(b), "\n") {
			k, rest, ok := strings.Cut(line, ":")
			if f := strings.Fields(rest); ok && len(f) > 0 {
				kv[k], _ = strconv.ParseUint(f[0], 10, 64)
			}
		}
		if t := kv["MemTotal"]; t > 0 {
			m.MemUsedPct = float64(t-kv["MemAvailable"]) / float64(t) * 100
		}
		if t := kv["SwapTotal"]; t > 0 {
			m.SwapUsedPct = float64(t-kv["SwapFree"]) / float64(t) * 100
		}
	}

	// Average core clock and package temperature, via the same readers as detection.
	m.CPUMHz, m.TempC = sm.cpuClockAndTemp()

	if sm.NetIface != "" {
		m.NetIface = sm.NetIface
		if b, err := sm.FS.ReadFile("/proc/net/dev"); err == nil {
			if c, ok := ParseNetDev(string(b))[sm.NetIface]; ok {
				if sm.havePrev && now.After(sm.prevAt) && c[0] >= sm.prevRx && c[1] >= sm.prevTx {
					dt := now.Sub(sm.prevAt).Seconds()
					m.NetRxBps = float64(c[0]-sm.prevRx) / dt
					m.NetTxBps = float64(c[1]-sm.prevTx) / dt
				}
				sm.prevRx, sm.prevTx = c[0], c[1]
			}
		}
	}
	sm.prevAt, sm.havePrev = now, true

	for _, g := range sm.GPUs {
		m.GPUs = append(m.GPUs, sm.sampleGPU(ctx, g))
	}
	return m
}

func (sm *Sampler) cpuClockAndTemp() (mhz, temp float64) {
	var sum float64
	var n int
	ents, _ := sm.FS.ReadDir("/sys/devices/system/cpu")
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, "cpu") || len(name) < 4 || name[3] < '0' || name[3] > '9' {
			continue
		}
		if v, ok := platform.ReadTrim(sm.FS, "/sys/devices/system/cpu/"+name+"/cpufreq/scaling_cur_freq"); ok {
			if k, err := strconv.ParseFloat(v, 64); err == nil {
				sum += k / 1000
				n++
			}
		}
	}
	if n > 0 {
		mhz = sum / float64(n)
	}
	hw, _ := sm.FS.ReadDir("/sys/class/hwmon")
	for _, d := range hw {
		base := "/sys/class/hwmon/" + d.Name()
		name, _ := platform.ReadTrim(sm.FS, base+"/name")
		if name == "coretemp" || name == "k10temp" || name == "zenpower" {
			if v, ok := platform.ReadTrim(sm.FS, base+"/temp1_input"); ok {
				if x, err := strconv.ParseFloat(v, 64); err == nil {
					temp = x / 1000
					break
				}
			}
		}
	}
	return
}

func (sm *Sampler) sampleGPU(ctx context.Context, g detect.GPU) GPUSample {
	s := GPUSample{Name: g.Name}
	if s.Name == "" {
		s.Name = g.Vendor + " " + g.PCIID
	}
	switch g.Vendor {
	case "nvidia":
		// Querying nvidia-smi while the GPU is runtime-suspended wakes it and
		// burns battery. Only look when it is already awake.
		if st, ok := platform.ReadTrim(sm.FS, "/sys/bus/pci/devices/"+g.Slot+"/power/runtime_status"); ok && st != "active" {
			s.Note = "asleep (not woken, to save power)"
			return s
		}
		if g.Driver != "nvidia" || !sm.Run.Has("nvidia-smi") {
			s.Note = "nvidia-smi unavailable"
			return s
		}
		res, err := sm.Run.Run(ctx, "nvidia-smi", "--query-gpu=utilization.gpu,memory.used,memory.total,temperature.gpu,power.draw",
			"--format=csv,noheader,nounits")
		if err != nil {
			s.Note = "nvidia-smi failed"
			return s
		}
		return ParseNvidiaSMI(s, res.Stdout)
	case "amd":
		if v, ok := platform.ReadTrim(sm.FS, "/sys/bus/pci/devices/"+g.Slot+"/gpu_busy_percent"); ok {
			if x, err := strconv.ParseFloat(v, 64); err == nil {
				s.Available, s.LoadPct = true, x
				return s
			}
		}
		s.Note = "no load counter exposed"
	default:
		s.Note = "load not exposed without root"
	}
	return s
}

// ParseNvidiaSMI parses one CSV line (first GPU) of the query above.
func ParseNvidiaSMI(s GPUSample, out string) GPUSample {
	line, _, _ := strings.Cut(strings.TrimSpace(out), "\n")
	f := strings.Split(line, ",")
	if len(f) < 5 {
		s.Note = "unexpected nvidia-smi output"
		return s
	}
	num := func(i int) float64 { v, _ := strconv.ParseFloat(strings.TrimSpace(f[i]), 64); return v }
	s.Available = true
	s.LoadPct, s.VRAMUsed, s.VRAMTotal, s.TempC, s.PowerW = num(0), num(1), num(2), num(3), num(4)
	return s
}

// ---- top processes ------------------------------------------------------

type Proc struct {
	PID    int
	Name   string
	CPUPct float64 // of one core
	RSSMiB float64
}

// clockTicks is USER_HZ, which is 100 on every mainstream Linux build.
const clockTicks = 100.0

func readProcTimes(fs platform.FS) map[int]struct {
	name  string
	ticks uint64
} {
	out := map[int]struct {
		name  string
		ticks uint64
	}{}
	ents, err := fs.ReadDir("/proc")
	if err != nil {
		return out
	}
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := fs.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue // process exited, or hidden: not an error
		}
		name, ticks, ok := parseStat(string(b))
		if ok {
			out[pid] = struct {
				name  string
				ticks uint64
			}{name, ticks}
		}
	}
	return out
}

// parseStat extracts comm and utime+stime. comm may contain spaces and
// parentheses, so split on the *last* ')'.
func parseStat(s string) (string, uint64, bool) {
	l, r := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
	if l < 0 || r < l {
		return "", 0, false
	}
	f := strings.Fields(s[r+1:])
	if len(f) < 13 { // state is f[0]; utime=f[11], stime=f[12]
		return "", 0, false
	}
	u, e1 := strconv.ParseUint(f[11], 10, 64)
	st, e2 := strconv.ParseUint(f[12], 10, 64)
	if e1 != nil || e2 != nil {
		return "", 0, false
	}
	return s[l+1 : r], u + st, true
}

// TopProcesses measures CPU use over `window` and returns the heaviest n
// processes by CPU, then fills RSS for them. Processes owned by other users
// that cannot be read are simply absent (permission limits are not "idle").
func TopProcesses(ctx context.Context, fs platform.FS, n int, window time.Duration) []Proc {
	a := readProcTimes(fs)
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(window):
	}
	b := readProcTimes(fs)
	var out []Proc
	for pid, now := range b {
		if prev, ok := a[pid]; ok && now.ticks >= prev.ticks {
			pct := float64(now.ticks-prev.ticks) / clockTicks / window.Seconds() * 100
			out = append(out, Proc{PID: pid, Name: now.name, CPUPct: pct})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPUPct > out[j].CPUPct })
	if len(out) > n {
		out = out[:n]
	}
	for i := range out {
		if v, ok := platform.ReadTrim(fs, fmt.Sprintf("/proc/%d/statm", out[i].PID)); ok {
			if f := strings.Fields(v); len(f) > 1 {
				pages, _ := strconv.ParseFloat(f[1], 64)
				out[i].RSSMiB = pages * 4096 / 1048576
			}
		}
	}
	return out
}
