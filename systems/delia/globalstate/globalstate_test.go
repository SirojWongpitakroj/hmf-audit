package globalstate

import (
	"bytes"
	"testing"

	"delia/hash"
	"delia/localstate"
	"delia/merkle"
)

func d(b byte) hash.Digest { return hash.H([]byte{b}) }

func TestEncodeDecodeRoundTrip(t *testing.T) {
	g := GlobalState{Nu: 7, Entries: []hash.Digest{d(1), d(2), d(3)}}
	enc := g.Encode()
	got, err := Decode(enc)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !got.Equal(g) {
		t.Fatalf("round trip mismatch: got %+v, want %+v", got, g)
	}
}

func TestEncodeLayout(t *testing.T) {
	g := GlobalState{Nu: 1, Entries: []hash.Digest{d(9)}}
	enc := g.Encode()
	if len(enc) != 8+4+hash.Size {
		t.Fatalf("unexpected encoded length %d", len(enc))
	}
	// nu big-endian
	if !bytes.Equal(enc[0:8], []byte{0, 0, 0, 0, 0, 0, 0, 1}) {
		t.Fatalf("nu not big-endian at offset 0: %x", enc[0:8])
	}
	// entry_count big-endian
	if !bytes.Equal(enc[8:12], []byte{0, 0, 0, 1}) {
		t.Fatalf("entry_count not big-endian at offset 8: %x", enc[8:12])
	}
	e := d(9)
	if !bytes.Equal(enc[12:12+hash.Size], e[:]) {
		t.Fatalf("entry bytes mismatch")
	}
}

func TestEqualComparesNu(t *testing.T) {
	// TEST-08(c): same entries, different nu -> not equal.
	a := GlobalState{Nu: 1, Entries: []hash.Digest{d(1), d(2), d(3)}}
	b := GlobalState{Nu: 2, Entries: []hash.Digest{d(1), d(2), d(3)}}
	if a.Equal(b) {
		t.Fatalf("states with different nu compared equal")
	}
}

func TestZeroState(t *testing.T) {
	g := Zero(3)
	if g.Nu != 0 {
		t.Fatalf("Zero: nu = %d, want 0", g.Nu)
	}
	if len(g.Entries) != 3 {
		t.Fatalf("Zero: %d entries, want 3", len(g.Entries))
	}
	for i, e := range g.Entries {
		if e != hash.Zero {
			t.Fatalf("Zero: entry %d not the zero sentinel", i)
		}
	}
}

// TEST-05: GlobalState generation (Alg. 1). p2 (index 1) completes a batch
// with li > 0; entries 0 and 2 are untouched, entry 1 = H(root||old entry1),
// serial = nu-1 + 1.
func TestGlobalGenChained(t *testing.T) {
	prev := GlobalState{Nu: 4, Entries: []hash.Digest{d(1), d(2), d(3)}}
	root := d(42)
	got, err := GlobalGen(root, prev, 1)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	if got.Nu != prev.Nu+1 {
		t.Fatalf("Nu = %d, want %d", got.Nu, prev.Nu+1)
	}
	if got.Entries[0] != prev.Entries[0] || got.Entries[2] != prev.Entries[2] {
		t.Fatalf("untouched entries changed: %+v", got.Entries)
	}
	oldE1 := prev.Entries[1]
	want := localstate.Compute(root, &oldE1)
	if got.Entries[1] != want {
		t.Fatalf("entry[1] = %x, want %x", got.Entries[1], want)
	}
}

// TEST-05b (ALG1-005/DEV-13): server slot is the zero digest (li = 0);
// entry i = SHA-256(root), not H(root || zero digest).
func TestGlobalGenFirstBatch(t *testing.T) {
	prev := Zero(3)
	root := d(7)
	got, err := GlobalGen(root, prev, 2)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	want := localstate.Compute(root, nil) // H(root) alone
	if got.Entries[2] != want {
		t.Fatalf("entry[2] = %x, want H(root) = %x", got.Entries[2], want)
	}
	// Sanity: this must differ from the (wrong) chained-with-zero-digest form.
	zero := hash.Zero
	wrongChained := localstate.Compute(root, &zero)
	if got.Entries[2] == wrongChained {
		t.Fatalf("first batch must not equal H(root||zero-digest)")
	}
	// Other entries stay the zero sentinel.
	if got.Entries[0] != hash.Zero || got.Entries[1] != hash.Zero {
		t.Fatalf("untouched entries not left as zero sentinel: %+v", got.Entries)
	}
}

