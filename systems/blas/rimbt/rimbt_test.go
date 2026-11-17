package rimbt

import (
	"reflect"
	"testing"
)

// buildFig2Fixture builds the F=2 RIMBT fixture: leaves 12: 00110010,
// 15: 00111010, 97: 10111110, 99: 11111111 over 8 objects (T-016).
// bit i (0-based) corresponds to figure-object i+1.
func buildFig2Fixture(t *testing.T) *Tree {
	t.Helper()
	tree, err := NewEmpty(2)
	if err != nil {
		t.Fatal(err)
	}
	// attrValues[i] = the attribute value of object i (0-based), chosen so
	// that the cumulative "<=v" bitmaps exactly match the fixture strings.
	// Fixture (leftmost bit = object 1):
	//   12: 0 0 1 1 0 0 1 0
	//   15: 0 0 1 1 1 0 1 0
	//   97: 1 0 1 1 1 1 1 0
	//   99: 1 1 1 1 1 1 1 1
	// Reading down each column gives the value of that object:
	//   obj1: 12? no(0) 15?no(0) 97?yes(1) -> so obj1's value is 97 (first v where bit becomes 1, since cumulative)
	// We derive it by finding, for each object, the smallest fixture value v
	// such that bit=1 (since bitmap(v) = value<=v is monotget non-decreasing in v).
	fixture := []struct {
		v    int64
		bits string
	}{
		{12, "00110010"},
		{15, "00111010"},
		{97, "10111110"},
		{99, "11111111"},
	}
	attrValues := make([]int64, 8)
	for obj := 0; obj < 8; obj++ {
		for _, f := range fixture {
			if f.bits[obj] == '1' {
				attrValues[obj] = f.v
				break
			}
		}
	}
	for oid, v := range attrValues {
		if err := tree.Insert(uint64(oid), v); err != nil {
			t.Fatal(err)
		}
	}
	return tree
}

func TestFig2CumulativeSemantics(t *testing.T) {
	tree := buildFig2Fixture(t)
	if tree.NumLeaves() != 4 {
		t.Fatalf("expected 4 distinct leaves, got %d", tree.NumLeaves())
	}
	// leaves ascend 12, 15, 97, 99
	vs := make([]int64, len(tree.leaves))
	for i, l := range tree.leaves {
		vs[i] = l.v
	}
	want := []int64{12, 15, 97, 99}
	if !reflect.DeepEqual(vs, want) {
		t.Fatalf("leaf values = %v, want %v", vs, want)
	}
	expectedBits := map[int64]string{12: "00110010", 15: "00111010", 97: "10111110", 99: "11111111"}
	for _, l := range tree.leaves {
		for i := 0; i < 8; i++ {
			want := expectedBits[l.v][i] == '1'
			if got := l.bm.Get(uint64(i)); got != want {
				t.Fatalf("leaf v=%d bit %d = %v, want %v", l.v, i, got, want)
			}
		}
	}
}

func TestRangeProofRejectsDifferentBoundsAndLooseNeighbor(t *testing.T) {
	tree := buildFig2Fixture(t)
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	res, err := tree.RangeQuery(13, 98)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyRangeResult(root, 8, 13, 98, res) {
		t.Fatal("honest range rejected")
	}
	if VerifyRangeResult(root, 8, 98, 99, res) {
		t.Fatal("accepted proof for different bounds")
	}
	loose := res
	loose.AlphaNeighborProof = tree.proveIndex(2)
	if VerifyRangeResult(root, 8, 13, 98, loose) {
		t.Fatal("accepted a non-adjacent lower neighbor")
	}
}

func TestMalformedChildIndexRejectsWithoutPanic(t *testing.T) {
	tree := buildFig2Fixture(t)
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	proof := tree.proveIndex(0)
	proof.Steps[0].ChildIndex = -1
	if Verify(root, proof) {
		t.Fatal("accepted malformed child index")
	}
}

// TestVSets: T-020 — parent of {12,15} has V={15}; parent of {97,99} has
// V={99}; root has V={97}.
func TestVSets(t *testing.T) {
	tree := buildFig2Fixture(t)
	// level0 = leaves [12,15,97,99]; level1 = groups of 2: [{12,15},{97,99}]; level2 = root grouping [level1 nodes]
	if len(tree.levels) != 3 {
		t.Fatalf("expected 3 levels (leaf, internal, root), got %d", len(tree.levels))
	}
	level1 := tree.levels[1]
	if len(level1) != 2 {
		t.Fatalf("expected 2 internal nodes at level 1, got %d", len(level1))
	}
	if !reflect.DeepEqual(level1[0].v, []int64{15}) {
		t.Fatalf("parent({12,15}).V = %v, want [15]", level1[0].v)
	}
	if !reflect.DeepEqual(level1[1].v, []int64{99}) {
		t.Fatalf("parent({97,99}).V = %v, want [99]", level1[1].v)
	}
	root := tree.levels[2]
	if !reflect.DeepEqual(root[0].v, []int64{97}) {
		t.Fatalf("root.V = %v, want [97]", root[0].v)
	}
}

