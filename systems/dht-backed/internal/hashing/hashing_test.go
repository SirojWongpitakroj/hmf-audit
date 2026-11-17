package hashing_test

import (
	"crypto/sha256"
	"testing"

	"dhtbacked/internal/hashing"
)

func TestHIsSHA256AndThirtyTwoBytes(t *testing.T) {
	got := hashing.H([]byte("ab"), []byte("c"))
	want := sha256.Sum256([]byte("abc"))
	if got != want {
		t.Fatalf("H is not SHA-256 over the raw concatenation")
	}
	if len(got) != 32 || hashing.Size != 32 {
		t.Fatalf("digest length = %d, want 32", len(got))
	}
}

func TestTagsAreExactlyThreeSingleBytes(t *testing.T) {
	if hashing.TagLeaf != 0x00 || hashing.TagInternal != 0x01 || hashing.TagAttestation != 0xA3 {
		t.Fatalf("domain-separation tag values changed")
	}
	for _, tag := range []byte{hashing.TagLeaf, hashing.TagInternal, hashing.TagAttestation} {
		if len(hashing.Tag(tag)) != 1 {
			t.Fatalf("tag %#x is not exactly one byte", tag)
		}
	}
}

func TestConcatenationHasNoSeparator(t *testing.T) {
	// H(0x01 || left || right) must equal SHA-256 over the 65 raw bytes.
	var left, right hashing.Digest
	for i := range left {
		left[i] = byte(i)
		right[i] = byte(255 - i)
	}
	pre := make([]byte, 0, 65)
	pre = append(pre, 0x01)
	pre = append(pre, left[:]...)
	pre = append(pre, right[:]...)
	if hashing.H(hashing.Tag(hashing.TagInternal), left[:], right[:]) != sha256.Sum256(pre) {
		t.Fatal("internal node hashing introduced a separator")
	}
}
