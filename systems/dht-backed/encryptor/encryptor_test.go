package encryptor_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"dhtbacked/encryptor"
	"dhtbacked/internal/hashing"
)

func testKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

// Q1: AES-256-GCM, 12-byte nonce, 16-byte tag, c = nonce || ciphertext.
func TestCiphertextFramingIsNoncePrefixed(t *testing.T) {
	e, err := encryptor.New(testKey())
	if err != nil {
		t.Fatal(err)
	}
	rec := sampleRecord()
	m := encryptor.CanonicalBytes(rec)
	ad := encryptor.ADOf(rec)
	c, tag, err := e.Encrypt(m, ad)
	if err != nil {
		t.Fatal(err)
	}
	if len(tag) != encryptor.TagLen {
		t.Fatalf("tag length = %d, want %d", len(tag), encryptor.TagLen)
	}
	if len(c) != encryptor.NonceLen+len(m) {
		t.Fatalf("c length = %d, want nonce(%d) + |M|(%d)", len(c), encryptor.NonceLen, len(m))
	}

	// Independently reconstruct AES-256-GCM to confirm the framing.
	block, err := aes.NewCipher(testKey())
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, encryptor.NonceLen)
	if err != nil {
		t.Fatal(err)
	}
	nonce := c[:encryptor.NonceLen]
	sealed := append(append([]byte{}, c[encryptor.NonceLen:]...), tag...)
	plain, err := gcm.Open(nil, nonce, sealed, ad)
	if err != nil {
		t.Fatalf("independent AES-256-GCM open failed: %v", err)
	}
	if !bytes.Equal(plain, m) {
		t.Fatal("decrypted plaintext differs from M")
	}
}

func TestNoncesDifferPerEncryption(t *testing.T) {
	e, _ := encryptor.New(testKey())
	m := []byte("m")
	ad := encryptor.BuildAD("p", "access", 1, "record-1")
	seen := make(map[string]bool)
	for i := 0; i < 64; i++ {
		c, _, err := e.Encrypt(m, ad)
		if err != nil {
			t.Fatal(err)
		}
		n := string(c[:encryptor.NonceLen])
		if seen[n] {
			t.Fatal("nonce repeated")
		}
		seen[n] = true
	}
}

func TestWrongKeyLengthRejected(t *testing.T) {
	for _, n := range []int{0, 16, 24, 31, 33} {
		if _, err := encryptor.New(make([]byte, n)); err == nil {
			t.Errorf("New accepted a %d-byte key", n)
		}
	}
}

// AD binds the record ID alongside the Q7 metadata so lookup substitution
// cannot produce a valid proof for a different requested record.
func TestADLayout(t *testing.T) {
	ad := encryptor.BuildAD("producer-access", "access", 1789101000500000000, "record-1")
	var want []byte
	want = binary.BigEndian.AppendUint32(want, uint32(len("producer-access")))
	want = append(want, "producer-access"...)
	want = binary.BigEndian.AppendUint32(want, uint32(len("access")))
	want = append(want, "access"...)
	want = binary.BigEndian.AppendUint64(want, uint64(1789101000500000000))
	want = binary.BigEndian.AppendUint32(want, uint32(len("record-1")))
	want = append(want, "record-1"...)
	if !bytes.Equal(ad, want) {
		t.Fatalf("AD = % x, want % x", ad, want)
	}
	p, l, ns, id, err := encryptor.ParseAD(ad)
	if err != nil || p != "producer-access" || l != "access" || ns != 1789101000500000000 || id != "record-1" {
		t.Fatalf("ParseAD = %q %q %d %q %v", p, l, ns, id, err)
	}
}

// Q7: the identical AD byte representation is used as AEAD associated data and
// inside the leaf digest.
func TestIdenticalADBytesForAEADAndLeafDigest(t *testing.T) {
	e, _ := encryptor.New(testKey())
	rec := sampleRecord()
	em, err := e.ProcessRecord(rec, newTestSigner(t))
	if err != nil {
		t.Fatal(err)
	}
	ad := encryptor.ADOf(rec)
	if !bytes.Equal(em.AD, ad) {
		t.Fatal("emitted AD differs from the AD built from the record")
	}
	// The AEAD must reject the ciphertext under any other associated data.
	block, _ := aes.NewCipher(testKey())
	gcm, _ := cipher.NewGCMWithNonceSize(block, encryptor.NonceLen)
	sealed := append(append([]byte{}, em.C[encryptor.NonceLen:]...), em.T...)
	if _, err := gcm.Open(nil, em.C[:encryptor.NonceLen], sealed, append(ad, 0)); err == nil {
		t.Fatal("AEAD accepted altered associated data")
	}
	// And the leaf digest must be over those same bytes.
	if em.D != encryptor.LeafDigest(em.C, em.T, ad) {
		t.Fatal("leaf digest is not over the emitted AD bytes")
	}
}

// R-ATT-01 / Eq. (15): d = H(0x00 || c || t || AD).
func TestLeafDigestPreImage(t *testing.T) {
	c := []byte{1, 2, 3}
	tag := []byte{4, 5}
	ad := []byte{6}
	pre := []byte{0x00, 1, 2, 3, 4, 5, 6}
	if encryptor.LeafDigest(c, tag, ad) != sha256.Sum256(pre) {
		t.Fatal("leaf digest pre-image is not 0x00 || c || t || AD")
	}
}

// Eq. (16): sigma = Ed25519.Sign(sk_P, SHA256(0xA3 || d)).
func TestAttestationOverPreHash(t *testing.T) {
	sk := newTestSigner(t)
	var d hashing.Digest
	d[0] = 0x7F
	sigma := encryptor.Attest(sk, d)
	if len(sigma) != 64 {
		t.Fatalf("signature length = %d, want 64", len(sigma))
	}
	pk := sk.Public().(ed25519.PublicKey)
	if len(pk) != 32 {
		t.Fatalf("public key length = %d, want 32", len(pk))
	}
	pre := append([]byte{0xA3}, d[:]...)
	m := sha256.Sum256(pre)
	if !ed25519.Verify(pk, m[:], sigma) {
		t.Fatal("signature is not over SHA256(0xA3 || d)")
	}
	// It must NOT be a signature over d itself.
	if ed25519.Verify(pk, d[:], sigma) {
		t.Fatal("signature verifies over d directly")
	}
	if !encryptor.VerifyAttestation(pk, d, sigma) {
		t.Fatal("VerifyAttestation rejected a valid signature")
	}
	d[1] ^= 0xFF
	if encryptor.VerifyAttestation(pk, d, sigma) {
		t.Fatal("VerifyAttestation accepted a signature over a different digest")
	}
}

// R-ATT-03: the emitted tuple carries everything Phase 2 needs.
func TestEmittedTupleIsComplete(t *testing.T) {
	e, _ := encryptor.New(testKey())
	sk := newTestSigner(t)
	em, err := e.ProcessRecord(sampleRecord(), sk)
	if err != nil {
		t.Fatal(err)
	}
	if len(em.C) == 0 || len(em.T) != 16 || len(em.AD) == 0 ||
		len(em.Sigma) != 64 || len(em.ProducerPublicKey) != 32 {
		t.Fatal("emitted tuple is incomplete")
	}
	if em.D != encryptor.LeafDigest(em.C, em.T, em.AD) {
		t.Fatal("emitted d does not match (c, t, AD)")
	}
	if !encryptor.VerifyAttestation(em.ProducerPublicKey, em.D, em.Sigma) {
		t.Fatal("emitted tuple does not validate statelessly")
	}
}

func newTestSigner(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}
