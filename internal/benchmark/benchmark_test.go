package benchmark

import (
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mttcn83/turboshift/internal/platform"
)

const frameHeader = "fps,frametime,cpu_load,cpu_power,gpu_load,cpu_temp,gpu_temp,gpu_core_clock,gpu_mem_clock,gpu_vram_used,gpu_power,ram_used,swap_used,process_rss,cpu_mhz,elapsed"

// genLog builds a MangoHud-format log from frame times in ms.
func genLog(frameMs []float64, versioned bool) string {
	var b strings.Builder
	if versioned {
		b.WriteString("v1\n0.8.1\n---------------------SYSTEM INFO---------------------\n")
	}
	b.WriteString("os,cpu,gpu,ram,kernel,driver,cpuscheduler\n")
	b.WriteString("CachyOS,Intel(R) Core(TM) i7-14650HX,GeForce RTX 5060,32000000,7.3.1-1-cachyos,,powersave\n")
	if versioned {
		b.WriteString("--------------------FRAME METRICS--------------------\n")
	}
	b.WriteString(frameHeader + "\n")
	var el float64
	for _, ms := range frameMs {
		el += ms * 1e6 // ns
		fmt.Fprintf(&b, "%.1f,%.3f,40,25,%d,60,55,2000,9000,4000,60,8,0,2,4000,%d\n", 1000/ms, ms, 70, int64(el))
	}
	return b.String()
}

func constFrames(n int, ms float64) []float64 {
	f := make([]float64, n)
	for i := range f {
		f[i] = ms
	}
	return f
}

func TestParseBothHeaderLayouts(t *testing.T) {
	for _, versioned := range []bool{false, true} {
		f, err := ParseMangoHud(strings.NewReader(genLog(constFrames(200, 10), versioned)))
		if err != nil {
			t.Fatalf("versioned=%v: %v", versioned, err)
		}
		if len(f.FPS) != 200 || f.FrameMs[0] != 10 {
			t.Errorf("frames: %d %v", len(f.FPS), f.FrameMs[0])
		}
		if f.Info.CPU != "Intel(R) Core(TM) i7-14650HX" || f.Info.Kernel != "7.3.1-1-cachyos" || f.Info.RAMKB != 32000000 || f.Info.CPUSched != "powersave" {
			t.Errorf("sysinfo: %+v", f.Info)
		}
		if last := f.ElapsedS[len(f.ElapsedS)-1]; math.Abs(last-2.0) > 1e-9 {
			t.Errorf("elapsed ns->s: %v", last)
		}
		if len(f.GPULoad) != 200 || f.GPULoad[0] != 70 {
			t.Error("optional columns not parsed by name")
		}
	}
}

func TestParseRejectsNonLogsAndSkipsGarbageRows(t *testing.T) {
	for _, bad := range []string{"", "hello\nworld\n", "a,b,c\n1,2,3\n", strings.Repeat("junk\n", 100)} {
		if _, err := ParseMangoHud(strings.NewReader(bad)); err == nil {
			t.Errorf("accepted %q", bad[:min(len(bad), 20)])
		}
	}
	if _, err := ParseMangoHud(strings.NewReader(frameHeader + "\n")); err == nil {
		t.Error("header-only log accepted")
	}
	// Garbage, NaN, zero frametime and a torn final line are skipped, never invented.
	log := genLog(constFrames(150, 10), false) + "nan,nan,1\n100,0,1,1,1,1,1,1,1,1,1,1,1,1,1,1\n100,10,1,1"
	f, err := ParseMangoHud(strings.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.FPS) != 150 {
		t.Errorf("got %d frames, want 150 (bad rows skipped)", len(f.FPS))
	}
}

func TestParseHugeLineDoesNotExhaustMemory(t *testing.T) {
	log := genLog(constFrames(10, 10), false) + strings.Repeat("9", 2<<20) + "\n"
	if _, err := ParseMangoHud(strings.NewReader(log)); err == nil {
		t.Error("expected an error for an oversized line")
	}
}

