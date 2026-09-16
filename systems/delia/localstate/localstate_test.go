package localstate

import (
	"testing"

	"delia/hash"
)

func root(b byte) hash.Digest {
	return hash.H([]byte{b})
}

// TEST-03: omega_{p,0} = SHA-256(raw 32-byte root_{p,0}).
func TestInitialLocalState(t *testing.T) {
	r0 := root(0)
	got := Compute(r0, nil)
	want := hash.H(r0[:])
	if got != want {
		t.Fatalf("Compute(root, nil) = %x, want %x", got, want)
	}
}

// TEST-04: chained LocalState across batches 0, 1, 2; altering batch 0
// changes omega_{p,1} and omega_{p,2}; inputs are raw 64-byte concatenations,
// root first.
func TestChainedLocalState(t *testing.T) {
	r0, r1, r2 := root(0), root(1), root(2)

	omega0 := Compute(r0, nil)
	omega1 := Compute(r1, &omega0)
	omega2 := Compute(r2, &omega1)

	wantOmega1 := hash.H(hash.Concat(r1[:], omega0[:]))
	if omega1 != wantOmega1 {
		t.Fatalf("omega1 = %x, want %x (root first, then prev)", omega1, wantOmega1)
	}
	wantOmega2 := hash.H(hash.Concat(r2[:], omega1[:]))
	if omega2 != wantOmega2 {
		t.Fatalf("omega2 = %x, want %x", omega2, wantOmega2)
	}

	// Altering batch 0's root must change omega1 and omega2.
	r0Alt := root(200)
	omega0Alt := Compute(r0Alt, nil)
	omega1Alt := Compute(r1, &omega0Alt)
	omega2Alt := Compute(r2, &omega1Alt)
	if omega1Alt == omega1 {
		t.Fatalf("altering batch 0 did not change omega1")
	}
	if omega2Alt == omega2 {
		t.Fatalf("altering batch 0 did not change omega2")
	}
}

func TestOperandOrderMatters(t *testing.T) {
	r := root(1)
	prev := root(2)
	got := Compute(r, &prev)
	// Root-first, per Eq. (2)/GAP-02: swapping the operand order must give a
	// different digest (guards against silently adopting the Fig. 2 order).
	swapped := hash.H(hash.Concat(prev[:], r[:]))
	if got == swapped {
		t.Fatalf("Compute did not honor root-first concatenation order")
	}
}
