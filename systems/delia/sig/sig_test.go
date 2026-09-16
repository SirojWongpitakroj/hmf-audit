package sig

import (
	"bytes"
	"testing"

	"delia/hash"
)

func TestGenSignVrfyRoundTrip(t *testing.T) {
	kp, err := Gen()
	if err != nil {
		t.Fatalf("Gen: %v", err)
	}
	digest := hash.H([]byte("hello world"))
	s, err := kp.Sign(digest)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(s) != SignatureSize {
		t.Fatalf("signature length = %d, want %d", len(s), SignatureSize)
	}
	if !Vrfy(kp.PublicKey(), digest, s) {
		t.Fatalf("Vrfy rejected a valid signature")
	}
}

// TEST-41-style: a one-bit modification to the signed content is rejected.
func TestVrfyRejectsModifiedDigest(t *testing.T) {
	kp, _ := Gen()
	digest := hash.H([]byte("hello world"))
	s, _ := kp.Sign(digest)

	other := hash.H([]byte("hello worlD"))
	if Vrfy(kp.PublicKey(), other, s) {
		t.Fatalf("Vrfy accepted a signature for a different digest")
	}
}

func TestVrfyRejectsModifiedSignature(t *testing.T) {
	kp, _ := Gen()
	digest := hash.H([]byte("hello world"))
	s, _ := kp.Sign(digest)
	tampered := make([]byte, len(s))
	copy(tampered, s)
	tampered[0] ^= 0xFF
	if Vrfy(kp.PublicKey(), digest, tampered) {
		t.Fatalf("Vrfy accepted a tampered signature")
	}
}

func TestVrfyRejectsWrongKey(t *testing.T) {
	kp1, _ := Gen()
	kp2, _ := Gen()
	digest := hash.H([]byte("hello world"))
	s, _ := kp1.Sign(digest)
	if Vrfy(kp2.PublicKey(), digest, s) {
		t.Fatalf("Vrfy accepted a signature under the wrong key")
	}
}

func TestPublicKeyCanonicalEncodingLength(t *testing.T) {
	kp, _ := Gen()
	if len(kp.PublicKey()) != PublicKeySize {
		t.Fatalf("public key length = %d, want %d (canonical compressed encoding)", len(kp.PublicKey()), PublicKeySize)
	}
}

// CRYPTO-008: the Ethereum address is derived from the same secp256k1 key,
// with no second identity mapping; deriving it from the public key alone
// (without the private key) must give the same result.
func TestAddressDerivedFromPublicKeyOnly(t *testing.T) {
	kp, _ := Gen()
	want := kp.Address()
	got, err := AddressFromPublicKey(kp.PublicKey())
	if err != nil {
		t.Fatalf("AddressFromPublicKey: %v", err)
	}
	if got != want {
		t.Fatalf("address mismatch: %x != %x", got, want)
	}
}

func TestDifferentKeysDifferentAddresses(t *testing.T) {
	kp1, _ := Gen()
	kp2, _ := Gen()
	if kp1.Address() == kp2.Address() {
		t.Fatalf("two freshly generated keys produced the same address")
	}
}

// CRYPTO-009: local private-key storage round trip (no runtime PKI).
func TestKeyBytesRoundTrip(t *testing.T) {
	kp, _ := Gen()
	b := kp.Bytes()
	if len(b) != 32 {
		t.Fatalf("private key bytes length = %d, want 32", len(b))
	}
	loaded, err := Load(b)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !bytes.Equal(loaded.PublicKey(), kp.PublicKey()) {
		t.Fatalf("loaded key does not match original")
	}
	digest := hash.H([]byte("round trip"))
	s, _ := loaded.Sign(digest)
	if !Vrfy(kp.PublicKey(), digest, s) {
		t.Fatalf("signature from loaded key did not verify under original public key")
	}
}

func TestVrfyRejectsMalformedInputs(t *testing.T) {
	kp, _ := Gen()
	digest := hash.H([]byte("x"))
	if Vrfy(kp.PublicKey(), digest, nil) {
		t.Fatalf("Vrfy accepted a nil signature")
	}
	if Vrfy(nil, digest, make([]byte, SignatureSize)) {
		t.Fatalf("Vrfy accepted a nil public key")
	}
}