func TestSummarizeKnownDistribution(t *testing.T) {
	frames := append(constFrames(990, 10), constFrames(10, 40)...)
	f, _ := ParseMangoHud(strings.NewReader(genLog(frames, false)))
	r, err := Summarize(f, ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// total 10300 ms over 1000 frames -> 97.087 fps (time-weighted)
	if math.Abs(r.AvgFPS-97.087) > 0.01 {
		t.Errorf("avg fps = %v", r.AvgFPS)
	}
	// p99 frame time = 10.3 ms -> 1% low = 97.09 fps
	if math.Abs(r.FrameP99Ms-10.3) > 0.01 || math.Abs(r.Low1FPS-97.087) > 0.05 {
		t.Errorf("p99 = %v, 1%% low = %v", r.FrameP99Ms, r.Low1FPS)
	}
	if r.Low01FPS >= r.Low1FPS {
		t.Errorf("0.1%% low (%v) must not exceed 1%% low (%v)", r.Low01FPS, r.Low1FPS)
	}
	if r.GPULoadAvg != 70 {
		t.Errorf("gpu load = %v", r.GPULoadAvg)
	}
}

func TestSummarizeTrimAndTooFewFrames(t *testing.T) {
	// first 1 s slow (shader compile), rest fast
	frames := append(constFrames(20, 50), constFrames(500, 10)...)
	f, _ := ParseMangoHud(strings.NewReader(genLog(frames, false)))
	all, _ := Summarize(f, ImportOptions{})
	trimmed, err := Summarize(f, ImportOptions{TrimStartS: 1.1})
	if err != nil {
		t.Fatal(err)
	}
	if trimmed.AvgFPS <= all.AvgFPS || math.Abs(trimmed.AvgFPS-100) > 0.5 {
		t.Errorf("trim ineffective: all=%v trimmed=%v", all.AvgFPS, trimmed.AvgFPS)
	}
	if _, err := Summarize(f, ImportOptions{TrimStartS: 5.9}); err == nil {
		t.Error("too few frames after trim should be an error")
	}
}

func TestBottleneckHintIsHedged(t *testing.T) {
	if h := bottleneckHint(Run{GPULoadAvg: 99, CPULoadAvg: 30}); !strings.Contains(h, "GPU-bound") {
		t.Error(h)
	}
	if h := bottleneckHint(Run{GPULoadAvg: 60, CPULoadAvg: 80}); !strings.Contains(h, "possibly") {
		t.Error(h)
	}
	if bottleneckHint(Run{}) != "" {
		t.Error("no data must give no hint")
	}
}

// runs builds n runs whose AvgFPS are centred on mean with the given relative noise.
func runs(label string, n int, mean, noise float64, seed int64, start time.Time, gap time.Duration) []Run {
	rng := rand.New(rand.NewSource(seed))
	out := make([]Run, n)
	for i := range out {
		v := mean * (1 + noise*(rng.Float64()*2-1))
		out[i] = Run{Label: label, AvgFPS: v, Low1FPS: v * 0.8, FrameP99Ms: 1000 / (v * 0.8), TrimStartS: 10,
			StartedAt: start.Add(time.Duration(i) * gap), Source: "game_2026-11-06_12-00-00.csv"}
	}
	return out
}

var t0 = time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC)

func TestCompareClearImprovement(t *testing.T) {
	a := runs("A", 5, 100, 0.01, 1, t0, time.Hour)
	b := runs("B", 5, 108, 0.01, 2, t0.Add(30*time.Minute), time.Hour)
	c := Compare("A", a, "B", b)
	if c.Results[0].Verdict != VerdictImproved {
		t.Fatalf("%+v", c.Results[0])
	}
	if c.Results[0].DeltaPct < 6 || c.Results[0].P >= 0.05 {
		t.Errorf("%+v", c.Results[0])
	}
	// lower-is-better metric: frame time got lower -> improved too
	if c.Results[2].Metric.Name != "99th pct frame time" || c.Results[2].Verdict != VerdictImproved {
		t.Errorf("p99 direction wrong: %+v", c.Results[2])
	}
	if v, _ := c.Overall(); v != VerdictImproved {
		t.Errorf("overall = %v", v)
	}
}

