package confirm

import (
	"reflect"
	"testing"
)

func keys(vals ...byte) []PublicKey {
	out := make([]PublicKey, len(vals))
	for i, v := range vals {
		out[i] = PublicKey{v}
	}
	return out
}

// TEST-17: P = {K1 < K2 < K3}; 4 rounds -> leaders K1, K2, K3, K1.
func TestRotationSequenceBasic(t *testing.T) {
	p := keys(0x01, 0x02, 0x03) // K1, K2, K3
	got, err := RotationSequence(p, nil, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := keys(0x01, 0x02, 0x03, 0x01)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TEST-17: rotation is independent of P's input order (keys are compared,
// not positionally ordered).
func TestRotationSequenceOrderIndependent(t *testing.T) {
	p := keys(0x03, 0x01, 0x02) // supplied out of order
	got, err := RotationSequence(p, nil, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := keys(0x01, 0x02, 0x03, 0x01)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TEST-17b(a): K2 in the finalized OffList, 3 rounds -> leaders K1, K3, K1.
func TestRotationSequenceFinalizedOffListSkipped(t *testing.T) {
	p := keys(0x01, 0x02, 0x03)
	finalized := keys(0x02) // K2 finalized offline
	got, err := RotationSequence(p, finalized, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := keys(0x01, 0x03, 0x01)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TEST-17b(b): only a pending, unfinalized OfflineMsg for K2 (never passed
// as finalizedOffList) -> leaders K1, K2, K3, because pending requests must
// not affect eligibility (GAP-79).
func TestRotationSequencePendingOfflineDoesNotExclude(t *testing.T) {
	p := keys(0x01, 0x02, 0x03)
	// The caller simply never puts a merely-pending offline participant
	// into finalizedOffList — there is no separate "pending" parameter to
	// this function at all, which is the point: pending state cannot leak
	// into eligibility because this API gives it nowhere to go.
	got, err := RotationSequence(p, nil, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := keys(0x01, 0x02, 0x03)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// CONFIRM-001/002: at most one leader at a time (trivial from the function
// signature: NextLeader returns exactly one PublicKey), and equal
// opportunity over |P| consecutive rounds with no OffList members.
func TestEqualOpportunityOverFullCycle(t *testing.T) {
	p := keys(0x05, 0x01, 0x09, 0x03)
	got, err := RotationSequence(p, nil, len(p))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	seen := map[byte]bool{}
	for _, k := range got {
		seen[k[0]] = true
	}
	if len(seen) != len(p) {
		t.Fatalf("expected every participant to lead exactly once over a full cycle, got %v", got)
	}
}

func TestEligibleEmptyErrors(t *testing.T) {
	p := keys(0x01)
	if _, err := RotationSequence(p, keys(0x01), 1); err == nil {
		t.Fatal("expected an error when every participant is offline")
	}
}
