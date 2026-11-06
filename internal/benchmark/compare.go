package benchmark

import (
	"fmt"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strings"

	"github.com/mttcn83/turboshift/internal/stats"
)

// Verdict is the honest outcome of comparing two configurations.
type Verdict string

const (
	VerdictImproved     Verdict = "Measurable improvement"
	VerdictRegressed    Verdict = "Regression"
	VerdictNoClear      Verdict = "No statistically clear difference"
	VerdictInconclusive Verdict = "Inconclusive"
	VerdictInsufficient Verdict = "Insufficient measurements"
)

// Alpha is the significance level; MinEffect is the smallest relative change
// worth calling a difference, however significant.
const (
	Alpha     = 0.05
	MinEffect = 0.01 // 1 %
	NoisyCV   = 0.10 // run-to-run spread above 10 % is flagged
)

// Metric selects what is compared.
type Metric struct {
	Name         string
	Unit         string
	HigherBetter bool
	Get          func(Run) float64
}

var (
	MetricAvgFPS = Metric{"Average FPS", "fps", true, func(r Run) float64 { return r.AvgFPS }}
	MetricLow1   = Metric{"1% low FPS", "fps", true, func(r Run) float64 { return r.Low1FPS }}
	MetricP99    = Metric{"99th pct frame time", "ms", false, func(r Run) float64 { return r.FrameP99Ms }}
	Metrics      = []Metric{MetricAvgFPS, MetricLow1, MetricP99}
)

// MetricResult is the comparison of one metric between configurations A and B.
type MetricResult struct {
	Metric   Metric
	NA, NB   int
	MedianA  float64
	MedianB  float64
	MinA     float64
	MaxA     float64
	MinB     float64
	MaxB     float64
	CVA, CVB float64 // run-to-run coefficient of variation
	DeltaPct float64 // (B-A)/A in percent, raw sign
	P        float64 // two-sided permutation p-value (NaN if not computed)
	MinP     float64 // smallest p-value achievable with these sample sizes
	Verdict  Verdict
	Reason   string
}

// Comparison bundles all metrics plus methodological warnings.
type Comparison struct {
	LabelA, LabelB string
	Results        []MetricResult
	Warnings       []string
}

// Compare evaluates B against A on every metric.
func Compare(labelA string, a []Run, labelB string, b []Run) Comparison {
	c := Comparison{LabelA: labelA, LabelB: labelB}
	for _, m := range Metrics {
		c.Results = append(c.Results, compareMetric(m, a, b))
	}
	c.Warnings = methodWarnings(labelA, a, labelB, b)
	return c
}

func values(m Metric, runs []Run) []float64 {
	v := make([]float64, len(runs))
	for i, r := range runs {
		v[i] = m.Get(r)
	}
	return v
}

func compareMetric(m Metric, a, b []Run) MetricResult {
	x, y := values(m, a), values(m, b)
	r := MetricResult{Metric: m, NA: len(x), NB: len(y), P: math.NaN(), MinP: math.NaN()}
	if len(x) > 0 {
		r.MedianA, r.MinA, r.MaxA, r.CVA = stats.Median(x), stats.Min(x), stats.Max(x), stats.CV(x)
	}
	if len(y) > 0 {
		r.MedianB, r.MinB, r.MaxB, r.CVB = stats.Median(y), stats.Min(y), stats.Max(y), stats.CV(y)
	}
	if r.MedianA != 0 {
		r.DeltaPct = (r.MedianB - r.MedianA) / r.MedianA * 100
	}

	if len(x) < 2 || len(y) < 2 {
		r.Verdict = VerdictInsufficient
		r.Reason = fmt.Sprintf("Need at least 2 runs of each configuration to measure run-to-run variation (have %d and %d), and %d or more each to reach significance.", len(x), len(y), minRunsFor(Alpha))
		return r
	}
	r.MinP = 2 / combinations(len(x)+len(y), len(x))
	if r.MinP > Alpha {
		r.Verdict = VerdictInsufficient
		r.Reason = fmt.Sprintf("With %d and %d runs, even a perfectly consistent difference could not reach p<%.2f (best possible p=%.3f). Record at least %d runs of each configuration.",
			len(x), len(y), Alpha, r.MinP, minRunsFor(Alpha))
		return r
	}

	r.P = permutationP(x, y)
	rel := (stats.Mean(y) - stats.Mean(x)) / stats.Mean(x)
	better := rel > 0
	if !m.HigherBetter {
		better = rel < 0
	}
	switch {
	case r.P < Alpha && math.Abs(rel) >= MinEffect:
		if better {
			r.Verdict = VerdictImproved
		} else {
			r.Verdict = VerdictRegressed
		}
		r.Reason = fmt.Sprintf("Median %.1f → %.1f %s (%+.1f%%), p=%.3f over %d vs %d runs.", r.MedianA, r.MedianB, m.Unit, r.DeltaPct, r.P, len(x), len(y))
	case r.P < Alpha:
		r.Verdict = VerdictNoClear
		r.Reason = fmt.Sprintf("Statistically detectable (p=%.3f) but only %+.2f%%, below the %.0f%% needed to matter in practice.", r.P, r.DeltaPct, MinEffect*100)
	case math.Max(r.CVA, r.CVB) > NoisyCV:
		r.Verdict = VerdictInconclusive
		r.Reason = fmt.Sprintf("Runs of the same configuration differ by up to %.0f%% between themselves, which hides any smaller change (p=%.2f). Reduce background activity, fix the test scene, and repeat.",
			math.Max(r.CVA, r.CVB)*100, r.P)
	default:
		r.Verdict = VerdictNoClear
		r.Reason = fmt.Sprintf("The %+.1f%% difference is within normal run-to-run variation (p=%.2f). The change did not measurably help in this workload.", r.DeltaPct, r.P)
	}
	return r
}