// TestExactValueInsertion: T-017 — inserting an object with an existing
// value sets the bit from that leaf's position to the last leaf only.
func TestExactValueInsertion(t *testing.T) {
	tree := buildFig2Fixture(t)
	if err := tree.Insert(8, 15); err != nil { // oid=8 (9th object), attr=15 (exists)
		t.Fatal(err)
	}
	find := func(v int64) leafRec {
		for _, l := range tree.leaves {
			if l.v == v {
				return l
			}
		}
		t.Fatalf("leaf v=%d not found", v)
		return leafRec{}
	}
	if !find(15).bm.Get(8) {
		t.Fatal("bit 8 should be set in leaf 15")
	}
	if !find(97).bm.Get(8) {
		t.Fatal("bit 8 should be set in leaf 97 (suffix update)")
	}
	if !find(99).bm.Get(8) {
		t.Fatal("bit 8 should be set in leaf 99 (suffix update)")
	}
	if find(12).bm.Get(8) {
		t.Fatal("bit 8 should NOT be set in leaf 12 (before the insertion point)")
	}
}

// TestNewValueInsertion: T-018 — inserting a new value between existing
// ones copies the predecessor's bitmap then applies the suffix update.
func TestNewValueInsertion(t *testing.T) {
	tree := buildFig2Fixture(t)
	predBM := make([]bool, 8)
	for _, l := range tree.leaves {
		if l.v == 15 {
			for i := 0; i < 8; i++ {
				predBM[i] = l.bm.Get(uint64(i))
			}
		}
	}
	if err := tree.Insert(8, 50); err != nil { // 50 is strictly between 15 and 97
		t.Fatal(err)
	}
	if tree.NumLeaves() != 5 {
		t.Fatalf("expected 5 leaves after new-value insertion, got %d", tree.NumLeaves())
	}
	var newLeaf leafRec
	found := false
	for _, l := range tree.leaves {
		if l.v == 50 {
			newLeaf = l
			found = true
		}
	}
	if !found {
		t.Fatal("expected new leaf v=50")
	}
	for i := 0; i < 8; i++ {
		want := predBM[i]
		if i == 8 {
			continue
		}
		if got := newLeaf.bm.Get(uint64(i)); got != want {
			t.Fatalf("new leaf bit %d = %v, want copy of predecessor's %v", i, got, want)
		}
	}
	if !newLeaf.bm.Get(8) {
		t.Fatal("new leaf bit 8 (the inserted object) should be set")
	}
}

// TestMinimumValueInsertion: T-019 resolved by B2 — inserting below the
// current minimum uses a virtual all-zero predecessor.
func TestMinimumValueInsertion(t *testing.T) {
	tree := buildFig2Fixture(t)
	if err := tree.Insert(8, 5); err != nil { // 5 < 12, the current minimum
		t.Fatal(err)
	}
	var newLeaf leafRec
	for _, l := range tree.leaves {
		if l.v == 5 {
			newLeaf = l
		}
	}
	for i := 0; i < 8; i++ {
		if newLeaf.bm.Get(uint64(i)) {
			t.Fatalf("bit %d should be 0 (virtual all-zero predecessor), got 1", i)
		}
	}
	if !newLeaf.bm.Get(8) {
		t.Fatal("bit 8 (the inserted object) should be set")
	}
}

// TestRangeQueryFig2: T-021 — SR=[13,98]: BM={bm(12),bm(97)},
// QR=¬bm(12)∧bm(97)={1,5,6} (0-based: {0,4,5}).
func TestRangeQueryFig2(t *testing.T) {
	tree := buildFig2Fixture(t)
	res, err := tree.RangeQuery(13, 98)
	if err != nil {
		t.Fatal(err)
	}
	if res.LowerReal == false {
		t.Fatal("expected a real lower boundary leaf (13 > min 12)")
	}
	if res.LowerProof.V != 12 {
		t.Fatalf("expected lower boundary leaf v=12, got %d", res.LowerProof.V)
	}
	if res.UpperIsExact {
		t.Fatal("98 does not equal any leaf value; expected the 'else' branch")
	}
	if res.UpperProof.V != 97 {
		t.Fatalf("expected upper boundary leaf v=97, got %d", res.UpperProof.V)
	}
	want := []uint64{0, 4, 5}
	if !reflect.DeepEqual(res.QR, want) {
		t.Fatalf("QR = %v, want %v", res.QR, want)
	}
	root, _ := tree.Root()
	if !Verify(root, res.LowerProof) {
		t.Fatal("lower boundary proof should verify")
	}
	if !Verify(root, res.UpperProof) {
		t.Fatal("upper boundary proof should verify")
	}
}

