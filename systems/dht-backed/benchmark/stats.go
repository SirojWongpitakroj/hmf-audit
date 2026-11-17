package benchmark

import (
	"fmt"
	"math"
	"time"
)

// Repetitions is the fixed number of independent repetitions per experiment:
// exactly 50 (Sec. V.C, Q39).
const Repetitions = 50

// tCritical95 holds the Student-t critical value for a two-sided 95%
// confidence interval.
//
// The reproduction always runs 50 repetitions, so the degrees of freedom are
// 49 and the value used is t(0.975, 49) = 2.009575. The other entries exist
// only so a mis-sized sample is reported honestly rather than silently using
// the wrong constant.
var tCritical95 = map[int]float64{
	1: 12.706, 2: 4.303, 3: 3.182, 4: 2.776, 5: 2.571,
	9: 2.262, 19: 2.093, 29: 2.045, 39: 2.023, 49: 2.009575,
	99: 1.984, 999: 1.962,
}

// Summary is a measured mean with its 95% confidence interval.
type Summary struct {
	N        int
	Mean     float64
	StdDev   float64
	CI95     float64 // half-width of the interval
	Min, Max float64
}

// String renders the summary as "mean ± half-width".
func (s Summary) String() string {
	return fmt.Sprintf("%.9g ± %.3g", s.Mean, s.CI95)
}

// Summarize computes the mean and the 95% confidence interval half-width using
// the Student-t critical value for n-1 degrees of freedom (Q39). It is
// ordinary Go code; no statistics library is used.
func Summarize(samples []float64) Summary {
	n := len(samples)
	if n == 0 {
		return Summary{}
	}
	sum := 0.0
	min, max := samples[0], samples[0]
	for _, v := range samples {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	mean := sum / float64(n)
	if n == 1 {
		return Summary{N: 1, Mean: mean, Min: min, Max: max}
	}
	ss := 0.0
	for _, v := range samples {
		d := v - mean
		ss += d * d
	}
	sd := math.Sqrt(ss / float64(n-1))
	return Summary{
		N:      n,
		Mean:   mean,
		StdDev: sd,
		CI95:   tCritical95For(n-1) * sd / math.Sqrt(float64(n)),
		Min:    min,
		Max:    max,
	}
}

// tCritical95For returns the two-sided 95% Student-t critical value for df
// degrees of freedom, using the nearest tabulated df at or above it.
func tCritical95For(df int) float64 {
	if v, ok := tCritical95[df]; ok {
		return v
	}
	best, bestDF := tCritical95[1], 1
	for k, v := range tCritical95 {
		if k >= df && (bestDF < df || k < bestDF) {
			best, bestDF = v, k
		}
	}
	return best
}

// SummarizeDurations summarizes a slice of durations in seconds.
func SummarizeDurations(d []time.Duration) Summary {
	xs := make([]float64, len(d))
	for i, v := range d {
		xs[i] = v.Seconds()
	}
	return Summarize(xs)
}