// minRunsFor returns the smallest equal group size n for which 2/C(2n,n) <= alpha.
func minRunsFor(alpha float64) int {
	for n := 2; n < 30; n++ {
		if 2/combinations(2*n, n) <= alpha {
			return n
		}
	}
	return 30
}

func combinations(n, k int) float64 {
	if k > n-k {
		k = n - k
	}
	r := 1.0
	for i := 1; i <= k; i++ {
		r = r * float64(n-k+i) / float64(i)
	}
	return math.Round(r)
}

// permutationP is the two-sided permutation test p-value for a difference in
// means: exact (all assignments) when feasible, otherwise 20 000 random
// shuffles with a fixed seed so results are reproducible.
func permutationP(x, y []float64) float64 {
	all := append(append([]float64(nil), x...), y...)
	n, k := len(all), len(x)
	obs := math.Abs(stats.Mean(y) - stats.Mean(x))
	const eps = 1e-9
	diffOf := func(inX []bool) float64 {
		var sx, sy float64
		for i, v := range all {
			if inX[i] {
				sx += v
			} else {
				sy += v
			}
		}
		return math.Abs(sy/float64(n-k) - sx/float64(k))
	}
	total := combinations(n, k)
	count := 0
	if total <= 200000 {
		inX := make([]bool, n)
		var rec func(start, left int)
		var seen float64
		rec = func(start, left int) {
			if left == 0 {
				seen++
				if diffOf(inX) >= obs-eps {
					count++
				}
				return
			}
			for i := start; i <= n-left; i++ {
				inX[i] = true
				rec(i+1, left-1)
				inX[i] = false
			}
		}
		rec(0, k)
		return float64(count) / seen
	}
	rng := rand.New(rand.NewSource(1))
	const iters = 20000
	for it := 0; it < iters; it++ {
		perm := rng.Perm(n)
		inX := make([]bool, n)
		for _, i := range perm[:k] {
			inX[i] = true
		}
		if diffOf(inX) >= obs-eps {
			count++
		}
	}
	return float64(count+1) / float64(iters+1)
}

func methodWarnings(la string, a []Run, lb string, b []Run) []string {
	var w []string
	diff := func(name string, get func(Run) string) {
		set := map[string]bool{}
		for _, r := range append(append([]Run(nil), a...), b...) {
			if v := get(r); v != "" {
				set[v] = true
			}
		}
		if len(set) > 1 {
			var v []string
			for s := range set {
				v = append(v, s)
			}
			sort.Strings(v)
			w = append(w, fmt.Sprintf("%s differs between runs (%s). Only the setting under test should change.", name, strings.Join(v, " vs ")))
		}
	}
	diff("Kernel", func(r Run) string { return r.Info.Kernel })
	diff("GPU", func(r Run) string { return r.Info.GPU })
	diff("CPU", func(r Run) string { return r.Info.CPU })
	diff("OS", func(r Run) string { return r.Info.OS })
	diff("Game/program", func(r Run) string { return ProgramName(r.Source) })

	if len(a) > 0 && len(b) > 0 {
		lastA, firstB := a[0].StartedAt, b[0].StartedAt
		for _, r := range a {
			if r.StartedAt.After(lastA) {
				lastA = r.StartedAt
			}
		}
		for _, r := range b {
			if r.StartedAt.Before(firstB) {
				firstB = r.StartedAt
			}
		}
		firstA, lastB := a[0].StartedAt, b[0].StartedAt
		for _, r := range a {
			if r.StartedAt.Before(firstA) {
				firstA = r.StartedAt
			}
		}
		for _, r := range b {
			if r.StartedAt.After(lastB) {
				lastB = r.StartedAt
			}
		}
		if lastA.Before(firstB) || lastB.Before(firstA) {
			w = append(w, fmt.Sprintf("All %q runs happened before all %q runs (or the reverse). Heat build-up and caches can bias that. Interleave the order (A B B A A B B A) when you record.", la, lb))
		}
	}
	var warm int
	for _, r := range append(append([]Run(nil), a...), b...) {
		if r.TrimStartS == 0 {
			warm++
		}
	}
	if warm > 0 {
		w = append(w, "Some runs were imported without trimming warm-up. Shader compilation and loading in the first seconds add noise; consider --trim-start 10.")
	}
	return w
}

// Overall condenses the primary metric (average FPS) into a headline label,
// but never hides a regression in the lows.
func (c Comparison) Overall() (Verdict, string) {
	res := c.Results
	for _, r := range res {
		if r.Verdict == VerdictRegressed {
			return VerdictRegressed, fmt.Sprintf("%s got worse: %s", r.Metric.Name, r.Reason)
		}
	}
	for _, r := range res {
		if r.Verdict == VerdictImproved {
			return VerdictImproved, fmt.Sprintf("%s improved: %s", r.Metric.Name, r.Reason)
		}
	}
	for _, v := range []Verdict{VerdictInsufficient, VerdictInconclusive} {
		for _, r := range res {
			if r.Verdict == v {
				return v, r.Reason
			}
		}
	}
	return VerdictNoClear, res[0].Reason
}

var programRe = regexp.MustCompile(`^(.*)_\d{4}-\d{2}-\d{2}_\d{2}-\d{2}-\d{2}(?:_summary)?\.csv$`)

// ProgramName extracts the game/program part of a MangoHud log file name.
func ProgramName(source string) string {
	if m := programRe.FindStringSubmatch(source); m != nil {
		return m[1]
	}
	return source
}
