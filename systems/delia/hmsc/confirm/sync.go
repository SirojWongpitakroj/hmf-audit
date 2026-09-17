package confirm

import (
	"errors"

	"delia/globalstate"
	"delia/internal/spec"
	"delia/sig"
)

// SyncRequest is CONFIRM-016/017's desync recovery request (NET-013,
// GAP-46): a receiver whose local latest serial does not equal nu'-1 sends
// (fromSerial, toSerial) to its DIRECT leader only — never recursing to a
// higher leader (that recursion is exactly what this type's shape refuses
// to express: there is no parent-leader field or hop count anywhere in this
// package).
type SyncRequest struct {
	FromSerial uint64
	ToSerial   uint64
}

// NeedsSync implements CONFIRM-016's serial check: a receiver's local
// latest serial must equal nu'-1. If it does not, the receiver is
// unsynchronized and must request (localLatest+1 .. newSerial-1) from its
// leader before adopting newSerial (TEST-21).
func NeedsSync(localLatest uint64, newSerial uint64) (SyncRequest, bool) {
	if newSerial == 0 || localLatest >= newSerial-1 {
		// Already synchronized (localLatest == newSerial-1), or already at
		// or ahead of newSerial: not a desync-forward case this function
		// handles; callers should not be adopting an equal/older serial in
		// the first place (Rule-1-shaped situation, out of scope here).
		return SyncRequest{}, false
	}
	return SyncRequest{FromSerial: localLatest + 1, ToSerial: newSerial - 1}, true
}

// SyncResponseEntry is one state the direct leader returns to satisfy a
// SyncRequest: the GlobalState plus the leader's signature over it.
//
// Implementation note (small, documented ambiguity call, not a protocol
// gap): the SRS does not pin down which purpose byte the leader's signature
// on a resynchronization entry carries. These entries are prior signed
// round states the leader is relaying, not the still-open no-signed-state
// signature purpose (GAP-86 covers that one, CONFIRM-015/017), so this
// package verifies them under the already-frozen PurposeSTATE/RoleSigner
// pair — the same pair a signed round state's own Update submission uses.
// If a future milestone's ruling says otherwise, only this one constant
// choice needs to change.
type SyncResponseEntry struct {
	State     globalstate.GlobalState
	Signature []byte
}

// VerifySyncResponse implements CONFIRM-016/017's receiver-side check
// (TEST-21): the returned entries must exactly cover req's serial range,
// consecutively (serial continuity), and every entry's signature must
// verify against leaderPubKey. It returns the verified states in order, or
// an error on the first entry that fails either check — nothing is adopted
// on a partial/invalid response.
func VerifySyncResponse(leaderPubKey []byte, req SyncRequest, entries []SyncResponseEntry) ([]globalstate.GlobalState, error) {
	want := req.FromSerial
	if req.ToSerial < req.FromSerial {
		return nil, errors.New("confirm: empty sync range")
	}
	expectedCount := req.ToSerial - req.FromSerial + 1
	if uint64(len(entries)) != expectedCount {
		return nil, errors.New("confirm: sync response does not cover the requested range")
	}
	out := make([]globalstate.GlobalState, 0, len(entries))
	for _, e := range entries {
		if e.State.Nu != want {
			return nil, errors.New("confirm: sync response serial continuity broken")
		}
		digest := spec.SignedDigest(spec.PurposeSTATE, spec.RoleSigner, e.State.Encode())
		if !sig.Vrfy(leaderPubKey, digest, e.Signature) {
			return nil, errors.New("confirm: sync response signature invalid")
		}
		out = append(out, e.State)
		want++
	}
	return out, nil
}
