package encryptor

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"

	"dhtbacked/internal/hashing"
)

// AEAD parameters (Q1): AES-256-GCM with a 12-byte nonce drawn from
// crypto/rand and the standard 16-byte GCM authentication tag.
const (
	KeyLen   = 32
	NonceLen = 12
	TagLen   = 16
)

// Encryptor holds the single system-wide 256-bit AEAD key (Q2). There is no
// key rotation.
type Encryptor struct {
	aead cipher.AEAD
}

// New constructs an Encryptor over the system-wide AEAD key.
func New(key []byte) (*Encryptor, error) {
	if len(key) != KeyLen {
		return nil, fmt.Errorf("encryptor: AEAD key must be %d bytes, got %d", KeyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCMWithNonceSize(block, NonceLen)
	if err != nil {
		return nil, err
	}
	return &Encryptor{aead: aead}, nil
}

// Encrypt performs (c, t) <- Enc_K(M, AD) of Eq. (14).
//
// Framing per Q1: c = nonce || ciphertext, and t is the 16-byte GCM tag. This
// is a recorded deliberate deviation from the paper's nonce-less c.
func (e *Encryptor) Encrypt(m, ad []byte) (c, t []byte, err error) {
	nonce := make([]byte, NonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, err
	}
	sealed := e.aead.Seal(nil, nonce, m, ad)
	if len(sealed) < TagLen {
		return nil, nil, errors.New("encryptor: sealed output shorter than the tag")
	}
	ct := sealed[:len(sealed)-TagLen]
	t = sealed[len(sealed)-TagLen:]
	c = make([]byte, 0, NonceLen+len(ct))
	c = append(c, nonce...)
	c = append(c, ct...)
	return c, t, nil
}

// LeafDigest computes d = H(0x00 || c || t || AD) — Eq. (15), and the leaf
// digest of Eq. (2)/(20).
func LeafDigest(c, t, ad []byte) hashing.Digest {
	return hashing.H(hashing.Tag(hashing.TagLeaf), c, t, ad)
}

// AttestationPreImage computes m_P = SHA256(0xA3 || d) — the value signed by
// Eq. (16). The signature is over this pre-image, not over d.
func AttestationPreImage(d hashing.Digest) hashing.Digest {
	return hashing.H(hashing.Tag(hashing.TagAttestation), d[:])
}

// Attest computes sigma = Ed25519.Sign(sk_P, SHA256(0xA3 || d)) — Eq. (16).
func Attest(sk ed25519.PrivateKey, d hashing.Digest) []byte {
	m := AttestationPreImage(d)
	return ed25519.Sign(sk, m[:])
}

// VerifyAttestation checks Ver_pkP(sigma, H(0xA3 || d)) = 1 — Eq. (18).
func VerifyAttestation(pk ed25519.PublicKey, d hashing.Digest, sigma []byte) bool {
	if len(pk) != ed25519.PublicKeySize || len(sigma) != ed25519.SignatureSize {
		return false
	}
	m := AttestationPreImage(d)
	return ed25519.Verify(pk, m[:], sigma)
}

// Emitted is exactly the Phase 1 output tuple (c, t, AD, d, sigma, pk_P) of
// Eq. (17). It carries everything Phase 2 needs to validate statelessly.
type Emitted struct {
	C                 []byte
	T                 []byte
	AD                []byte
	D                 hashing.Digest
	Sigma             []byte
	ProducerPublicKey ed25519.PublicKey
}

// Process runs the whole producer-side Phase 1 pipeline for one raw record:
// canonicalize (Eq. 13), encrypt with metadata binding (Eq. 14), compute the
// leaf digest (Eq. 15), and attest (Eq. 16).
func (e *Encryptor) Process(raw []byte, sk ed25519.PrivateKey) (Emitted, []byte, error) {
	rec, err := ParseRaw(raw)
	if err != nil {
		return Emitted{}, nil, err
	}
	m := CanonicalBytes(rec)
	em, err := e.ProcessRecord(rec, sk)
	return em, m, err
}

// ProcessRecord runs Phase 1 for an already-parsed logical record.
func (e *Encryptor) ProcessRecord(rec Record, sk ed25519.PrivateKey) (Emitted, error) {
	m := CanonicalBytes(rec)
	ad := ADOf(rec)
	c, t, err := e.Encrypt(m, ad)
	if err != nil {
		return Emitted{}, err
	}
	d := LeafDigest(c, t, ad)
	sigma := Attest(sk, d)
	return Emitted{
		C:                 c,
		T:                 t,
		AD:                ad,
		D:                 d,
		Sigma:             sigma,
		ProducerPublicKey: sk.Public().(ed25519.PublicKey),
	}, nil
}
