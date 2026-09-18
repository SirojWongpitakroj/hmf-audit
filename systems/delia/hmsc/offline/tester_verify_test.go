package offline

import (
	"testing"

	"delia/hash"
	"delia/sig"
)

// Independent tester-pass check of TEST-48's exact n-1/n-2 boundary, with a
// LARGER domain (n=6) than the developer's own n=4 vectors, and checking one
// value on each side of the boundary in the SAME test to directly confirm
// the accept/reject line sits exactly between n-2 and n-1, not elsewhere.
func TestCheckOfflineMsgSignersExactBoundaryLargerDomain(t *testing.T) {
	n := 6
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
	digest := hash.H([]byte("boundary-digest"))

	full := func(skip int) []SignerEntry {
		var out []SignerEntry
		count := 0
		for i, kp := range keys {
			if i == 0 {
				continue // pi
			}
			if count == skip {
				count++
				continue // drop this one extra signer for the n-2 case
			}
			count++
			s, err := kp.Sign(digest)
			if err != nil {
				t.Fatalf("sign: %v", err)
			}
			out = append(out, SignerEntry{PubKey: pubs[i], Signature: s})
		}
		return out
	}

	// n-1 (5 signers, all non-pi members): accepted.
	if !CheckOfflineMsgSigners(pubs, pi, digest, full(-1)) {
		t.Fatal("expected exactly n-1 (5) valid signatures to be accepted")
	}
	// n-2 (4 signers, one non-pi member missing): rejected.
	if CheckOfflineMsgSigners(pubs, pi, digest, full(0)) {
		t.Fatal("expected n-2 (4) signatures to be rejected")
	}
}

// A domain of exactly n=1 (pi is the only participant): the expected signer
// set is empty, so an empty signer list is accepted -- this is a natural
// consequence of "n-1 signers excluding pi" applied at the boundary, not a
// separately-built special case; confirmed directly rather than assumed.
func TestCheckOfflineMsgSignersSingleParticipantDomain(t *testing.T) {
	kp, err := sig.Gen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	pi := kp.PublicKey()
	digest := hash.H([]byte("solo"))
	if !CheckOfflineMsgSigners([][]byte{pi}, pi, digest, nil) {
		t.Fatal("expected an empty signer set to be accepted when pi is the domain's only participant")
	}
}

// A signer that is valid and expected, but ALSO signs a second time under a
// different (bad) signature for the same key, must not let the good
// signature "cover" for padding out the count -- exercised as n+1 entries
// where one PubKey appears twice (once good, once bad).
func TestCheckOfflineMsgSignersRejectsExtraEntryEvenIfOtherwiseComplete(t *testing.T) {
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
	sig1, _ := keys[1].Sign(digest)
	sig2, _ := keys[2].Sign(digest)
	// Complete, correct set (2 signers for n=3) PLUS one duplicate entry for
	// keys[1] -- total length 3 != expected count 2.
	signers := []SignerEntry{
		{PubKey: pubs[1], Signature: sig1},
		{PubKey: pubs[2], Signature: sig2},
		{PubKey: pubs[1], Signature: sig1}, // duplicate, extra
	}
	if CheckOfflineMsgSigners(pubs, pi, digest, signers) {
		t.Fatal("expected a signer set padded with an extra duplicate entry to be rejected")
	}
}
