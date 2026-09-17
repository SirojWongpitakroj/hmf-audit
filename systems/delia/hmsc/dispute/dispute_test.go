package dispute

import "testing"

// CONFIRM-020: any participant can invoke the manual dispute trigger; there
// is no leader-only restriction and no automatic-detection code path (that
// property is structural — this package contains no periodic/background
// trigger anywhere, confirmed by code review: the only way TriggerDispute
// values are produced is this explicit constructor).
func TestTriggerDisputeAnyParticipant(t *testing.T) {
	leaderKey := []byte{0x01}
	ordinaryKey := []byte{0x02}

	td1 := NewTriggerDispute(leaderKey, 4)
	td2 := NewTriggerDispute(ordinaryKey, 4)

	if string(td1.Caller) == string(td2.Caller) {
		t.Fatal("expected distinct callers to be representable")
	}
	if td1.Checkpoint != 4 || td2.Checkpoint != 4 {
		t.Fatal("checkpoint not carried through")
	}
}

// TEST-30, Fig. 9(a)-shaped scenario: no children, signed states exist at
// several serials; the checkpoint is the latest one (prose semantics,
// DEV-02) — illustrative vectors matching DISPUTE-009's stated rule, since
// the paper's own Fig. 9 pixel content is not available as text.
func TestCheckpointSelectionNoChildrenLatestSigned(t *testing.T) {
	got, ok := CheckpointSelection([]uint64{1, 2, 3, 4})
	if !ok {
		t.Fatal("expected a checkpoint to be found")
	}
	if got != 4 {
		t.Fatalf("got checkpoint %d, want 4", got)
	}
}

// A second, differently-shaped no-children sequence (unordered input,
// non-contiguous), to confirm the rule is "the maximum signed serial", not
// an assumption about contiguity or input order.
func TestCheckpointSelectionNoChildrenUnorderedNonContiguous(t *testing.T) {
	got, ok := CheckpointSelection([]uint64{7, 2, 5})
	if !ok {
		t.Fatal("expected a checkpoint to be found")
	}
	if got != 7 {
		t.Fatalf("got checkpoint %d, want 7", got)
	}
}

func TestCheckpointSelectionEmptyHistory(t *testing.T) {
	_, ok := CheckpointSelection(nil)
	if ok {
		t.Fatal("expected ok=false for an empty signed-state history")
	}
}
