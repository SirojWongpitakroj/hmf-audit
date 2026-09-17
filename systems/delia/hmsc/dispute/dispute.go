// Package dispute implements the Tier-A slices of the dispute-resolving
// client flow M7/M6 of TRACKER.md license now: a manual dispute-trigger
// entry point (CONFIRM-020) and Alg. 4's CheckpointSelection for the
// no-children case only (DISPUTE-005/009, TEST-30).
//
// The full dispute flow — DisputeMsg submission and on-chain checkpoint
// verification, voting, exclusion, NewLeader assignment beyond the isolated
// selectNewLeader sub-computation (contracts/HMSC.sol), rollback — stays
// out of scope (DISPUTE-001..004/006..008/010/011/013..019, Tier B, GAP-88
// residual).
package dispute

// TriggerDispute is CONFIRM-020's manual entry point: "any participant that
// finds an abnormal situation can call DisputeResolving". There is no
// automatic detection threshold anywhere in this reproduction (GAP-40(b));
// this function's only job is to record who explicitly triggered it and
// against which checkpoint their own Alg. 4 run selected — it does not
// itself execute the (still-blocked) on-chain DisputeMsg flow.
type TriggerDispute struct {
	Caller     []byte // the calling participant's canonical public key (GAP-73)
	Checkpoint uint64 // the serial this caller's own CheckpointSelection run picked
}

// NewTriggerDispute constructs a manual dispute trigger. Any participant's
// key may be supplied — this function itself enforces no leader-only or
// other role restriction, matching CONFIRM-020's "any participant".
func NewTriggerDispute(caller []byte, checkpoint uint64) TriggerDispute {
	return TriggerDispute{Caller: caller, Checkpoint: checkpoint}
}

// CheckpointSelection implements Alg. 4's checkpoint selection for the
// no-children case only (ChildList_x == null, DISPUTE-005/009, TEST-30),
// following the PROSE semantics (DEV-02), not the literal off-by-one lines
// 3-8: "the checkpoint is Gamma_x's latest signed round state." The literal
// listing's decrement-before-test is not reproduced (INC-07).
//
// signedSerials is the set of serials at which a signed round state exists
// in the retained off-chain signed-state history (GAP-64: looked up
// off-chain; SCC does not rerun Alg. 4 and keeps no historical state arrays
// for it, GAP-35/GAP-15). It returns (0, false) if the set is empty (no
// signed round state exists yet — not a case any TEST-30 vector exercises,
// but callers must not treat this as "checkpoint 0").
func CheckpointSelection(signedSerials []uint64) (uint64, bool) {
	if len(signedSerials) == 0 {
		return 0, false
	}
	max := signedSerials[0]
	for _, s := range signedSerials[1:] {
		if s > max {
			max = s
		}
	}
	return max, true
}