func TestGlobalGenIndexOutOfRange(t *testing.T) {
	prev := Zero(3)
	if _, err := GlobalGen(d(1), prev, 3); err == nil {
		t.Fatalf("expected error for out-of-range index")
	}
	if _, err := GlobalGen(d(1), prev, -1); err == nil {
		t.Fatalf("expected error for negative index")
	}
}

func psiFor(bs ...byte) []hash.Digest {
	out := make([]hash.Digest, len(bs))
	for i, b := range bs {
		out[i] = d(b)
	}
	return out
}

// TEST-06: StateVerify success on honest inputs.
func TestStateVerifySuccess(t *testing.T) {
	prev := Zero(3)
	psi := psiFor(10, 11, 12)
	root := merkle.MHTGen(psi)
	next, err := GlobalGen(root, prev, 0)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	if !StateVerify(psi, root, prev, next, 0) {
		t.Fatalf("StateVerify returned false on honest inputs")
	}
}

// TEST-07: StateVerify invalid root -> 0, and GlobalGen is not invoked.
func TestStateVerifyInvalidRoot(t *testing.T) {
	prev := Zero(3)
	psi := psiFor(10, 11, 12)
	realRoot := merkle.MHTGen(psi)
	next, _ := GlobalGen(realRoot, prev, 0)

	badRoot := d(255)

	mhtCalls := 0
	origMHT := mhtGen
	mhtGen = func(leaves []hash.Digest) hash.Digest {
		mhtCalls++
		return origMHT(leaves)
	}
	defer func() { mhtGen = origMHT }()

	genCalls := 0
	origGen := globalGenFn
	globalGenFn = func(root hash.Digest, prev GlobalState, i int) (GlobalState, error) {
		genCalls++
		return origGen(root, prev, i)
	}
	defer func() { globalGenFn = origGen }()

	if StateVerify(psi, badRoot, prev, next, 0) {
		t.Fatalf("StateVerify accepted a mismatching root")
	}
	if mhtCalls != 1 {
		t.Fatalf("MHTGen call count = %d, want 1 (recomputing root')", mhtCalls)
	}
	if genCalls != 0 {
		t.Fatalf("GlobalGen call count = %d, want 0: GlobalGen must not run after a root mismatch", genCalls)
	}
}

// TEST-08(a): G_nu entry i altered -> 0.
func TestStateVerifyInvalidGlobalStateOwnEntry(t *testing.T) {
	prev := Zero(3)
	psi := psiFor(10, 11, 12)
	root := merkle.MHTGen(psi)
	next, _ := GlobalGen(root, prev, 0)
	tampered := next.Clone()
	tampered.Entries[0] = d(250)
	if StateVerify(psi, root, prev, tampered, 0) {
		t.Fatalf("StateVerify accepted a tampered own entry")
	}
}

// TEST-08(b): another server's entry altered -> 0.
func TestStateVerifyInvalidGlobalStateOtherEntry(t *testing.T) {
	prev := Zero(3)
	psi := psiFor(10, 11, 12)
	root := merkle.MHTGen(psi)
	next, _ := GlobalGen(root, prev, 0)
	tampered := next.Clone()
	tampered.Entries[2] = d(251)
	if StateVerify(psi, root, prev, tampered, 0) {
		t.Fatalf("StateVerify accepted a tampered other-server entry")
	}
}

// TEST-08(c): same entries, different nu -> 0.
func TestStateVerifyInvalidGlobalStateNu(t *testing.T) {
	prev := Zero(3)
	psi := psiFor(10, 11, 12)
	root := merkle.MHTGen(psi)
	next, _ := GlobalGen(root, prev, 0)
	tampered := next.Clone()
	tampered.Nu = next.Nu + 100
	if StateVerify(psi, root, prev, tampered, 0) {
		t.Fatalf("StateVerify accepted a state with a tampered nu")
	}
}

func TestStateVerifyUsesGlobalGenAsSubroutine(t *testing.T) {
	// ALG2-004: implemented by calling Alg. 1, not a separate reimplementation.
	// We check this indirectly: the li=0 branch behavior (zero-sentinel
	// detection) must be visible through StateVerify too.
	prev := Zero(2)
	psi := psiFor(1, 2)
	root := merkle.MHTGen(psi)
	next, err := GlobalGen(root, prev, 1)
	if err != nil {
		t.Fatalf("GlobalGen: %v", err)
	}
	if next.Entries[1] != localstate.Compute(root, nil) {
		t.Fatalf("GlobalGen did not take the li=0 branch")
	}
	if !StateVerify(psi, root, prev, next, 1) {
		t.Fatalf("StateVerify did not accept the li=0-branch state")
	}
}
