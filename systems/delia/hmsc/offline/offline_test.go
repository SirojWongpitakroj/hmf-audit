package offline

import (
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/internal/spec"
	"delia/sig"
)

func TestHashActiveQueueDeterministicAndOrderSensitive(t *testing.T) {
	psi := []hash.Digest{hash.H([]byte("a")), hash.H([]byte("b")), hash.H([]byte("c"))}
	got1 := HashActiveQueue(psi)
	got2 := HashActiveQueue(psi)
	if got1 != got2 {
		t.Fatal("expected HashActiveQueue to be deterministic")
	}

	reordered := []hash.Digest{psi[1], psi[0], psi[2]}
	if HashActiveQueue(reordered) == got1 {
		t.Fatal("expected record order to affect the hash")
	}
}

func TestHashActiveQueueIncludesPartialBatch(t *testing.T) {
	// A "partial batch" is just whatever digests are currently active —
	// this function takes no theta/completeness parameter at all, so a
	// 2-element partial batch hashes differently from its 1-element prefix,
	// confirming nothing is dropped/truncated to only complete batches.
	full := []hash.Digest{hash.H([]byte("x")), hash.H([]byte("y"))}
	partial := full[:1]
	if HashActiveQueue(full) == HashActiveQueue(partial) {
		t.Fatal("expected the partial-batch digest to differ from the full one")
	}
}

func mustSign(t *testing.T, kp *sig.KeyPair, digest hash.Digest) []byte {
	t.Helper()
	s, err := kp.Sign(digest)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return s
}

// TEST-48(a): exactly n-1 valid signatures (excluding pi) accepted.
func TestCheckOfflineMsgSignersAcceptsExactlyNMinus1(t *testing.T) {
	n := 4
	keys := make([]*sig.KeyPair, n)
	pubs := make([][]byte, n)
	for i := range keys {
		kp, err := sig.Gen()
		if err != nil {
			t.Fatalf("keygen: %v", err)
		}
		keys[i] = kp
		pubs[i] = kp.PublicKey()
	}
	pi := pubs[1] // pending-offline participant, excluded from expected signers
	digest := hash.H([]byte("offline-digest"))

	var signers []SignerEntry
	for i, kp := range keys {
		if i == 1 {
			continue // pi does not sign its own OfflineMsg
		}
		signers = append(signers, SignerEntry{PubKey: pubs[i], Signature: mustSign(t, kp, digest)})
	}

	if !CheckOfflineMsgSigners(pubs, pi, digest, signers) {
		t.Fatal("expected exactly n-1 valid signatures (excluding pi) to be accepted")
	}
}

// TEST-48(b): n-2 signatures rejected.
func TestCheckOfflineMsgSignersRejectsNMinus2(t *testing.T) {
	n := 4
	keys := make([]*sig.KeyPair, n)
	pubs := make([][]byte, n)
	for i := range keys {
		kp, err := sig.Gen()
		if err != nil {
			t.Fatalf("keygen: %v", err)
		}
		keys[i] = kp
		pubs[i] = kp.PublicKey()
	}
	pi := pubs[1]
	digest := hash.H([]byte("offline-digest"))

	var signers []SignerEntry
	skipped := false
	for i, kp := range keys {
		if i == 1 {
			continue // pi excluded
		}
		if !skipped {
			skipped = true
			continue // drop one more valid signer -> n-2 total
		}
		signers = append(signers, SignerEntry{PubKey: pubs[i], Signature: mustSign(t, kp, digest)})
	}

	if CheckOfflineMsgSigners(pubs, pi, digest, signers) {
		t.Fatal("expected n-2 signatures to be rejected")
	}
}

func TestCheckOfflineMsgSignersRejectsDuplicateSigner(t *testing.T) {
	n := 3
	keys := make([]*sig.KeyPair, n)
	pubs := make([][]byte, n)
	for i := range keys {
		kp, err := sig.Gen()
		if err != nil {
			t.Fatalf("keygen: %v", err)
		}
		keys[i] = kp
		pubs[i] = kp.PublicKey()
	}
	pi := pubs[0]
	digest := hash.H([]byte("d"))
	sigA := mustSign(t, keys[1], digest)
	signers := []SignerEntry{
		{PubKey: pubs[1], Signature: sigA},
		{PubKey: pubs[1], Signature: sigA}, // same signer twice, not covering pubs[2]
	}
	if CheckOfflineMsgSigners(pubs, pi, digest, signers) {
		t.Fatal("expected a duplicate signer (not covering the full expected set) to be rejected")
	}
}

func TestCheckOfflineMsgSignersRejectsBadSignature(t *testing.T) {
	kp1, _ := sig.Gen()
	kp2, _ := sig.Gen()
	pubs := [][]byte{kp1.PublicKey(), kp2.PublicKey()}
	pi := []byte{0xFF} // not a member of p at all; expected set = both of pubs
	digest := hash.H([]byte("d"))
	badSig := mustSign(t, kp1, hash.H([]byte("different digest")))
	signers := []SignerEntry{
		{PubKey: pubs[0], Signature: badSig}, // signs the wrong digest
		{PubKey: pubs[1], Signature: mustSign(t, kp2, digest)},
	}
	if CheckOfflineMsgSigners(pubs, pi, digest, signers) {
		t.Fatal("expected an invalid signature to be rejected")
	}
}

func stateAt(nu uint64) globalstate.GlobalState {
	return globalstate.GlobalState{Nu: nu, Entries: []hash.Digest{{byte(nu)}}}
}

func TestVerifyRejoinSyncAcceptsValidRange(t *testing.T) {
	leader, _ := sig.Gen()
	req := RejoinSyncRequest{FromSerial: 1, ToSerial: 3}
	var entries []RejoinStateEntry
	for nu := req.FromSerial; nu <= req.ToSerial; nu++ {
		g := stateAt(nu)
		digest := spec.SignedDigest(spec.PurposeSTATE, spec.RoleSigner, g.Encode())
		entries = append(entries, RejoinStateEntry{State: g, Signature: mustSign(t, leader, digest)})
	}
	resp := RejoinSyncResponse{
		States: entries,
		LogCacheQueue: map[string][]hash.Digest{
			"p1": {hash.H([]byte("r1"))},
		},
	}
	states, err := VerifyRejoinSync(leader.PublicKey(), req, resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("got %d states, want 3", len(states))
	}
}

func TestVerifyRejoinSyncRejectsWrongSigner(t *testing.T) {
	leader, _ := sig.Gen()
	impostor, _ := sig.Gen()
	req := RejoinSyncRequest{FromSerial: 1, ToSerial: 1}
	g := stateAt(1)
	digest := spec.SignedDigest(spec.PurposeSTATE, spec.RoleSigner, g.Encode())
	resp := RejoinSyncResponse{States: []RejoinStateEntry{{State: g, Signature: mustSign(t, impostor, digest)}}}
	if _, err := VerifyRejoinSync(leader.PublicKey(), req, resp); err == nil {
		t.Fatal("expected an error for a signature not from the direct leader")
	}
}
