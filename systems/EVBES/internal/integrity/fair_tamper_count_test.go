package integrity

import (
	"fmt"
	"testing"
)

func TestFairEVBESTamperedCandidatesUseStratumCenters(t *testing.T) {
	candidates := make([]string, 16)
	for index := range candidates {
		candidates[index] = fmt.Sprintf("log-%d", index)
	}

	tampered, err := fairEVBESTamperedCandidates(candidates, 4)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"log-2", "log-6", "log-10", "log-14"}
	for index := range want {
		if tampered[index] != want[index] {
			t.Fatalf("tampered[%d] = %q, want %q", index, tampered[index], want[index])
		}
	}
}

func TestFairEVBESTamperedCandidatesRejectInvalidCount(t *testing.T) {
	if _, err := fairEVBESTamperedCandidates(make([]string, 8), 9); err == nil {
		t.Fatal("expected tamper count larger than batch to fail")
	}
}

func TestFairEVBESClusteredTwoSupportsMaximumRequestCount(t *testing.T) {
	candidates := fairEVBESCandidates(20_000, 8192, "clustered-2", 16384)
	if len(candidates) != 16384 || candidates[0] != fairEVBESLID(0) ||
		candidates[len(candidates)-1] != fairEVBESLID(16383) {
		t.Fatalf("unexpected q=16384 endpoints: first=%q last=%q",
			candidates[0], candidates[len(candidates)-1])
	}
}
