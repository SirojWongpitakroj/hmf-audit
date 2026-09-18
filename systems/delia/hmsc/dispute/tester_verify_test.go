package dispute

import (
	"reflect"
	"testing"
)

// Independent tester-pass check: CheckpointSelection must implement the
// PROSE semantics (DEV-02) -- "the checkpoint is Gamma_x's latest signed
// round state" -- and must NOT reproduce Alg. 4's literal listing defect
// (lines 3-8), which decrements k BEFORE testing for existence. If the
// literal off-by-one bug were reproduced, a caller whose highest signed
// serial is exactly `nu` (here, the max value in the supplied history) would
// get back nu-1 instead of nu, because the literal listing starts by
// decrementing nu at least once before ever checking it. Assert directly
// that supplying a history whose maximum entry is a specific, deliberately
// "landmark" serial returns THAT exact serial, not one less.
func TestCheckpointSelectionDoesNotReproduceLiteralOffByOne(t *testing.T) {
	// History has a signed state at serial 9 (the landmark / "nu") and
	// earlier ones. Prose rule: checkpoint = 9. Literal decrement-before-test
	// listing (if it were wrongly reproduced starting from k=nu=9) would
	// decrement to 8 before its first existence check, and since 8 is not in
	// the history, would keep decrementing until landing on some lower value
	// -- never returning 9 itself.
	got, ok := CheckpointSelection([]uint64{9, 5, 1})
	if !ok {
		t.Fatal("expected a checkpoint to be found")
	}
	if got != 9 {
		t.Fatalf("got checkpoint %d, want 9 (prose: latest signed round state) -- got %d instead suggests the literal off-by-one decrement-before-test bug leaked back in", got, got)
	}
}

// A single-entry history: the checkpoint must be that entry itself, not
// "one before it" and not zero.
func TestCheckpointSelectionSingleEntry(t *testing.T) {
	got, ok := CheckpointSelection([]uint64{42})
	if !ok {
		t.Fatal("expected a checkpoint to be found")
	}
	if got != 42 {
		t.Fatalf("got %d, want 42", got)
	}
}

// Duplicate entries in the retained history must not confuse the max-finding
// logic.
func TestCheckpointSelectionDuplicateEntries(t *testing.T) {
	got, ok := CheckpointSelection([]uint64{3, 3, 3})
	if !ok {
		t.Fatal("expected a checkpoint to be found")
	}
	if got != 3 {
		t.Fatalf("got %d, want 3", got)
	}
}

// Zero is a valid serial value in principle (though G_0 itself is a
// sentinel elsewhere in the system) -- CheckpointSelection takes a bare
// []uint64 with no special-casing, so a single zero entry must round-trip
// as "checkpoint 0, found" rather than being conflated with the
// not-found (0, false) case.
func TestCheckpointSelectionSingleZeroEntryIsFoundNotConflatedWithEmpty(t *testing.T) {
	got, ok := CheckpointSelection([]uint64{0})
	if !ok {
		t.Fatal("expected ok=true for a history containing a single entry, even if that entry is 0")
	}
	if got != 0 {
		t.Fatalf("got %d, want 0", got)
	}
}

// CONFIRM-020: structural check that TriggerDispute carries no leader/role
// field at all -- a caller cannot even express "only the leader may trigger"
// through this type, confirming "any participant" isn't just unenforced by
// convention but has nowhere to encode a restriction.
func TestTriggerDisputeStructHasNoRoleRestrictionField(t *testing.T) {
	typ := reflect.TypeOf(TriggerDispute{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if name != "Caller" && name != "Checkpoint" {
			t.Fatalf("unexpected field %q on TriggerDispute -- would this let a role restriction leak in?", name)
		}
	}
}

// An "ordinary" (non-leader-looking) key can trigger a dispute against a
// checkpoint of 0 (a domain that has never produced a signed round state) --
// the constructor applies no validation at all, matching "any participant,
// no automatic detection threshold" literally (nothing about the checkpoint
// value is checked here; Alg. 4's own CheckpointSelection is what would
// reject an empty history, separately).
func TestNewTriggerDisputeAcceptsZeroCheckpoint(t *testing.T) {
	td := NewTriggerDispute([]byte{0xAB, 0xCD}, 0)
	if td.Checkpoint != 0 {
		t.Fatalf("got checkpoint %d, want 0", td.Checkpoint)
	}
}
