package canonicalingestion

import (
	"math"
	"testing"
)

func TestAuditMeanConfidenceAndSampleDeviation(t *testing.T) {
	s := SummarizeAudit([]float64{1, 2, 3, 4})
	if s.Mean != 2.5 || s.Median != 2.5 || s.P95 != 4 || math.Abs(s.StdDev-math.Sqrt(5./3)) > 1e-12 || math.Abs(studentT95(3)-3.182446305284263) > 1e-8 {
		t.Fatal(s, studentT95(3))
	}
	if !s.MeanCI95Available || s.MeanCI95Lower >= s.Mean || s.MeanCI95Upper <= s.Mean {
		t.Fatal(s)
	}
	if SummarizeAudit([]float64{1}).MeanCI95Available {
		t.Fatal("one sample cannot estimate a mean CI")
	}
}
