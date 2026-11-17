package omt

import (
	"math/rand"
	"testing"
)

func mkLeaves(n int) []Leaf {
	leaves := make([]Leaf, n)
	for i := 0; i < n; i++ {
		leaves[i] = Leaf{OID: uint64(i + 1), Object: []byte{byte(i + 1)}}
	}
	return leaves
}

func TestMalformedChildIndexRejectsWithoutPanic(t *testing.T) {
	tree, err := Build(2, mkLeaves(4))
	if err != nil {
		t.Fatal(err)
	}
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := tree.Prove(1)
	if !ok || len(proof.Steps) == 0 {
		t.Fatal("missing proof path")
	}
	proof.Steps[0].ChildIndex = 999
	if Verify(root, proof) {
		t.Fatal("accepted malformed child index")
	}
}

// TestOrderIndependence: T-001 — shuffled input yields the same root as
// sorted input.
func TestOrderIndependence(t *testing.T) {
	leaves := mkLeaves(8)
	sortedTree, err := Build(2, leaves)
	if err != nil {
		t.Fatal(err)
	}
	sortedRoot, err := sortedTree.Root()
	if err != nil {
		t.Fatal(err)
	}

	shuffled := make([]Leaf, len(leaves))
	copy(shuffled, leaves)
	rand.New(rand.NewSource(1)).Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	shuffledTree, err := Build(2, shuffled)
	if err != nil {
		t.Fatal(err)
	}
	shuffledRoot, err := shuffledTree.Root()
	if err != nil {
		t.Fatal(err)
	}

	if sortedRoot != shuffledRoot {
		t.Fatalf("roots differ: sorted=%x shuffled=%x", sortedRoot, shuffledRoot)
	}
}

// TestProofShape reproduces the Fig. 2 worked example's path *shape*: for
// oid=5 in an 8-leaf binary tree, the path has 3 steps (T-002).
func TestProofShape(t *testing.T) {
	tree, err := Build(2, mkLeaves(8))
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := tree.Prove(5)
	if !ok {
		t.Fatal("expected oid=5 to exist")
	}
	if len(proof.Steps) != 3 {
		t.Fatalf("expected 3 proof steps for 8 leaves at fanout 2, got %d", len(proof.Steps))
	}
	root, _ := tree.Root()
	if !Verify(root, proof) {
		t.Fatal("proof should verify")
	}
}

// TestTamperedObjectRejected: T-003.
func TestTamperedObjectRejected(t *testing.T) {
	tree, err := Build(2, mkLeaves(8))
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := tree.Prove(5)
	if !ok {
		t.Fatal("expected oid=5 to exist")
	}
	root, _ := tree.Root()
	if !Verify(root, proof) {
		t.Fatal("genuine proof should verify")
	}
	proof.Object = []byte{99} // tamper
	if Verify(root, proof) {
		t.Fatal("tampered object should not verify")
	}
}

// TestOIDSwapRejected: T-004 — presenting o5's bytes under a different oid
// must not verify.
func TestOIDSwapRejected(t *testing.T) {
	tree, err := Build(2, mkLeaves(8))
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := tree.Prove(5)
	if !ok {
		t.Fatal("expected oid=5 to exist")
	}
	root, _ := tree.Root()
	proof.OID = 6 // swap
	if Verify(root, proof) {
		t.Fatal("oid swap should not verify")
	}
}

// TestNonExistenceBoundaryEvidence: T-005.
func TestNonExistenceBoundaryEvidence(t *testing.T) {
	// Leaves at oid 1,2,4,5 (3 is absent).
	leaves := []Leaf{
		{OID: 1, Object: []byte{1}},
		{OID: 2, Object: []byte{2}},
		{OID: 4, Object: []byte{4}},
		{OID: 5, Object: []byte{5}},
	}
	tree, err := Build(2, leaves)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tree.Prove(3); ok {
		t.Fatal("oid=3 should not exist")
	}
	left, leftOK, right, rightOK := tree.Neighbors(3)
	if !leftOK || left.OID != 2 {
		t.Fatalf("expected left neighbor oid=2, got ok=%v oid=%d", leftOK, left.OID)
	}
	if !rightOK || right.OID != 4 {
		t.Fatalf("expected right neighbor oid=4, got ok=%v oid=%d", rightOK, right.OID)
	}
	root, _ := tree.Root()
	if !Verify(root, left) || !Verify(root, right) {
		t.Fatal("neighbor proofs should verify")
	}

	// Domain edge: querying below the minimum oid should have no left
	// neighbor.
	leftEdge, leftEdgeOK, rightEdge, rightEdgeOK := tree.Neighbors(0)
	if leftEdgeOK {
		t.Fatalf("expected no left neighbor below domain minimum, got oid=%d", leftEdge.OID)
	}
	if !rightEdgeOK || rightEdge.OID != 1 {
		t.Fatalf("expected right neighbor oid=1 at domain edge, got ok=%v", rightEdgeOK)
	}
}

