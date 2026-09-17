package confirm

import (
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/internal/spec"
	"delia/sig"
)

// TEST-21: receiver's latest serial is nu'-2 (i.e. two behind) -> it must
// sync before accepting.
func TestNeedsSyncDetectsGap(t *testing.T) {
	req, needed := NeedsSync(3, 5) // local latest 3, incoming nu' = 5: gap of one missing state (4)
	if !needed {
		t.Fatal("expected sync to be needed")
	}
	if req.FromSerial != 4 || req.ToSerial != 4 {
		t.Fatalf("got range [%d,%d], want [4,4]", req.FromSerial, req.ToSerial)
	}
}

func TestNeedsSyncNoGap(t *testing.T) {
	_, needed := NeedsSync(4, 5) // local latest == nu'-1: in sync
	if needed {
		t.Fatal("expected no sync needed when local latest == newSerial-1")
	}
}

func TestNeedsSyncLargerGap(t *testing.T) {
	req, needed := NeedsSync(2, 6)
	if !needed {
		t.Fatal("expected sync to be needed")
	}
	if req.FromSerial != 3 || req.ToSerial != 5 {
		t.Fatalf("got range [%d,%d], want [3,5]", req.FromSerial, req.ToSerial)
	}
}

func stateAt(nu uint64) globalstate.GlobalState {
	return globalstate.GlobalState{Nu: nu, Entries: []hash.Digest{{byte(nu)}}}
}

func signState(t *testing.T, kp *sig.KeyPair, g globalstate.GlobalState) []byte {
	t.Helper()
	digest := spec.SignedDigest(spec.PurposeSTATE, spec.RoleSigner, g.Encode())
	s, err := kp.Sign(digest)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

func TestVerifySyncResponseAcceptsValidContinuousRange(t *testing.T) {
	leader, err := sig.Gen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	req := SyncRequest{FromSerial: 3, ToSerial: 5}
	var entries []SyncResponseEntry
	for nu := req.FromSerial; nu <= req.ToSerial; nu++ {
		g := stateAt(nu)
		entries = append(entries, SyncResponseEntry{State: g, Signature: signState(t, leader, g)})
	}

	states, err := VerifySyncResponse(leader.PublicKey(), req, entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 3 || states[0].Nu != 3 || states[2].Nu != 5 {
		t.Fatalf("unexpected states: %+v", states)
	}
}

func TestVerifySyncResponseRejectsSerialGap(t *testing.T) {
	leader, _ := sig.Gen()
	req := SyncRequest{FromSerial: 3, ToSerial: 4}
	entries := []SyncResponseEntry{
		{State: stateAt(3), Signature: signState(t, leader, stateAt(3))},
		{State: stateAt(5), Signature: signState(t, leader, stateAt(5))}, // gap: should be 4
	}
	if _, err := VerifySyncResponse(leader.PublicKey(), req, entries); err == nil {
		t.Fatal("expected an error for a serial continuity gap")
	}
}

func TestVerifySyncResponseRejectsBadSignature(t *testing.T) {
	leader, _ := sig.Gen()
	impostor, _ := sig.Gen()
	req := SyncRequest{FromSerial: 3, ToSerial: 3}
	entries := []SyncResponseEntry{
		{State: stateAt(3), Signature: signState(t, impostor, stateAt(3))}, // wrong signer
	}
	if _, err := VerifySyncResponse(leader.PublicKey(), req, entries); err == nil {
		t.Fatal("expected an error for a signature that does not verify against leaderPubKey")
	}
}

func TestVerifySyncResponseRejectsWrongCoverage(t *testing.T) {
	leader, _ := sig.Gen()
	req := SyncRequest{FromSerial: 3, ToSerial: 5}
	entries := []SyncResponseEntry{
		{State: stateAt(3), Signature: signState(t, leader, stateAt(3))},
	} // missing 4, 5
	if _, err := VerifySyncResponse(leader.PublicKey(), req, entries); err == nil {
		t.Fatal("expected an error when the response does not cover the full requested range")
	}
}