func TestCompareRegression(t *testing.T) {
	a := runs("A", 5, 100, 0.01, 1, t0, time.Hour)
	b := runs("B", 5, 90, 0.01, 2, t0.Add(30*time.Minute), time.Hour)
	if v, _ := Compare("A", a, "B", b).Overall(); v != VerdictRegressed {
		t.Errorf("overall = %v", v)
	}
}

func TestCompareSameConfigurationShowsNoDifference(t *testing.T) {
	// Run-to-run noise only (±2 %): must not be called an improvement.
	falsePositives := 0
	for seed := int64(0); seed < 200; seed++ {
		a := runs("A", 5, 100, 0.02, seed*2+1, t0, time.Hour)
		b := runs("B", 5, 100, 0.02, seed*2+2, t0, time.Hour)
		if r := Compare("A", a, "B", b).Results[0]; r.Verdict == VerdictImproved || r.Verdict == VerdictRegressed {
			falsePositives++
		}
	}
	// alpha = 5 %: expect about 10 of 200; allow generous slack but fail on a broken test.
	if falsePositives > 25 {
		t.Errorf("%d/200 false positives; the test is not controlling error rate", falsePositives)
	}
}

func TestCompareInsufficientRunsStatesWhy(t *testing.T) {
	a := runs("A", 3, 100, 0.01, 1, t0, time.Hour)
	b := runs("B", 3, 130, 0.01, 2, t0, time.Hour)
	r := Compare("A", a, "B", b).Results[0]
	if r.Verdict != VerdictInsufficient || !strings.Contains(r.Reason, "0.100") || !strings.Contains(r.Reason, "at least 4") {
		t.Errorf("a 30%% jump with 3 vs 3 runs is still not provable: %+v", r)
	}
	one := Compare("A", runs("A", 1, 100, 0, 1, t0, 0), "B", b).Results[0]
	if one.Verdict != VerdictInsufficient {
		t.Errorf("%+v", one)
	}
}

func TestCompareNoisyRunsAreInconclusive(t *testing.T) {
	a := runs("A", 5, 100, 0.25, 7, t0, time.Hour)
	b := runs("B", 5, 103, 0.25, 8, t0, time.Hour)
	r := Compare("A", a, "B", b).Results[0]
	if r.Verdict != VerdictInconclusive && r.Verdict != VerdictNoClear {
		t.Fatalf("%+v", r)
	}
	if r.Verdict == VerdictInconclusive && !strings.Contains(r.Reason, "differ by up to") {
		t.Errorf("reason: %s", r.Reason)
	}
}

func TestTinyButSignificantDifferenceIsNotPracticallyMeaningful(t *testing.T) {
	a := runs("A", 6, 100, 0.0005, 1, t0, time.Hour)
	b := runs("B", 6, 100.5, 0.0005, 2, t0, time.Hour)
	r := Compare("A", a, "B", b).Results[0]
	if r.Verdict != VerdictNoClear || !strings.Contains(r.Reason, "below") {
		t.Errorf("%+v", r)
	}
}

func TestPermutationExactValue(t *testing.T) {
	// Perfectly separated 4 vs 4: only the 2 extreme splits out of C(8,4)=70 are as extreme.
	p := permutationP([]float64{1, 2, 3, 4}, []float64{5, 6, 7, 8})
	if math.Abs(p-2.0/70) > 1e-12 {
		t.Errorf("p = %v, want %v", p, 2.0/70)
	}
	if got := minRunsFor(0.05); got != 4 {
		t.Errorf("minRunsFor = %d, want 4", got)
	}
	if combinations(8, 4) != 70 || combinations(10, 5) != 252 {
		t.Error("combinations")
	}
}

func TestPermutationMonteCarloIsDeterministic(t *testing.T) {
	x, y := runsVals(12, 100, 3), runsVals(12, 103, 4) // C(24,12) = 2.7M > exact limit
	if p1, p2 := permutationP(x, y), permutationP(x, y); p1 != p2 {
		t.Errorf("not reproducible: %v %v", p1, p2)
	}
}

