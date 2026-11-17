package canonicalingestion

import (
	"math"
	"sort"
)

type AuditSummary struct {
	Mean, Median, P95, StdDev, MeanCI95Lower, MeanCI95Upper float64
	Samples                                                 int
	MeanCI95Available                                       bool
}

// SummarizeAudit uses sample standard deviation and a two-sided Student-t
// interval for the mean of measured repetitions. Warmups are excluded upstream.
func SummarizeAudit(values []float64) AuditSummary {
	if len(values) == 0 {
		return AuditSummary{}
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	s := AuditSummary{Samples: len(v), P95: v[int(math.Ceil(.95*float64(len(v))))-1]}
	for _, x := range v {
		s.Mean += x
	}
	s.Mean /= float64(len(v))
	s.Median = v[len(v)/2]
	if len(v)%2 == 0 {
		s.Median = (v[len(v)/2-1] + s.Median) / 2
	}
	if len(v) > 1 {
		var sum float64
		for _, x := range v {
			sum += (x - s.Mean) * (x - s.Mean)
		}
		s.StdDev = math.Sqrt(sum / float64(len(v)-1))
		margin := studentT95(len(v)-1) * s.StdDev / math.Sqrt(float64(len(v)))
		s.MeanCI95Lower = s.Mean - margin
		s.MeanCI95Upper = s.Mean + margin
		s.MeanCI95Available = true
	}
	return s
}
func studentT95(df int) float64 {
	if df == 29 {
		return 2.045229642132703
	} // standard 30-run experiment
	a, _ := math.Lgamma((float64(df) + 1) / 2)
	b, _ := math.Lgamma(float64(df) / 2)
	norm := a - b - .5*math.Log(float64(df)*math.Pi)
	f := func(x float64) float64 { return math.Exp(norm - (float64(df)+1)/2*math.Log1p(x*x/float64(df))) }
	var integrate func(float64, float64, float64, float64, float64, float64, float64, int) float64
	integrate = func(l, r, fl, fm, fr, whole, eps float64, depth int) float64 {
		m := (l + r) / 2
		lm, rm := f((l+m)/2), f((m+r)/2)
		left := (m - l) * (fl + 4*lm + fm) / 6
		right := (r - m) * (fm + 4*rm + fr) / 6
		delta := left + right - whole
		if depth == 0 || math.Abs(delta) < 15*eps {
			return left + right + delta/15
		}
		return integrate(l, m, fl, lm, fm, left, eps/2, depth-1) + integrate(m, r, fm, rm, fr, right, eps/2, depth-1)
	}
	cdf := func(x float64) float64 {
		return .5 + integrate(0, x, f(0), f(x/2), f(x), x*(f(0)+4*f(x/2)+f(x))/6, 1e-11, 20)
	}
	low, high := 0., 1.
	for cdf(high) < .975 {
		high *= 2
	}
	for range 50 {
		mid := (low + high) / 2
		if cdf(mid) < .975 {
			low = mid
		} else {
			high = mid
		}
	}
	return (low + high) / 2
}
