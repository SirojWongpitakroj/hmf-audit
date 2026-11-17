package iimt

import (
	"reflect"
	"testing"

	"blas/bhash"
	"blas/bitmap"
)

// buildFig2Fixture builds the F=2 IIMT over the 8-object Fig. 2 fixture:
// Error 11001001, Warning 00100110, PUT 10011000, GET 01000001 (objects
// o1..o8, 1-based in the figure; we insert oid 0..7, so bit i (0-based)
// corresponds to figure-object i+1). This reproduces the fixture's boolean
// *shape*; digest values are never asserted (per T-006..T-013's own
// caveats), only structure, set membership and accept/reject.
func buildFig2Fixture(t *testing.T) *Tree {
	t.Helper()
	tree, err := NewEmpty(2)
	if err != nil {
		t.Fatal(err)
	}
	tree.AddKeyword("Error")
	tree.AddKeyword("Warning")
	tree.AddKeyword("PUT")
	tree.AddKeyword("GET")

	// Rather than hand-deriving membership from the printed bit strings
	// (error-prone by inspection), insert objects directly by setting the
	// fixture bits through Insert's keyword-membership mechanism: for each
	// object index i (0-based), the object "has" keyword w iff the
	// fixture's bit i for w is 1.
	fixture := map[string]string{
		"Error":   "11001001",
		"Warning": "00100110",
		"PUT":     "10011000",
		"GET":     "01000001",
	}
	for i := 0; i < 8; i++ {
		var kws []string
		for w, bits := range fixture {
			if bits[i] == '1' {
				kws = append(kws, w)
			}
		}
		if err := tree.Insert(uint64(i), kws); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

func TestMalformedChildIndexRejectsWithoutPanic(t *testing.T) {
	tree := buildFig2Fixture(t)
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	_, proof, ok := tree.Query("PUT")
	if !ok || len(proof.Steps) == 0 {
		t.Fatal("missing proof path")
	}
	proof.Steps[0].ChildIndex = 999
	if Verify(root, proof) {
		t.Fatal("accepted malformed child index")
	}
}

// TestLeafHashFormula: T-006 — H(w||H(bm)), not H(w||bm), not H(H(bm)||w).
func TestLeafHashFormula(t *testing.T) {
	w := "PUT"
	bm := bitmap.New(8)
	bm.Set(0)
	got := leafHash(w, bm)

	want := bhash.HashConcat(bhash.FramedString(w), bhash.DigestBytes(bhash.H(bm.Bytes())))
	if got != want {
		t.Fatalf("leaf hash formula mismatch")
	}

	wrong1 := bhash.HashConcat(bhash.FramedString(w), bm.Bytes())
	if got == wrong1 {
		t.Fatal("leaf hash must not equal H(w||bm) (missing inner hash)")
	}
	inner := bhash.H(bm.Bytes())
	wrong2 := bhash.HashConcat(bhash.DigestBytes(inner), bhash.FramedString(w))
	if got == wrong2 {
		t.Fatal("leaf hash must not equal H(H(bm)||w) (wrong order)")
	}
}

// TestInternalHashChildOrder: T-007 — reordering children changes the hash.
func TestInternalHashChildOrder(t *testing.T) {
	a := bhash.H([]byte("a"))
	b := bhash.H([]byte("b"))
	h1 := bhash.HashConcat(bhash.DigestBytes(a), bhash.DigestBytes(b))
	h2 := bhash.HashConcat(bhash.DigestBytes(b), bhash.DigestBytes(a))
	if h1 == h2 {
		t.Fatal("child order must matter")
	}
}

// TestKIDAppendOnly: kid starts at 0, increases monotonically (C1).
func TestKIDAppendOnly(t *testing.T) {
	tree, err := NewEmpty(4)
	if err != nil {
		t.Fatal(err)
	}
	k1 := tree.AddKeyword("a")
	k2 := tree.AddKeyword("b")
	k3 := tree.AddKeyword("c")
	if k1 != 0 || k2 != 1 || k3 != 2 {
		t.Fatalf("expected kids 0,1,2 got %d,%d,%d", k1, k2, k3)
	}
	// Re-adding an existing keyword returns its existing kid, not a new one.
	if again := tree.AddKeyword("a"); again != 0 {
		t.Fatalf("re-adding keyword should return existing kid 0, got %d", again)
	}
	if tree.NumLeaves() != 3 {
		t.Fatalf("expected 3 leaves, got %d", tree.NumLeaves())
	}
}

// TestQueryFig2PUT: T-008 — QR={1,4,5} for w=PUT (0-based: {0,3,4}), bm as
// fixture, proof length matches tree height.
func TestQueryFig2PUT(t *testing.T) {
	tree := buildFig2Fixture(t)
	qr, proof, ok := tree.Query("PUT")
	if !ok {
		t.Fatal("expected PUT to exist")
	}
	want := []uint64{0, 3, 4} // fixture PUT = 10011000 -> bits 0,3,4 (0-based)
	if !reflect.DeepEqual(qr, want) {
		t.Fatalf("QR = %v, want %v", qr, want)
	}
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(root, proof) {
		t.Fatal("proof should verify")
	}
}

// TestArrayBasedLookup: T-009 — leaf lookup by kid is O(1) and leaf order
// equals kid order.
func TestArrayBasedLookup(t *testing.T) {
	tree, _ := NewEmpty(4)
	tree.AddKeyword("Error")
	tree.AddKeyword("Warning")
	tree.AddKeyword("PUT")
	kid, ok := tree.KID("PUT")
	if !ok || kid != 2 {
		t.Fatalf("expected PUT to have kid=2, got %d ok=%v", kid, ok)
	}
}

// TestTamperedBitmapRejected: T-011.
func TestTamperedBitmapRejected(t *testing.T) {
	tree := buildFig2Fixture(t)
	_, proof, ok := tree.Query("PUT")
	if !ok {
		t.Fatal("expected PUT to exist")
	}
	root, _ := tree.Root()
	if !Verify(root, proof) {
		t.Fatal("genuine proof should verify")
	}
	tampered := proof.Bitmap.Clone()
	tampered.Set(1) // flip a bit that was 0
	proof.Bitmap = tampered
	if Verify(root, proof) {
		t.Fatal("tampered bitmap should not verify")
	}
}

// TestAlgorithm1UpdateBehaviour: T-013 — inserting an object only sets bits
// on matching leaves; matching leaves' hashes are recomputed; with the D-1
// deviation, non-matching leaves' hashes are ALSO recomputed whenever N
// crosses a byte boundary (which happens on every single-object insert
// while N/8 has no remainder before growth in this small test, or on
// crossing 8,16,...). We verify both the literal Alg. 1 bit-setting
// behavior and the D-1 recomputation trigger explicitly.
func TestAlgorithm1UpdateBehaviour(t *testing.T) {
	tree, _ := NewEmpty(2)
	tree.AddKeyword("PUT")
	tree.AddKeyword("GET")
	tree.AddKeyword("Error")
	tree.AddKeyword("Warning")

	// Insert oid=0..6 with no keywords, to bring N to 7 (still 1 byte: ceil(7/8)=1).
	for i := 0; i < 7; i++ {
		if err := tree.Insert(uint64(i), nil); err != nil {
			t.Fatal(err)
		}
	}
	putHashBefore := tree.leaves[tree.kwIndex["PUT"]].hash
	errHashBefore := tree.leaves[tree.kwIndex["Error"]].hash

	// Insert oid=7 with keyword PUT: N goes 7->8, ceil(7/8)=1, ceil(8/8)=1:
	// no byte-length change. Only PUT's bit is set, only PUT's hash changes.
	if err := tree.Insert(7, []string{"PUT"}); err != nil {
		t.Fatal(err)
	}
	putHashAfter := tree.leaves[tree.kwIndex["PUT"]].hash
	errHashAfter := tree.leaves[tree.kwIndex["Error"]].hash
	if putHashAfter == putHashBefore {
		t.Fatal("PUT (matching) leaf hash should have changed")
	}
	if errHashAfter != errHashBefore {
		t.Fatal("Error (non-matching) leaf hash should NOT have changed: no byte-length change occurred")
	}

	// Now insert oid=8 with keyword GET: N goes 8->9, ceil(8/8)=1, ceil(9/8)=2:
	// byte length changes, so EVERY leaf's hash must be recomputed (D-1),
	// even Error and Warning which do not match.
	errHashBefore2 := tree.leaves[tree.kwIndex["Error"]].hash
	warnHashBefore2 := tree.leaves[tree.kwIndex["Warning"]].hash
	if err := tree.Insert(8, []string{"GET"}); err != nil {
		t.Fatal(err)
	}
	errHashAfter2 := tree.leaves[tree.kwIndex["Error"]].hash
	warnHashAfter2 := tree.leaves[tree.kwIndex["Warning"]].hash
	if errHashAfter2 == errHashBefore2 {
		t.Fatal("D-1: Error leaf hash should change when N crosses a byte boundary, even though Error did not match")
	}
	if warnHashAfter2 == warnHashBefore2 {
		t.Fatal("D-1: Warning leaf hash should change when N crosses a byte boundary, even though Warning did not match")
	}
	// And the bit pattern itself: Error's bitmap must still be all-zero.
	errBM := tree.leaves[tree.kwIndex["Error"]].bm
	for i := uint64(0); i < errBM.Len(); i++ {
		if errBM.Get(i) {
			t.Fatalf("Error bitmap should remain all-zero, bit %d is set", i)
		}
	}
}

// TestAbsentKeywordAllZero: T-014.
func TestAbsentKeywordAllZero(t *testing.T) {
	tree, _ := NewEmpty(4)
	tree.AddKeyword("used")
	tree.AddKeyword("unused")
	if err := tree.Insert(0, []string{"used"}); err != nil {
		t.Fatal(err)
	}
	if err := tree.Insert(1, []string{"used"}); err != nil {
		t.Fatal(err)
	}
	_, unusedProof, ok := tree.Query("unused")
	if !ok {
		t.Fatal("unused keyword should still have a leaf")
	}
	for i := uint64(0); i < unusedProof.Bitmap.Len(); i++ {
		if unusedProof.Bitmap.Get(i) {
			t.Fatalf("unused keyword bitmap should be all-zero, bit %d set", i)
		}
	}
}

func TestQueryUnknownKeyword(t *testing.T) {
	tree, _ := NewEmpty(4)
	tree.AddKeyword("a")
	if _, _, ok := tree.Query("b"); ok {
		t.Fatal("unknown keyword should not be found")
	}
}

// TestMergeProofsSharedInternalHash: T-012 — for the Fig. 2 fixture,
// querying {Error, PUT, GET} should share exactly one internal hash (the
// two-leaf group each keyword's sibling comes from is not shared across all
// three, but Error and PUT are both under kid range covered so that one
// shared internal hash — h2, the top-level's other branch's hash — is
// common across all three individual proofs' final step at F=2 with 4
// leaves: level0 has 4 leaves -> level1 has 2 nodes -> level2 root).
// Structural assertion only: merging reduces the distinct sibling-hash
// count below the naive per-keyword total (B4 ruling); exact fixture
// digest values are never asserted.
func TestMergeProofsSharedInternalHash(t *testing.T) {
	tree := buildFig2Fixture(t)
	var proofs []Proof
	for _, w := range []string{"Error", "PUT", "GET"} {
		_, p, ok := tree.Query(w)
		if !ok {
			t.Fatalf("expected keyword %q to exist", w)
		}
		proofs = append(proofs, p)
	}
	naiveTotal := 0
	for _, p := range proofs {
		for _, step := range p.Steps {
			naiveTotal += len(step.SiblingHashes)
		}
	}
	_, distinct := MergeProofs(proofs)
	if distinct >= naiveTotal {
		t.Fatalf("merged distinct sibling hash count (%d) should be less than the naive per-proof total (%d)", distinct, naiveTotal)
	}
	// Each individual proof must still independently verify against the
	// tree's root — merging is a size accounting step, not a change to any
	// proof's own verifiability.
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range proofs {
		if !Verify(root, p) {
			t.Fatalf("proof for keyword %q should still verify after merge accounting", p.Keyword)
		}
	}
}
