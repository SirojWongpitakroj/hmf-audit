// Package ingestverifier is the dedicated ingestion-verifier module of Phase 2
// (Q20). It runs before partitioning and MK-Tree insertion and is separate
// from the auditor-side verifier.
package ingestverifier

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// registryEntry is one line of the deterministic JSON producer registry.
//
// The registry is an array sorted by producer_id rather than a JSON object, so
// no Go map is ever serialized (Q12). The SRS requires "deterministic JSON"
// (Q46) without fixing the field names; these are stable and explicit.
type registryEntry struct {
	ProducerID   string `json:"producer_id"`
	PublicKeyHex string `json:"public_key_hex"`
}

type registryFile struct {
	Producers []registryEntry `json:"producers"`
}

// Registry is the pre-registered ProducerID -> pk_P mapping of Q5. It is
// loaded before ingestion begins and is not modified afterwards.
type Registry struct {
	keys map[string]ed25519.PublicKey
}

// WriteRegistry serializes a registry deterministically: entries sorted by
// producer_id, compact JSON, raw 32-byte Ed25519 public keys in lowercase hex.
func WriteRegistry(path string, entries map[string]ed25519.PublicKey) error {
	ids := make([]string, 0, len(entries))
	for id := range entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	f := registryFile{Producers: make([]registryEntry, 0, len(ids))}
	for _, id := range ids {
		f.Producers = append(f.Producers, registryEntry{
			ProducerID:   id,
			PublicKeyHex: hex.EncodeToString(entries[id]),
		})
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// LoadRegistry reads the producer registry from the path configured as
// DHT_PRODUCER_REGISTRY_PATH (Q46). It fails on a malformed file or a key of
// the wrong length.
func LoadRegistry(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f registryFile
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("ingestverifier: registry: %w", err)
	}
	r := &Registry{keys: make(map[string]ed25519.PublicKey, len(f.Producers))}
	for _, e := range f.Producers {
		raw, err := hex.DecodeString(e.PublicKeyHex)
		if err != nil {
			return nil, fmt.Errorf("ingestverifier: registry: %s: %w", e.ProducerID, err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("ingestverifier: registry: %s: public key is %d bytes, want %d",
				e.ProducerID, len(raw), ed25519.PublicKeySize)
		}
		if _, dup := r.keys[e.ProducerID]; dup {
			return nil, fmt.Errorf("ingestverifier: registry: duplicate producer %s", e.ProducerID)
		}
		r.keys[e.ProducerID] = ed25519.PublicKey(raw)
	}
	return r, nil
}

// NewRegistry builds a registry in memory.
func NewRegistry(entries map[string]ed25519.PublicKey) *Registry {
	r := &Registry{keys: make(map[string]ed25519.PublicKey, len(entries))}
	for id, pk := range entries {
		r.keys[id] = pk
	}
	return r
}

// Lookup returns the registered public key for a producer.
func (r *Registry) Lookup(producerID string) (ed25519.PublicKey, bool) {
	pk, ok := r.keys[producerID]
	return pk, ok
}
