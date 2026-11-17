package trapdoor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"testing"

	"evseb/internal/crypto"
)

func TestGenerateIsHMACOfSHA256(t *testing.T) {
	k, _ := crypto.Rand256()
	h := sha256.Sum256([]byte("deny"))
	m := hmac.New(sha256.New, k[:])
	m.Write(h[:])
	if got := Generate("deny", k); !bytes.Equal(got.Tw, m.Sum(nil)) || len(got.Tw) != 32 {
		t.Fatalf("Tw = %x", got.Tw)
	}
}

func TestDeterministicAndKeyed(t *testing.T) {
	k1, _ := crypto.Rand256()
	k2, _ := crypto.Rand256()
	if !bytes.Equal(Generate("deny", k1).Tw, Generate("deny", k1).Tw) {
		t.Fatal("trapdoor not deterministic under the same key")
	}
	if bytes.Equal(Generate("deny", k1).Tw, Generate("deny", k2).Tw) {
		t.Fatal("trapdoor independent of Ksearch")
	}
	if bytes.Equal(Generate("deny", k1).Tw, Generate("allow", k1).Tw) {
		t.Fatal("distinct keywords share a trapdoor")
	}
	h := sha256.Sum256([]byte("deny"))
	if bytes.Equal(Generate("deny", k1).Tw, h[:]) {
		t.Fatal("trapdoor equals unkeyed hash")
	}
}
