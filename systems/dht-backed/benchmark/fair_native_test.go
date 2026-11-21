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

func TestFairNativeDHTBatchesFromHMFAuditQueries(t *testing.T) {
	t.Setenv("FAIR_BATCHES_FROM_HMF", "1")
	dir := filepath.Join("..", "..", "experiments", "native-queries", "10000000-skewed")
	batches, cases, err := fairNativeDHTBatches(dir, 10_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != len(fairNativeShapes)*len(fairDHTRequestCounts)*fairNativeSets {
		t.Fatalf("got %d cases", len(cases))
	}
	for _, c := range cases {
		ids := batches[c]
		if len(ids) != c.count {
			t.Fatalf("%+v has %d IDs", c, len(ids))
		}
		// The records are taken in ascending ordinal order, as the other schemes get them.
		for i := 1; i < len(ids); i++ {
			if ids[i-1] >= ids[i] {
				t.Fatalf("%+v is not in ascending record order at %d", c, i)
			}
		}
	}
	// Without the switch the DHT-backed batches of dht_evseb_queries.json are used again,
	// and a dataset of another size is still refused.
	t.Setenv("FAIR_BATCHES_FROM_HMF", "")
	if _, _, err := fairNativeDHTBatches(filepath.Join("..", "..", "experiments", "native-queries", "10000000"), 100_000); err == nil {
		t.Fatal("a record-count mismatch was accepted")
	}
}