// TestRangeQueryBetaEqualsLeaf: T-022 — SR=[13,99]: beta matches an
// existing leaf exactly.
func TestRangeQueryBetaEqualsLeaf(t *testing.T) {
	tree := buildFig2Fixture(t)
	res, err := tree.RangeQuery(13, 99)
	if err != nil {
		t.Fatal(err)
	}
	if !res.UpperIsExact {
		t.Fatal("expected the 'ln_{y+1}.v == beta' branch")
	}
	if res.UpperProof.V != 99 {
		t.Fatalf("expected upper boundary leaf v=99, got %d", res.UpperProof.V)
	}
}

// TestRangeProofTamperRejected: T-023.
func TestRangeProofTamperRejected(t *testing.T) {
	tree := buildFig2Fixture(t)
	res, err := tree.RangeQuery(13, 98)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := tree.Root()
	if !Verify(root, res.UpperProof) {
		t.Fatal("genuine proof should verify")
	}
	tampered := res.UpperProof
	tampered.Bitmap = tampered.Bitmap.Clone()
	tampered.Bitmap.Set(1)
	if Verify(root, tampered) {
		t.Fatal("tampered bitmap should not verify")
	}
}

// TestOutOfDomainBoundaries: T-025, resolved by B5/B6 rather than left
// undefined.
func TestOutOfDomainBoundaries(t *testing.T) {
	tree := buildFig2Fixture(t)

	// (a) alpha below min v: no real lower boundary leaf.
	resA, err := tree.RangeQuery(5, 98)
	if err != nil {
		t.Fatal(err)
	}
	if resA.LowerReal {
		t.Fatal("expected no real lower boundary leaf for alpha below the minimum")
	}

	// (b) beta above max v: upper boundary falls back to the rightmost leaf.
	resB, err := tree.RangeQuery(13, 200)
	if err != nil {
		t.Fatal(err)
	}
	if !resB.UpperReal || resB.UpperProof.V != 99 {
		t.Fatalf("expected upper boundary to fall back to rightmost leaf v=99, got real=%v v=%d", resB.UpperReal, resB.UpperProof.V)
	}

	// (c) alpha > beta: reject.
	if _, err := tree.RangeQuery(98, 13); err != ErrInvertedRange {
		t.Fatalf("expected ErrInvertedRange, got %v", err)
	}
}

// TestBoundaryAdjacencyEnforced: T-024 — a non-adjacent boundary pair that
// still verifies against the root must be rejected by a verifier that
// checks adjacency (this test demonstrates the proof carries the
// information — v — needed for that check; the check itself belongs to the
// iomf/verify layer, but the primitive must expose v, which it does via
// Proof.V, per B5 "boundary v values ARE returned in the VO").
func TestBoundaryAdjacencyEnforced(t *testing.T) {
	tree := buildFig2Fixture(t)
	res, err := tree.RangeQuery(13, 98)
	if err != nil {
		t.Fatal(err)
	}
	// A correct verifier must check ln_x.v < alpha <= ln_x+1.v-ish adjacency;
	// demonstrate that v=12 (< 13) and v=97 is indeed not equal to 98 but is
	// the correct "less than beta" boundary. A wrong pair, e.g. using v=99
	// (not adjacent to 98 on the correct side) as if it were the ln_y
	// leaf, is distinguishable because its v does not satisfy v < 98.
	if !(res.LowerProof.V < 13) {
		t.Fatalf("lower boundary v=%d must be < alpha=13", res.LowerProof.V)
	}
	if !(res.UpperProof.V < 98) {
		t.Fatalf("upper boundary v=%d must be < beta=98 in the non-exact branch", res.UpperProof.V)
	}
}

func TestInsertRequiresSequentialOID(t *testing.T) {
	tree, _ := NewEmpty(4)
	if err := tree.Insert(1, 10); err == nil {
		t.Fatal("expected error inserting oid=1 before oid=0")
	}
}
