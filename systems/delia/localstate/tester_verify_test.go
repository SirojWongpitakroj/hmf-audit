package localstate

// Independent tester-agent verification: cross-checks Compute against
// crypto/sha256 directly (bypassing the hash package's own H, so a shared
// bug between hash.H and Compute's use of it can't hide) and stress-tests
// the li=0 vs li>0 branch selection and operand order at a byte level.

import (
	"bytes"
	"crypto/sha256"
	"testing"

	"delia/hash"
)

func TestTesterLi0BranchExactBytes(t *testing.T) {
	var root hash.Digest
	copy(root[:], bytes.Repeat([]byte{0xAB}, 32))
	got := Compute(root, nil)
	want := sha256.Sum256(root[:])
	if got != want {
		t.Fatalf("li=0: Compute = %x, want raw SHA-256(root) = %x", got, want)
	}
	// Must NOT be H(root || 32 zero bytes) -- that would be the wrong
	// "chain with a zero digest" branch (DEV-13's ALG1 concern, but the same
	// mistake is possible here too).
	zero := make([]byte, 32)
	wrongInput := append(append([]byte{}, root[:]...), zero...)
	wrong := sha256.Sum256(wrongInput)
	if got == wrong {
		t.Fatalf("li=0 Compute incorrectly matches H(root || zero-digest)")
	}
}

func TestTesterLiGT0ExactBytesRootFirst(t *testing.T) {
	var root, prev hash.Digest
	copy(root[:], bytes.Repeat([]byte{0x11}, 32))
	copy(prev[:], bytes.Repeat([]byte{0x22}, 32))

	got := Compute(root, &prev)

	rootFirst := sha256.Sum256(append(append([]byte{}, root[:]...), prev[:]...))
	prevFirst := sha256.Sum256(append(append([]byte{}, prev[:]...), root[:]...))

	if got != rootFirst {
		t.Fatalf("Compute(root,prev) = %x, want H(root||prev) = %x", got, rootFirst)
	}
	if got == prevFirst {
		t.Fatalf("Compute matched the wrong (prev||root) operand order")
	}
}

// A 3-batch chain built independently of localstate_test.go's own chain
// test, verifying each link against a hand-rolled sha256 computation.
func TestTesterChainIndependentReconstruction(t *testing.T) {
	roots := make([]hash.Digest, 4)
	for i := range roots {
		var d hash.Digest
		copy(d[:], bytes.Repeat([]byte{byte(0x30 + i)}, 32))
		roots[i] = d
	}

	// li = 0
	omega := Compute(roots[0], nil)
	wantOmega := sha256.Sum256(roots[0][:])
	if omega != wantOmega {
		t.Fatalf("batch 0: got %x want %x", omega, wantOmega)
	}

	for li := 1; li < 4; li++ {
		prev := omega
		omega = Compute(roots[li], &prev)
		wantOmega = sha256.Sum256(append(append([]byte{}, roots[li][:]...), prev[:]...))
		if omega != wantOmega {
			t.Fatalf("batch %d: got %x want %x", li, omega, wantOmega)
		}
	}
}
