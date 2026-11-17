package dht

import (
	"crypto/ed25519"
	"errors"

	"dhtbacked/auth"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
)

// Lookup failures.
var (
	ErrNotFound    = errors.New("dht: no record at that key")
	ErrKeyMismatch = errors.New("dht: supplied key does not derive from the request envelope")
)

// DHT is the deterministic in-process test DHT: 16 logical nodes with
// replication factor 1, synchronous lookups, and no routing-delay simulation
// (Q10). The persistent request path uses internal/cassandradht instead.
//
// Records are held per logical node in a Go map keyed by the lookup key. The
// map itself is never serialized (Q12).
type DHT struct {
	nodes       [NodeCount]map[hashing.Digest][]byte
	caPublicKey ed25519.PublicKey
	clock       clock.Clock
}

// New constructs the simulated DHT. pkCA is configured into the DHT verifier
// before execution (Q4); it is the key the access gate checks tokens against.
func New(pkCA ed25519.PublicKey, c clock.Clock) *DHT {
	d := &DHT{caPublicKey: pkCA, clock: c}
	for i := range d.nodes {
		d.nodes[i] = make(map[hashing.Digest][]byte)
	}
	return d
}

// put stores a value, overwriting any previous value at the same key. A newer
// write to the same logical object replaces the previous one (Q45).
func (d *DHT) put(key hashing.Digest, value []byte) {
	d.nodes[NodeIndex(key)][key] = value
}

func (d *DHT) get(key hashing.Digest) ([]byte, error) {
	v, ok := d.nodes[NodeIndex(key)][key]
	if !ok {
		return nil, ErrNotFound
	}
	out := make([]byte, len(v))
	copy(out, v)
	return out, nil
}

// PutParent publishes P_j,k at Key_P.
func (d *DHT) PutParent(p ParentNode) { d.put(KeyP(p.TreeID, p.J, p.K), p.Serialize()) }

// PutCert publishes C_s,k at Key_C.
func (d *DHT) PutCert(c AncestorCert) { d.put(KeyC(c.TreeID, c.Level, c.Index), c.Serialize()) }

// PutRoot publishes the root record at Key_R.
func (d *DHT) PutRoot(rr RootRecord) { d.put(KeyR(rr.TreeID), rr.Serialize()) }

// PutMicroRoot publishes a micro-root record at Key_M, overwriting the
// previous provisional value for that window (Q53a).
func (d *DHT) PutMicroRoot(m MicroRootRecord) { d.put(KeyM(m.TreeID, m.WindowID), m.Serialize()) }

// PutRaw writes arbitrary bytes at an arbitrary key. The DHT is
// adversary-controlled (R-DHT-07); this is the handle tests use to corrupt a
// stored record.
func (d *DHT) PutRaw(key hashing.Digest, value []byte) { d.put(key, value) }

// GetRaw reads bytes at a key with no authorization check. It is used by the
// system's own publication path, never by an auditor.
func (d *DHT) GetRaw(key hashing.Digest) ([]byte, error) { return d.get(key) }

// Count returns the number of records held across all logical nodes.
func (d *DHT) Count() int {
	n := 0
	for i := range d.nodes {
		n += len(d.nodes[i])
	}
	return n
}

// CountAt returns the number of records held by one logical node.
func (d *DHT) CountAt(node int) int { return len(d.nodes[node]) }

// ParentRequest is the Q41 typed envelope for a parent node record.
// No envelope carries seq.
type ParentRequest struct {
	Epoch   uint64
	LogType string
	J       uint64
	K       uint64
	Key     hashing.Digest
}

// CertRequest is the Q41 typed envelope for an ancestor certificate.
type CertRequest struct {
	Epoch   uint64
	LogType string
	S       uint64
	K       uint64
	Key     hashing.Digest
}

// RootRequest is the Q41 typed envelope for a root record.
type RootRequest struct {
	Epoch   uint64
	LogType string
	Key     hashing.Digest
}

// MicroRootRequest is the Q41 typed envelope for a micro-root record.
type MicroRootRequest struct {
	Epoch    uint64
	LogType  string
	WindowID uint64
	Key      hashing.Digest
}

// authorize performs the Phase 5(a) gate: it verifies the CA signature, that
// the requested epoch equals the token epoch, and that the requested log type
// is permitted; then it recomputes treeID and the expected object key and
// rejects a mismatch (Q13, Q41).
//
// On any failure it returns an authorization failure and no proof data.
func (d *DHT) authorize(tok *auth.Token, epoch uint64, logType string, supplied hashing.Digest, expected func(hashing.Digest) hashing.Digest) ([]byte, error) {
	if err := auth.Verify(d.caPublicKey, tok, d.clock.Now(), epoch, logType); err != nil {
		return nil, err
	}
	treeID := TreeID(epoch, logType)
	if expected(treeID) != supplied {
		return nil, ErrKeyMismatch
	}
	return d.get(supplied)
}

// GetParent serves a P_j,k record to an authorized auditor.
func (d *DHT) GetParent(tok *auth.Token, req ParentRequest) ([]byte, error) {
	return d.authorize(tok, req.Epoch, req.LogType, req.Key, func(t hashing.Digest) hashing.Digest {
		return KeyP(t, req.J, req.K)
	})
}

// GetCert serves a C_s,k record to an authorized auditor.
func (d *DHT) GetCert(tok *auth.Token, req CertRequest) ([]byte, error) {
	return d.authorize(tok, req.Epoch, req.LogType, req.Key, func(t hashing.Digest) hashing.Digest {
		return KeyC(t, req.S, req.K)
	})
}

// GetRoot serves a root record to an authorized auditor.
func (d *DHT) GetRoot(tok *auth.Token, req RootRequest) ([]byte, error) {
	return d.authorize(tok, req.Epoch, req.LogType, req.Key, func(t hashing.Digest) hashing.Digest {
		return KeyR(t)
	})
}

// GetMicroRoot serves a micro-root record to an authorized auditor.
func (d *DHT) GetMicroRoot(tok *auth.Token, req MicroRootRequest) ([]byte, error) {
	return d.authorize(tok, req.Epoch, req.LogType, req.Key, func(t hashing.Digest) hashing.Digest {
		return KeyM(t, req.WindowID)
	})
}

// ForEach visits every stored record so its contents can be locally rehashed
// and reconciled against the anchor (R-DHT-06).
//
// Iteration order is unspecified and must never feed a hash computation; the
// system's own hashing paths address records by key.
func (d *DHT) ForEach(visit func(key hashing.Digest, value []byte)) {
	for i := range d.nodes {
		for k, v := range d.nodes[i] {
			visit(k, v)
		}
	}
}
