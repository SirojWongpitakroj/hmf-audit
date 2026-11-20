package benchmark

import (
	"path/filepath"
	"testing"
)

func TestFairNativeDHTBatchesLoadAllCells(t *testing.T) {
	dir := filepath.Join("..", "..", "experiments", "native-queries", "10000000")
	batches, cases, err := fairNativeDHTBatches(dir, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != len(fairNativeShapes)*len(fairDHTRequestCounts)*fairNativeSets {
		t.Fatalf("got %d cases", len(cases))
	}
	for _, c := range cases {
		if len(batches[c]) != c.count {
			t.Fatalf("%+v has %d IDs", c, len(batches[c]))
		}
	}
	if first := cases[0]; first.placement != "clustered" || first.count != 1 || first.set != 1 {
		t.Fatalf("first case %+v", first)
	}
}

func TestFairDHTBenchmarkLimitsFitLargestNativeBatch(t *testing.T) {
	limits, err := fairDHTBenchmarkLimits(8)
	if err != nil {
		t.Fatal(err)
	}
	// scattered q=4096 needs about 53,846 distinct proof records (README).
	if limits.MaxRequests < 53_846 || limits.ProofBytes < 53_846*400 {
		t.Fatalf("limits too small: %+v", limits)
	}
	t.Setenv("FAIR_DHT_MAX_REQUESTS", "0")
	if _, err := fairDHTBenchmarkLimits(8); err == nil {
		t.Fatal("expected an invalid override to fail")
	}
}
