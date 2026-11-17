package auth

import (
	"crypto/ed25519"
	"errors"
	"time"

	"dhtbacked/internal/hashing"
	"dhtbacked/internal/serial"
)

// TokenBody is the signed token body: exactly the five fields of Q13, in this
// order — (version, auditorID, epoch, allowedTypes, expiresAt).
//
// `policy` is interpreted as allowedTypes, the set of permitted log types
// (Q13). expiresAt is Unix seconds (Q40).
type TokenBody struct {
	Version       uint64
	AuditorID     string
	Epoch         uint64
	AllowedTypes  []string
	ExpiresAtUnix int64
}

// Token is tau_A = Ed25519.Sign(sk_CA, SHA256(TokenBody)) together with the
// body it was signed over (Q13).
type Token struct {
	Body      TokenBody
	Signature []byte
}

// Serialize renders the token body deterministically: the one-byte record-type
// prefix, then the five fields in fixed order, with the allowed-type list
// written as a count followed by that many length-prefixed strings. No Go map
// is involved (Q12).
func (b TokenBody) Serialize() []byte {
	w := serial.NewWriter(serial.TypeTokenBody)
	w.Uint64(b.Version)
	w.String(b.AuditorID)
	w.Uint64(b.Epoch)
	w.Uint64(uint64(len(b.AllowedTypes)))
	for _, t := range b.AllowedTypes {
		w.String(t)
	}
	w.Uint64(uint64(b.ExpiresAtUnix))
	return w.Result()
}

// Authorization failures. On any failure the DHT returns an authorization
// failure and no proof data (Q13, R-AUTH-04).
var (
	ErrNoToken          = errors.New("auth: no authorization token presented")
	ErrMalformedToken   = errors.New("auth: malformed authorization token")
	ErrBadSignature     = errors.New("auth: token signature does not verify under pk_CA")
	ErrEpochMismatch    = errors.New("auth: requested epoch does not equal the token epoch")
	ErrTypeNotPermitted = errors.New("auth: requested log type is not in allowedTypes")
	ErrTokenExpired     = errors.New("auth: token has expired")
)

// Verify performs all four checks Q13 requires, in order:
//
//	(i)   the CA signature verifies under pk_CA;
//	(ii)  the requested epoch equals the token epoch;
//	(iii) the requested log type is in allowedTypes;
//	(iv)  now < expiresAt.
//
// There is no clock-skew grace period: a token is rejected at or after
// expiresAt (Q40).
func Verify(pk ed25519.PublicKey, tok *Token, now time.Time, epoch uint64, logType string) error {
	if tok == nil {
		return ErrNoToken
	}
	if len(tok.Signature) != ed25519.SignatureSize || tok.Body.Version != TokenVersion || tok.Body.AuditorID == "" {
		return ErrMalformedToken
	}
	h := hashing.H(tok.Body.Serialize())
	if !ed25519.Verify(pk, h[:], tok.Signature) {
		return ErrBadSignature
	}
	if tok.Body.Epoch != epoch {
		return ErrEpochMismatch
	}
	permitted := false
	for _, t := range tok.Body.AllowedTypes {
		if t == logType {
			permitted = true
			break
		}
	}
	if !permitted {
		return ErrTypeNotPermitted
	}
	if !now.UTC().Before(time.Unix(tok.Body.ExpiresAtUnix, 0).UTC()) {
		return ErrTokenExpired
	}
	return nil
}