func runsVals(n int, mean float64, seed int64) []float64 {
	rng := rand.New(rand.NewSource(seed))
	v := make([]float64, n)
	for i := range v {
		v[i] = mean + rng.NormFloat64()*2
	}
	return v
}

func TestMethodologyWarnings(t *testing.T) {
	a := runs("A", 4, 100, 0.01, 1, t0, time.Hour)                   // 12:00–15:00
	b := runs("B", 4, 100, 0.01, 2, t0.Add(10*time.Hour), time.Hour) // all later
	b[0].Info.Kernel = "7.3.2"
	a[0].Info.Kernel = "7.3.1"
	w := strings.Join(Compare("A", a, "B", b).Warnings, "\n")
	for _, want := range []string{"Kernel differs", "Interleave"} {
		if !strings.Contains(w, want) {
			t.Errorf("missing warning %q in:\n%s", want, w)
		}
	}
	// interleaved ABBA: no ordering warning
	a2 := runs("A", 4, 100, 0.01, 1, t0, 2*time.Hour)
	b2 := runs("B", 4, 100, 0.01, 2, t0.Add(time.Hour), 2*time.Hour)
	if w := strings.Join(Compare("A", a2, "B", b2).Warnings, "\n"); strings.Contains(w, "Interleave") {
		t.Errorf("interleaved runs flagged: %s", w)
	}
}

func TestStoreImportListDelete(t *testing.T) {
	dir := t.TempDir()
	st := &Store{Dir: filepath.Join(dir, "runs"), Now: func() time.Time { return t0 }}
	fs := platform.NewMemFS()
	fs.Set("/logs/Game_2026-11-06_12-00-00.csv", genLog(constFrames(300, 8), false))
	fs.Set("/logs/bad.csv", "nope")

	run, err := st.Import(fs, "/logs/Game_2026-11-06_12-00-00.csv", "balanced", ImportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if run.Label != "balanced" || run.Frames != 300 || math.Abs(run.AvgFPS-125) > 0.01 {
		t.Errorf("%+v", run)
	}
	if run.StartedAt.IsZero() {
		t.Error("start time from filename missing")
	}
	got, _ := st.List()
	if len(got) != 1 || got[0].ID != run.ID {
		t.Fatalf("list: %+v", got)
	}
	if _, err := st.Import(fs, "/logs/bad.csv", "x", ImportOptions{}); err == nil {
		t.Error("bad log accepted")
	}
	if _, err := st.Import(fs, "/logs/missing.csv", "x", ImportOptions{}); err == nil {
		t.Error("missing file accepted")
	}
	for _, l := range []string{"", "../x", "a/b", strings.Repeat("x", 41), "semi;colon"} {
		if _, err := st.Import(fs, "/logs/Game_2026-11-06_12-00-00.csv", l, ImportOptions{}); err == nil {
			t.Errorf("label %q accepted", l)
		}
	}
	if err := st.Delete("../../etc/passwd"); err == nil {
		t.Error("path traversal in Delete")
	}
	if err := st.Delete(run.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.List(); len(got) != 0 {
		t.Error("not deleted")
	}
}

func TestFindLogs(t *testing.T) {
	fs := platform.NewMemFS()
	fs.Set("/home/u/Game_2026-11-06_12-00-00.csv", "x")
	fs.Set("/home/u/Game_2026-11-06_12-00-00_summary.csv", "x")
	fs.Set("/home/u/Other_2026-11-07_09-00-00.csv", "x")
	fs.Set("/home/u/notes.csv", "x")
	fs.Set("/tmp/MangoHud/Zed_2026-11-05_01-02-03.csv", "x")
	got := FindLogs(fs, []string{"/home/u", "/tmp/MangoHud", "/nonexistent", "/home/u"})
	want := []string{"/home/u/Other_2026-11-07_09-00-00.csv", "/home/u/Game_2026-11-06_12-00-00.csv", "/tmp/MangoHud/Zed_2026-11-05_01-02-03.csv"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got %v", got)
	}
	if ProgramName("Cyberpunk 2077_2026-11-06_12-00-00.csv") != "Cyberpunk 2077" {
		t.Error("ProgramName")
	}
}
