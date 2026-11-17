package bloom

import (
	"crypto/sha256"
	"fmt"
	"math"
	"testing"
)

func item(i int) []byte { d := sha256.Sum256(fmt.Appendf(nil, "kw-%d", i)); return d[:] }

func TestNoFalseNegatives(t *testing.T) {
	m, k := Params(1000, 0.01)
	b := New(m, k)
	for i := range 1000 {
		b.Insert(item(i))
	}
	for i := range 1000 {
		if !b.Check(item(i)) {
			t.Fatalf("false negative for %d", i)
		}
	}
}

func TestEmptyFilterRejects(t *testing.T) {
	b := New(1024, 4)
	if b.Check(item(1)) {
		t.Fatal("empty filter reported membership")
	}
}

func TestFalsePositiveRateNearEq1(t *testing.T) {
	const n = 2000
	m, k := Params(n, 0.01)
	b := New(m, k)
	for i := range n {
		b.Insert(item(i))
	}
	fp := 0
	const trials = 20000
	for i := n; i < n+trials; i++ {
		if b.Check(item(i)) {
			fp++
		}
	}
	got := float64(fp) / trials
	want := FalsePositiveRate(k, n, m)
	if got > 2*want+0.005 {
		t.Fatalf("observed FP %.4f far above Eq.1 %.4f (m=%d k=%d)", got, want, m, k)
	}
}

func TestEq1(t *testing.T) {
	got := FalsePositiveRate(3, 100, 1000)
	want := math.Pow(1-math.Exp(-0.3), 3)
	if math.Abs(got-want) > 1e-12 {
		t.Fatalf("Eq.1 = %v want %v", got, want)
	}
}
