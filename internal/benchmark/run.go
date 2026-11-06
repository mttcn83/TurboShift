package benchmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/stats"
)

// Run is the stored summary of one imported log. Raw frames are not stored.
type Run struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"` // the configuration this run belongs to, e.g. "balanced"
	Source     string    `json:"source"`
	StartedAt  time.Time `json:"started_at"`
	ImportedAt time.Time `json:"imported_at"`
	Info       SysInfo   `json:"system"`

	Frames     int     `json:"frames"`
	DurationS  float64 `json:"duration_s"`
	AvgFPS     float64 `json:"avg_fps"`
	Low1FPS    float64 `json:"low1_fps"`
	Low01FPS   float64 `json:"low01_fps"`
	FrameP50Ms float64 `json:"frame_p50_ms"`
	FrameP95Ms float64 `json:"frame_p95_ms"`
	FrameP99Ms float64 `json:"frame_p99_ms"`
	CPULoadAvg float64 `json:"cpu_load_avg,omitempty"`
	GPULoadAvg float64 `json:"gpu_load_avg,omitempty"`
	CPUTempAvg float64 `json:"cpu_temp_avg_c,omitempty"`
	GPUTempAvg float64 `json:"gpu_temp_avg_c,omitempty"`
	CPUTempMax float64 `json:"cpu_temp_max_c,omitempty"`
	TrimStartS float64 `json:"trim_start_s,omitempty"`
	TrimEndS   float64 `json:"trim_end_s,omitempty"`
	Bottleneck string  `json:"bottleneck_hint,omitempty"`
}

// ImportOptions trims warm-up and tail from a log before summarising.
type ImportOptions struct {
	TrimStartS, TrimEndS float64
}

// Summarize computes run metrics from parsed frames.
func Summarize(f *Frames, opt ImportOptions) (Run, error) {
	total := f.ElapsedS[len(f.ElapsedS)-1]
	lo, hi := opt.TrimStartS, total-opt.TrimEndS
	var ft []float64
	var idx []int
	for i, el := range f.ElapsedS {
		if el >= lo && el <= hi {
			ft = append(ft, f.FrameMs[i])
			idx = append(idx, i)
		}
	}
	if len(ft) < 100 {
		return Run{}, fmt.Errorf("only %d frames remain after trimming; need at least 100 for stable percentiles", len(ft))
	}
	var sumMs float64
	for _, v := range ft {
		sumMs += v
	}
	r := Run{
		Info: f.Info, Frames: len(ft), DurationS: sumMs / 1000,
		AvgFPS:     float64(len(ft)) / (sumMs / 1000), // time-weighted, not mean of per-frame FPS
		Low1FPS:    percentileFPS(ft, 1),
		Low01FPS:   percentileFPS(ft, 0.1),
		FrameP50Ms: stats.Percentile(ft, 50), FrameP95Ms: stats.Percentile(ft, 95), FrameP99Ms: stats.Percentile(ft, 99),
		TrimStartS: opt.TrimStartS, TrimEndS: opt.TrimEndS,
	}
	pick := func(src []float64) []float64 {
		if len(src) != len(f.FPS) {
			return nil
		}
		out := make([]float64, 0, len(idx))
		for _, i := range idx {
			out = append(out, src[i])
		}
		return out
	}
	avg := func(src []float64) float64 {
		if p := pick(src); len(p) > 0 {
			return stats.Mean(p)
		}
		return 0
	}
	r.CPULoadAvg, r.GPULoadAvg = avg(f.CPULoad), avg(f.GPULoad)
	r.CPUTempAvg, r.GPUTempAvg = avg(f.CPUTempC), avg(f.GPUTempC)
	if p := pick(f.CPUTempC); len(p) > 0 {
		r.CPUTempMax = stats.Max(p)
	}
	r.Bottleneck = bottleneckHint(r)
	return r, nil
}

// bottleneckHint is a coarse, explicitly hedged reading of average load.
func bottleneckHint(r Run) string {
	switch {
	case r.GPULoadAvg == 0 && r.CPULoadAvg == 0:
		return ""
	case r.GPULoadAvg >= 95:
		return "GPU-bound (GPU load ≈ " + fmt.Sprintf("%.0f%%", r.GPULoadAvg) + "): CPU-side tweaks are unlikely to raise FPS"
	case r.GPULoadAvg > 0 && r.GPULoadAvg < 85 && r.CPULoadAvg >= 70:
		return "possibly CPU-bound (GPU " + fmt.Sprintf("%.0f%%", r.GPULoadAvg) + ", CPU " + fmt.Sprintf("%.0f%%", r.CPULoadAvg) + "); MangoHud's CPU load is an average over all cores, so one saturated thread can hide here"
	}
	return ""
}

