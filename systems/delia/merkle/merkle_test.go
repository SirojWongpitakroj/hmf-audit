package merkle

import (
	"testing"

	"delia/hash"
)

func leaf(b byte) hash.Digest {
	return hash.H([]byte{b})
}

// TestMHTGenVectorTheta3 checks the frozen GAP-01(a) test vector:
// theta = 3: root = H(H(L1||L2) || H(L3||L3)).
func TestMHTGenVectorTheta3(t *testing.T) {
	l1, l2, l3 := leaf(1), leaf(2), leaf(3)
	h12 := hash.H(hash.Concat(l1[:], l2[:]))
	h33 := hash.H(hash.Concat(l3[:], l3[:]))
	want := hash.H(hash.Concat(h12[:], h33[:]))
	got := MHTGen([]hash.Digest{l1, l2, l3})
	if got != want {
		t.Fatalf("MHTGen(theta=3) = %x, want %x", got, want)
	}
}

// TestMHTGenVectorTheta1 checks the frozen GAP-74(a) test vector:
// theta = 1: root = the single leaf, no self-hash at the root.
func TestMHTGenVectorTheta1(t *testing.T) {
	l1 := leaf(1)
	got := MHTGen([]hash.Digest{l1})
	if got != l1 {
		t.Fatalf("MHTGen(theta=1) = %x, want the leaf itself %x", got, l1)
	}
}

// TestMHTGenVectorTheta4 checks the paper's own Fig. 2 shape (4 leaves, two
// full levels, no duplication needed).
func TestMHTGenVectorTheta4(t *testing.T) {
	l1, l2, l3, l4 := leaf(1), leaf(2), leaf(3), leaf(4)
	h0 := hash.H(hash.Concat(l1[:], l2[:]))
	h1 := hash.H(hash.Concat(l3[:], l4[:]))
	want := hash.H(hash.Concat(h0[:], h1[:]))
	got := MHTGen([]hash.Digest{l1, l2, l3, l4})
	if got != want {
		t.Fatalf("MHTGen(theta=4) = %x, want %x", got, want)
	}
}

func TestMHTGenDeterministic(t *testing.T) {
	leaves := []hash.Digest{leaf(1), leaf(2), leaf(3), leaf(4), leaf(5)}
	a := MHTGen(leaves)
	b := MHTGen(leaves)
	if a != b {
		t.Fatalf("MHTGen is not deterministic")
	}
}

func TestMHTGenChangesWithAnyLeaf(t *testing.T) {
	base := []hash.Digest{leaf(1), leaf(2), leaf(3), leaf(4), leaf(5)}
	root := MHTGen(base)
	for i := range base {
		mutated := make([]hash.Digest, len(base))
		copy(mutated, base)
		mutated[i] = leaf(99)
		got := MHTGen(mutated)
		if got == root {
			t.Fatalf("changing leaf %d did not change the root", i)
		}
	}
}

func TestMHTGenEmptyPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected MHTGen to panic on empty leaves")
		}
	}()
	MHTGen(nil)
}
