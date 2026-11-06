// Package stats has the small descriptive statistics shared by Network Doctor
// and Benchmark Lab. Everything operates on copies; inputs are never reordered.
package stats

import (
	"math"
	"sort"
)

func Mean(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	var s float64
	for _, v := range x {
		s += v
	}
	return s / float64(len(x))
}

// StdDev is the sample standard deviation (n-1); NaN for fewer than 2 values.
func StdDev(x []float64) float64 {
	if len(x) < 2 {
		return math.NaN()
	}
	m := Mean(x)
	var s float64
	for _, v := range x {
		s += (v - m) * (v - m)
	}
	return math.Sqrt(s / float64(len(x)-1))
}

// CV is the coefficient of variation (stddev/mean), the run-to-run noise measure.
func CV(x []float64) float64 {
	m := Mean(x)
	if m == 0 || math.IsNaN(m) {
		return math.NaN()
	}
	return StdDev(x) / math.Abs(m)
}

// Percentile returns the p-th percentile (0..100) with linear interpolation.
func Percentile(x []float64, p float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	s := append([]float64(nil), x...)
	sort.Float64s(s)
	if p <= 0 {
		return s[0]
	}
	if p >= 100 {
		return s[len(s)-1]
	}
	pos := p / 100 * float64(len(s)-1)
	lo := int(math.Floor(pos))
	frac := pos - float64(lo)
	if lo+1 >= len(s) {
		return s[lo]
	}
	return s[lo] + frac*(s[lo+1]-s[lo])
}

func Median(x []float64) float64 { return Percentile(x, 50) }

func Min(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	m := x[0]
	for _, v := range x {
		m = math.Min(m, v)
	}
	return m
}

func Max(x []float64) float64 {
	if len(x) == 0 {
		return math.NaN()
	}
	m := x[0]
	for _, v := range x {
		m = math.Max(m, v)
	}
	return m
}

// MeanAbsDiff is the mean absolute difference of consecutive samples; for
// round-trip times this is the usual definition of jitter.
func MeanAbsDiff(x []float64) float64 {
	if len(x) < 2 {
		return math.NaN()
	}
	var s float64
	for i := 1; i < len(x); i++ {
		s += math.Abs(x[i] - x[i-1])
	}
	return s / float64(len(x)-1)
}
