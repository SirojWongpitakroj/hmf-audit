package merkle

// Independent tester-agent verification tests for M0-M4 sign-off. These do
// not modify merkle.go; they add coverage the developer's own tests did not
// exercise (theta=2, theta=5/6 multi-level duplication, leaf-order
// sensitivity) and cross-check against crypto/sha256 directly rather than
// going through the hash package, so a bug shared between hash.H and
// MHTGen's use of it would not hide from this test.

import (
	"crypto/sha256"
	"testing"

	"delia/hash"
)

func sha(b []byte) hash.Digest { return sha256.Sum256(b) }

func lf(b byte) hash.Digest { return sha([]byte{b}) }

// theta=2: exactly one level, no duplication.
func TestTesterMHTGenTheta2(t *testing.T) {
	l1, l2 := lf(1), lf(2)
	want := sha(append(append([]byte{}, l1[:]...), l2[:]...))
	got := MHTGen([]hash.Digest{l1, l2})
	if got != want {
		t.Fatalf("theta=2: got %x, want %x", got, want)
	}
}

// theta=5: three leaves duplicated at level 0 down to 3 nodes (odd),
// duplicate last -> 4 nodes... walk through manually:
// level0: L1 L2 L3 L4 L5 (5 nodes, odd -> pair (L1,L2) (L3,L4) (L5,L5))
// level1: A=H(L1|L2) B=H(L3|L4) C=H(L5|L5) (3 nodes, odd -> pair (A,B) (C,C))
// level2: D=H(A|B) E=H(C|C) (2 nodes, even)
// level3: root=H(D|E)
func TestTesterMHTGenTheta5(t *testing.T) {
	l1, l2, l3, l4, l5 := lf(1), lf(2), lf(3), lf(4), lf(5)
	cat := func(a, b hash.Digest) hash.Digest {
		return sha(append(append([]byte{}, a[:]...), b[:]...))
	}
	A := cat(l1, l2)
	B := cat(l3, l4)
	C := cat(l5, l5)
	D := cat(A, B)
	E := cat(C, C)
	want := cat(D, E)

	got := MHTGen([]hash.Digest{l1, l2, l3, l4, l5})
	if got != want {
		t.Fatalf("theta=5: got %x, want %x", got, want)
	}
}

// theta=6: level0 has 6 (even) -> 3 pairs -> level1 has 3 (odd) -> dup last.
func TestTesterMHTGenTheta6(t *testing.T) {
	leaves := make([]hash.Digest, 6)
	for i := range leaves {
		leaves[i] = lf(byte(i + 1))
	}
	cat := func(a, b hash.Digest) hash.Digest {
		return sha(append(append([]byte{}, a[:]...), b[:]...))
	}
	A := cat(leaves[0], leaves[1])
	B := cat(leaves[2], leaves[3])
	C := cat(leaves[4], leaves[5])
	D := cat(A, B)
	E := cat(C, C)
	want := cat(D, E)

	got := MHTGen(leaves)
	if got != want {
		t.Fatalf("theta=6: got %x, want %x", got, want)
	}
}

// Swapping two distinct adjacent leaves must change the root (leaf order
// matters, leaves are not re-hashed/sorted/deduplicated as a set).
func TestTesterMHTGenLeafOrderSensitive(t *testing.T) {
	original := []hash.Digest{lf(1), lf(2), lf(3), lf(4), lf(5)}
	swapped := []hash.Digest{lf(2), lf(1), lf(3), lf(4), lf(5)}
	r1 := MHTGen(original)
	r2 := MHTGen(swapped)
	if r1 == r2 {
		t.Fatalf("swapping two distinct leaves did not change the root")
	}
}

// Single-leaf root is exactly the leaf's own digest, not H(leaf) or
// H(leaf||leaf) (GAP-74). Cross-checked independently of the dev's own
// theta=1 vector test.
func TestTesterMHTGenSingleLeafIsIdentity(t *testing.T) {
	l := lf(42)
	got := MHTGen([]hash.Digest{l})
	if got != l {
		t.Fatalf("single-leaf root = %x, want the leaf itself %x", got, l)
	}
	notSelfHashed := sha(append(append([]byte{}, l[:]...), l[:]...))
	if got == notSelfHashed {
		t.Fatalf("single-leaf root incorrectly self-hashed the leaf")
	}
}
