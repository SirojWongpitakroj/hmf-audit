package confirm

import (
	"reflect"
	"sort"
	"testing"

	"delia/globalstate"
	"delia/hash"
	"delia/internal/spec"
	"delia/sig"
)

func signDigestForState(g globalstate.GlobalState) hash.Digest {
	return spec.SignedDigest(spec.PurposeSTATE, spec.RoleSigner, g.Encode())
}

// Independent tester-pass verification of TEST-17/17b against REAL
// secp256k1 public keys (not the developer's own single-byte stand-in keys),
// to confirm the "keys compared as fixed-width unsigned big-endian values of
// the M0 canonical encoding" claim actually holds for full 33-byte compressed
// keys, not just a construction that happens to work for one-byte inputs.
func TestRotationSequenceRealKeysExactVector(t *testing.T) {
	// Generate real keys, then sort a copy independently with bytes-wise
	// comparison to know what "K1 < K2 < K3" should mean for these actual
	// public keys before ever calling the package under test.
	var pks []PublicKey
	for i := 0; i < 3; i++ {
		kp, err := sig.Gen()
		if err != nil {
			t.Fatalf("keygen: %v", err)
		}
		pks = append(pks, PublicKey(kp.PublicKey()))
	}
	sorted := make([]PublicKey, len(pks))
	copy(sorted, pks)
	sort.Slice(sorted, func(i, j int) bool {
		for k := 0; k < len(sorted[i]); k++ {
			if sorted[i][k] != sorted[j][k] {
				return sorted[i][k] < sorted[j][k]
			}
		}
		return false
	})
	k1, k2, k3 := sorted[0], sorted[1], sorted[2]

	// Feed P in an arbitrary (non-sorted) order, as a real domain config
	// would not necessarily be pre-sorted.
	p := []PublicKey{k3, k1, k2}
	got, err := RotationSequence(p, nil, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []PublicKey{k1, k2, k3, k1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %x %x %x %x, want K1,K2,K3,K1 vector", got[0], got[1], got[2], got[3])
	}
}

// Distinguishes the two OffList cases directly against Eligible (not just
// RotationSequence), using real keys: a FINALIZED OffList entry excludes a
// participant, but the exact same key supplied through a hypothetical
// "pending" channel (i.e. simply never placed in finalizedOffList) does not.
// This directly probes that Eligible's signature gives pending state no
// place to influence the result at all.
func TestEligibleFinalizedVsPendingAreGenuinelyDistinct(t *testing.T) {
	var keys []PublicKey
	for i := 0; i < 3; i++ {
		kp, err := sig.Gen()
		if err != nil {
			t.Fatalf("keygen: %v", err)
		}
		keys = append(keys, PublicKey(kp.PublicKey()))
	}
	p := []PublicKey{keys[0], keys[1], keys[2]}

	// Case A: keys[1] finalized-offline -> excluded.
	eligibleFinalized := Eligible(p, []PublicKey{keys[1]})
	for _, e := range eligibleFinalized {
		if reflect.DeepEqual(e, keys[1]) {
			t.Fatal("expected a finalized OffList member to be excluded from eligibility")
		}
	}
	if len(eligibleFinalized) != 2 {
		t.Fatalf("expected exactly 2 eligible participants when one is finalized-offline, got %d", len(eligibleFinalized))
	}

	// Case B: no finalized OffList at all (keys[1] is merely "pending" in the
	// caller's own bookkeeping, which this function is never told about) ->
	// keys[1] is fully eligible.
	eligiblePending := Eligible(p, nil)
	found := false
	for _, e := range eligiblePending {
		if reflect.DeepEqual(e, keys[1]) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected a merely-pending (non-finalized) offline participant to remain eligible")
	}
	if len(eligiblePending) != 3 {
		t.Fatalf("expected all 3 participants eligible when OffList is not finalized, got %d", len(eligiblePending))
	}
}

// Multiple simultaneously-offline finalized participants: rotation should
// skip all of them, still wrapping correctly.
func TestRotationSequenceMultipleFinalizedOffline(t *testing.T) {
	p := keys(0x01, 0x02, 0x03, 0x04)
	finalized := keys(0x02, 0x03) // K2, K3 offline
	got, err := RotationSequence(p, finalized, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := keys(0x01, 0x04, 0x01, 0x04)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// NextLeader directly: wrap-around must restart at the minimum, not error
// and not stay stuck on the maximum.
func TestNextLeaderWrapsAtMaximum(t *testing.T) {
	eligible := keys(0x01, 0x02, 0x03)
	next, err := NextLeader(eligible, PublicKey{0x03}) // previous leader was the max
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(next, PublicKey{0x01}) {
		t.Fatalf("got %v, want wrap to minimum K1", next)
	}
}

// Independent structural check on CONFIRM-016/017's "direct leader only, no
// recursion to a higher leader" claim: reflect over SyncRequest's own field
// set and confirm there genuinely is no parent-leader / hop-count / higher
// leader field anywhere for a caller to even populate, rather than trusting
// the doc comment's assertion.
func TestSyncRequestHasNoParentLeaderOrHopCountField(t *testing.T) {
	typ := reflect.TypeOf(SyncRequest{})
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if name != "FromSerial" && name != "ToSerial" {
			t.Fatalf("unexpected field %q on SyncRequest -- CONFIRM-016/017 claims this type structurally cannot express recursion to a higher leader", name)
		}
	}
	if typ.NumField() != 2 {
		t.Fatalf("expected exactly 2 fields on SyncRequest, got %d", typ.NumField())
	}
}

// Independent end-to-end exercise of NeedsSync + VerifySyncResponse together
// (not just each in isolation, as the existing sync_test.go does): a
// receiver 3 states behind detects the gap, requests exactly that range from
// its leader, receives a genuinely signed, continuous response, and adopts
// it -- using a freshly generated real secp256k1 leader key, not a
// stand-in.
func TestDesyncRecoveryEndToEndWithRealLeaderKey(t *testing.T) {
	leader, err := sig.Gen()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	const localLatest = uint64(10)
	const incomingNu = uint64(14) // 3 states behind: missing 11, 12, 13
	req, needed := NeedsSync(localLatest, incomingNu)
	if !needed {
		t.Fatal("expected sync to be needed")
	}
	if req.FromSerial != 11 || req.ToSerial != 13 {
		t.Fatalf("got range [%d,%d], want [11,13]", req.FromSerial, req.ToSerial)
	}

	var entries []SyncResponseEntry
	for nu := req.FromSerial; nu <= req.ToSerial; nu++ {
		g := globalstate.GlobalState{Nu: nu, Entries: []hash.Digest{{byte(nu)}}}
		digest := signDigestForState(g)
		s, err := leader.Sign(digest)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		entries = append(entries, SyncResponseEntry{State: g, Signature: s})
	}
	states, err := VerifySyncResponse(leader.PublicKey(), req, entries)
	if err != nil {
		t.Fatalf("unexpected error adopting a genuinely valid response: %v", err)
	}
	if len(states) != 3 {
		t.Fatalf("got %d states, want 3", len(states))
	}
}