// TestFanoutParameterization: T-058 (minus the now-resolved GAP-5/6
// blockers) — same objects build with F=2 and F=4 without code change and
// yield different, self-consistent roots.
func TestFanoutParameterization(t *testing.T) {
	leaves := mkLeaves(9) // forces an incomplete rightmost node at both fanouts
	t2, err := Build(2, leaves)
	if err != nil {
		t.Fatal(err)
	}
	t4, err := Build(4, leaves)
	if err != nil {
		t.Fatal(err)
	}
	r2, _ := t2.Root()
	r4, _ := t4.Root()
	if r2 == r4 {
		t.Fatal("different fanouts should generally yield different roots")
	}
	p2, ok := t2.Prove(9)
	if !ok || !Verify(r2, p2) {
		t.Fatal("F=2 proof should verify")
	}
	p4, ok := t4.Prove(9)
	if !ok || !Verify(r4, p4) {
		t.Fatal("F=4 proof should verify")
	}
}

// TestIncompleteRightmostNode ensures a non-multiple-of-F leaf count still
// builds and verifies at every position, exercising the A7 "no padding"
// rule.
func TestIncompleteRightmostNode(t *testing.T) {
	for _, n := range []int{1, 2, 3, 5, 7, 9, 13} {
		leaves := mkLeaves(n)
		tree, err := Build(4, leaves)
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		root, err := tree.Root()
		if err != nil {
			t.Fatalf("n=%d: %v", n, err)
		}
		for _, l := range leaves {
			proof, ok := tree.Prove(l.OID)
			if !ok {
				t.Fatalf("n=%d oid=%d: expected to exist", n, l.OID)
			}
			if !Verify(root, proof) {
				t.Fatalf("n=%d oid=%d: proof did not verify", n, l.OID)
			}
		}
	}
}

func TestEmptyTree(t *testing.T) {
	tree, err := Build(4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.Root(); err != ErrEmpty {
		t.Fatalf("expected ErrEmpty, got %v", err)
	}
}

func TestDuplicateOIDRejected(t *testing.T) {
	leaves := []Leaf{{OID: 1, Object: []byte{1}}, {OID: 1, Object: []byte{2}}}
	if _, err := Build(4, leaves); err == nil {
		t.Fatal("expected error for duplicate oid")
	}
}

func TestSingleLeafTree(t *testing.T) {
	tree, err := Build(4, mkLeaves(1))
	if err != nil {
		t.Fatal(err)
	}
	root, err := tree.Root()
	if err != nil {
		t.Fatal(err)
	}
	proof, ok := tree.Prove(1)
	if !ok {
		t.Fatal("expected oid=1 to exist")
	}
	if len(proof.Steps) != 0 {
		t.Fatalf("single-leaf tree should have zero proof steps, got %d", len(proof.Steps))
	}
	if !Verify(root, proof) {
		t.Fatal("single-leaf proof should verify")
	}
}

// TestIsRightmost: the proof for the largest oid reports IsRightmost()
// true at every fanout/shape; every other leaf's proof reports false.
func TestIsRightmost(t *testing.T) {
	for _, n := range []int{1, 2, 5, 8, 9, 13} {
		leaves := mkLeaves(n)
		tree, err := Build(4, leaves)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range leaves {
			p, ok := tree.Prove(l.OID)
			if !ok {
				t.Fatal("expected leaf to exist")
			}
			want := l.OID == uint64(n) // mkLeaves assigns OID = i+1, so max OID == n
			if got := p.IsRightmost(); got != want {
				t.Fatalf("n=%d oid=%d: IsRightmost()=%v, want %v", n, l.OID, got, want)
			}
		}
	}
}
