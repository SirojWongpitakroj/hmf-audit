package ingestverifier

import (
	"bytes"
	"errors"

	"dhtbacked/encryptor"
)

// Validation outcomes. Phase 2 is a binary accept/discard gate: only verified
// tuples are accepted and failures are discarded (R-ING-03, Eq. (19)). No
// logging, alerting, quarantine or retry is performed.
var (
	ErrUnregisteredProducer = errors.New("ingestverifier: producer is not registered")
	ErrPublicKeyMismatch    = errors.New("ingestverifier: carried public key is not the registered key")
	ErrDigestMismatch       = errors.New("ingestverifier: recomputed digest does not match")
	ErrSignatureInvalid     = errors.New("ingestverifier: producer signature does not verify")
	ErrMalformedTuple       = errors.New("ingestverifier: malformed tuple")
)

// Verifier performs stateless ingestion-time validation. It holds only the
// pre-registered producer key registry; it performs no decryption at any point
// (R-ING-01).
type Verifier struct {
	registry *Registry
}

// New constructs the ingestion verifier over a loaded registry.
func New(r *Registry) *Verifier { return &Verifier{registry: r} }

// Validate performs the Phase 2 check on one emitted tuple (Eq. (17)–(19)):
//
//  1. the producer named in AD is registered, and the public key carried with
//     the record equals the registered key (Q5);
//  2. d' = H(0x00 || c || t || AD) is recomputed and must equal the carried d;
//  3. Ver_pkP(sigma, H(0xA3 || d')) = 1.
//
// No decryption occurs.
func (v *Verifier) Validate(em encryptor.Emitted) error {
	producerID, _, _, _, err := encryptor.ParseAD(em.AD)
	if err != nil {
		return ErrMalformedTuple
	}
	if len(em.T) != encryptor.TagLen || len(em.C) < encryptor.NonceLen {
		return ErrMalformedTuple
	}
	registered, ok := v.registry.Lookup(producerID)
	if !ok {
		return ErrUnregisteredProducer
	}
	if !bytes.Equal(registered, em.ProducerPublicKey) {
		return ErrPublicKeyMismatch
	}
	if encryptor.LeafDigest(em.C, em.T, em.AD) != em.D {
		return ErrDigestMismatch
	}
	if !encryptor.VerifyAttestation(registered, em.D, em.Sigma) {
		return ErrSignatureInvalid
	}
	return nil
}

// Accept returns true when the tuple passes validation. Failing tuples are
// discarded; they contribute no leaf to any MK-Tree and appear in no DHT
// record (R-ING-03 acceptance).
func (v *Verifier) Accept(em encryptor.Emitted) bool { return v.Validate(em) == nil }