// ---- store ----

// Store keeps one JSON file per run under Dir.
type Store struct {
	Dir string
	Now func() time.Time
}

var runIDRe = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
var labelRe = regexp.MustCompile(`^[A-Za-z0-9 _.+()-]{1,40}$`)

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// ValidateLabel enforces a short, filesystem-safe label.
func ValidateLabel(l string) error {
	if !labelRe.MatchString(l) {
		return errors.New("label must be 1–40 characters: letters, digits, space and _ . + ( ) -")
	}
	return nil
}

// Import parses a log from fs, summarises it and stores it under label.
func (s *Store) Import(fs platform.FS, path, label string, opt ImportOptions) (Run, error) {
	if err := ValidateLabel(label); err != nil {
		return Run{}, err
	}
	if fi, err := fs.Stat(path); err != nil {
		return Run{}, fmt.Errorf("cannot open %s: %w", path, err)
	} else if fi.IsDir() {
		return Run{}, fmt.Errorf("%s is a directory", path)
	} else if fi.Size() > 400<<20 {
		return Run{}, fmt.Errorf("%s is larger than 400 MiB; refusing to import", path)
	}
	b, err := fs.ReadFile(path)
	if err != nil {
		return Run{}, err
	}
	fr, err := ParseMangoHud(bytes.NewReader(b))
	if err != nil {
		return Run{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	run, err := Summarize(fr, opt)
	if err != nil {
		return Run{}, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	run.Label, run.Source = label, filepath.Base(path)
	run.ImportedAt = s.now().UTC()
	if t, ok := LogStartTime(run.Source); ok {
		run.StartedAt = t.UTC()
	} else if fi, err := fs.Stat(path); err == nil {
		run.StartedAt = fi.ModTime().UTC()
	}
	run.ID = fmt.Sprintf("%s-%08x", run.ImportedAt.Format("20060102T150405"), fnv(run.Source+label+run.ImportedAt.String()))
	if err := s.save(run); err != nil {
		return Run{}, err
	}
	return run, nil
}

func fnv(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h = (h ^ uint32(s[i])) * 16777619
	}
	return h
}

func (s *Store) save(r Run) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	tmp := filepath.Join(s.Dir, "."+r.ID+".tmp")
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.Dir, r.ID+".json"))
}

// List returns all stored runs, oldest first. Unreadable files are skipped.
func (s *Store) List() ([]Run, error) {
	ents, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Run
	for _, e := range ents {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.Dir, e.Name()))
		if err != nil {
			continue
		}
		var r Run
		if json.Unmarshal(b, &r) == nil && runIDRe.MatchString(r.ID) && r.Frames > 0 {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// Delete removes one stored run.
func (s *Store) Delete(id string) error {
	if !runIDRe.MatchString(id) {
		return fmt.Errorf("invalid run id %q", id)
	}
	return os.Remove(filepath.Join(s.Dir, id+".json"))
}

// ByLabel groups runs by configuration label.
func ByLabel(runs []Run) map[string][]Run {
	m := map[string][]Run{}
	for _, r := range runs {
		m[r.Label] = append(m[r.Label], r)
	}
	return m
}

// Labels returns distinct labels, sorted.
func Labels(runs []Run) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range runs {
		if !seen[r.Label] {
			seen[r.Label] = true
			out = append(out, r.Label)
		}
	}
	sort.Strings(out)
	return out
}

// FindLogs lists MangoHud log files (not summaries) in the given directories,
// non-recursively, newest first.
func FindLogs(fs platform.FS, dirs []string) []string {
	type f struct {
		p string
		t time.Time
	}
	var found []f
	seen := map[string]bool{}
	for _, d := range dirs {
		ents, err := fs.ReadDir(d)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || !IsMangoHudLogName(e.Name()) {
				continue
			}
			p := filepath.Join(d, e.Name())
			if seen[p] {
				continue
			}
			seen[p] = true
			t, ok := LogStartTime(e.Name())
			if !ok {
				if fi, err := e.Info(); err == nil {
					t = fi.ModTime()
				}
			}
			found = append(found, f{p, t})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].t.After(found[j].t) })
	out := make([]string, len(found))
	for i, x := range found {
		out[i] = x.p
	}
	return out
}
