// Package sig implements the (Gen, Sign, Vrfy) digital signature scheme
// (CRYPTO-001 "digital signature scheme"; §4.3.1 p. 3291).
//
// The paper gives ECDSA only as an example ("e.g., ECDSA") and names no
// curve (CRYPTO-004). Per the frozen GAP-27(b) ruling, this is an
// IMPLEMENTATION-NECESSARY-BUT-UNSPECIFIED choice, not paper-mandated:
// secp256k1 ECDSA, signing a 32-byte SHA-256 digest (the sole H(.) in the
// system, CRYPTO-002). A participant's Ethereum address (used wherever
// OffList/NewLeader/OnlineMsg reference an "address") is derived from that
// same secp256k1 key; there is no second identity mapping (CRYPTO-008,
// GAP-28).
//
// Keys are generated at provisioning and stored locally; there is no runtime
// PKI (CRYPTO-009, GAP-49). This package does not implement signature
// aggregation (SRS Section 26, item 14).
package sig

import (
	"crypto/ecdsa"
	"errors"

	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// PublicKeySize is the size in bytes of the canonical public-key encoding
// frozen by GAP-73: compressed secp256k1 SEC1 point encoding.
const PublicKeySize = 33

// SignatureSize is the size in bytes of a Sign output: a compact (R||S)
// ECDSA signature, without a recovery id.
const SignatureSize = 64

// KeyPair holds one participant's secp256k1 key pair.
type KeyPair struct {
	Priv *ecdsa.PrivateKey
}

// Gen generates a new secp256k1 key pair (CRYPTO-009: generated at
// provisioning).
func Gen() (*KeyPair, error) {
	priv, err := gethcrypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	return &KeyPair{Priv: priv}, nil
}

// PublicKey returns the canonical compressed secp256k1 public-key encoding
// (33 bytes, GAP-73). This is the encoding used both for signing context and
// for the CONFIRM-003 big-endian key comparison (the encoding is already a
// big-endian byte sequence, so lexicographic byte comparison is the
// comparison).
func (k *KeyPair) PublicKey() []byte {
	return gethcrypto.CompressPubkey(&k.Priv.PublicKey)
}

// Address returns the Ethereum address derived from this key (CRYPTO-008):
// no second identity mapping. This is what OffList, NewLeader and OnlineMsg
// "addresses" mean.
func (k *KeyPair) Address() [20]byte {
	return gethcrypto.PubkeyToAddress(k.Priv.PublicKey)
}

// Bytes returns the raw 32-byte big-endian private scalar, for local storage
// (CRYPTO-009: no runtime PKI).
func (k *KeyPair) Bytes() []byte {
	return gethcrypto.FromECDSA(k.Priv)
}

// Load reconstructs a KeyPair from the raw 32-byte big-endian private scalar
// produced by Bytes.
func Load(b []byte) (*KeyPair, error) {
	priv, err := gethcrypto.ToECDSA(b)
	if err != nil {
		return nil, err
	}
	return &KeyPair{Priv: priv}, nil
}

// Sign signs a 32-byte digest with SK, returning a 64-byte compact (R||S)
// ECDSA signature. The recovery id byte that the underlying library produces
// is dropped: verification here always happens against an explicitly known
// public key (Vrfy), never by recovery.
func (k *KeyPair) Sign(digest [32]byte) ([]byte, error) {
	sig, err := gethcrypto.Sign(digest[:], k.Priv)
	if err != nil {
		return nil, err
	}
	return sig[:SignatureSize], nil
}

// DecodePublicKey parses the canonical compressed public-key encoding.
func DecodePublicKey(b []byte) (*ecdsa.PublicKey, error) {
	return gethcrypto.DecompressPubkey(b)
}

// Vrfy verifies a 64-byte compact (R||S) signature over digest against the
// canonically-encoded public key pubKey.
func Vrfy(pubKey []byte, digest [32]byte, signature []byte) bool {
	if len(signature) != SignatureSize {
		return false
	}
	if len(pubKey) != PublicKeySize {
		return false
	}
	return gethcrypto.VerifySignature(pubKey, digest[:], signature)
}

// AddressFromPublicKey derives the Ethereum address from a canonically
// encoded public key, without needing the private key (CRYPTO-008).
func AddressFromPublicKey(pubKey []byte) ([20]byte, error) {
	pk, err := DecodePublicKey(pubKey)
	if err != nil {
		return [20]byte{}, errors.New("sig: invalid public key encoding")
	}
	return gethcrypto.PubkeyToAddress(*pk), nil
}
