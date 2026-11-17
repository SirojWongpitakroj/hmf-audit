// Package auth implements Phase 5(a): the in-process Certification Authority,
// the auditor enrollment credential, and the per-epoch authorization token
// tau_A that gates DHT proof access (Q4, Q13, Q40).
package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"sort"
	"time"

	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/internal/serial"
)

// TokenValidity is the fixed authorization-token lifetime: exactly one hour
// from issuance (Q40). There is no clock-skew grace period.
const TokenValidity = time.Hour

// TokenVersion is the version field of the token body (Q13).
const TokenVersion = uint64(1)

// CA is the in-process Certification Authority (Q4). Its key pair is Ed25519
// and its encoding is deterministic binary — not X.509 and not JWT.
type CA struct {
	priv  ed25519.PrivateKey
	pub   ed25519.PublicKey
	clock clock.Clock
}

// NewCA constructs a CA over an existing Ed25519 private key.
func NewCA(priv ed25519.PrivateKey, c clock.Clock) (*CA, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("auth: CA private key is %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
	}
	if c == nil {
		return nil, errors.New("auth: a Clock is required")
	}
	return &CA{priv: priv, pub: priv.Public().(ed25519.PublicKey), clock: c}, nil
}

// PublicKey returns pk_CA, which is configured into the DHT verifier before
// execution via DHT_CA_PUBLIC_KEY_PATH (Q4, Q46).
func (ca *CA) PublicKey() ed25519.PublicKey { return ca.pub }

// LoadOrCreateCAKeys loads the CA key pair from the configured paths,
// generating it with crypto/rand only if neither file exists. Key files hold
// raw Ed25519 bytes (deterministic binary, Q4).
func LoadOrCreateCAKeys(privPath, pubPath string) (ed25519.PrivateKey, ed25519.PublicKey, error) {
	priv, privErr := os.ReadFile(privPath)
	pub, pubErr := os.ReadFile(pubPath)
	switch {
	case privErr == nil && pubErr == nil:
		if len(priv) != ed25519.PrivateKeySize {
			return nil, nil, fmt.Errorf("auth: CA private key file is %d bytes, want %d", len(priv), ed25519.PrivateKeySize)
		}
		if len(pub) != ed25519.PublicKeySize {
			return nil, nil, fmt.Errorf("auth: CA public key file is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
		}
		return ed25519.PrivateKey(priv), ed25519.PublicKey(pub), nil
	case os.IsNotExist(privErr) && os.IsNotExist(pubErr):
		newPub, newPriv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(privPath, newPriv, 0o600); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(pubPath, newPub, 0o644); err != nil {
			return nil, nil, err
		}
		return newPriv, newPub, nil
	default:
		if privErr != nil {
			return nil, nil, privErr
		}
		return nil, nil, pubErr
	}
}

// LoadCAPublicKey reads pk_CA from DHT_CA_PUBLIC_KEY_PATH.
func LoadCAPublicKey(path string) (ed25519.PublicKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("auth: CA public key file is %d bytes, want %d", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// Credential is the auditor enrollment credential. It is deliberately kept
// distinct from the per-epoch authorization token (Q4) and is not accepted by
// the DHT access gate.
//
// The SRS states only that the credential exists, is issued by the CA, and is
// distinct from tau_A; no field set is specified, so it carries the minimum
// that makes it an enrollment record and introduces no additional mechanism.
type Credential struct {
	Version      uint64
	AuditorID    string
	IssuedAtUnix int64
	Signature    []byte
}

const credentialVersion = uint64(1)

func credentialBody(version uint64, auditorID string, issuedAt int64) []byte {
	w := serial.NewWriter(serial.TypeTokenBody)
	w.Uint64(0xC0) // credential discriminator: a credential is not a token body
	w.Uint64(version)
	w.String(auditorID)
	w.Uint64(uint64(issuedAt))
	return w.Result()
}

// Enroll issues an auditor enrollment credential.
func (ca *CA) Enroll(auditorID string) Credential {
	issued := ca.clock.Now().UTC().Unix()
	body := credentialBody(credentialVersion, auditorID, issued)
	h := hashing.H(body)
	return Credential{
		Version:      credentialVersion,
		AuditorID:    auditorID,
		IssuedAtUnix: issued,
		Signature:    ed25519.Sign(ca.priv, h[:]),
	}
}

// VerifyCredential checks an enrollment credential against pk_CA.
func VerifyCredential(pk ed25519.PublicKey, c Credential) bool {
	body := credentialBody(c.Version, c.AuditorID, c.IssuedAtUnix)
	h := hashing.H(body)
	return len(c.Signature) == ed25519.SignatureSize && ed25519.Verify(pk, h[:], c.Signature)
}

// Issue issues the per-epoch authorization token tau_A for an audit request
// (Q13, Q40).
//
// allowedTypes is the `policy` field, interpreted as the set of permitted log
// types. It is sorted lexicographically before serialization so the signed
// body is deterministic.
func (ca *CA) Issue(auditorID string, epoch uint64, allowedTypes []string) Token {
	types := append([]string(nil), allowedTypes...)
	sort.Strings(types)
	body := TokenBody{
		Version:       TokenVersion,
		AuditorID:     auditorID,
		Epoch:         epoch,
		AllowedTypes:  types,
		ExpiresAtUnix: ca.clock.Now().UTC().Add(TokenValidity).Unix(),
	}
	h := hashing.H(body.Serialize())
	return Token{Body: body, Signature: ed25519.Sign(ca.priv, h[:])}
}
