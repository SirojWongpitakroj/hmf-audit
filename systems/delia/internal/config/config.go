// Package config loads deployment/experiment configuration: the domain's
// participant set P (in the order frozen at Init, GAP-08) and theta
// (DATA-007, required with no protocol default, GAP-55). This is local
// tooling configuration, not a DELIA wire message — the SRS Section 26
// prohibition on JSON/REST/etc. as DELIA *requirements* (GAP-47) governs the
// off-chain protocol messages (see the messages package), not a node's own
// startup config file.
package config

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"delia/sig"
)

// Participant is one domain member's identity and network endpoint.
// PublicKey is the canonical compressed secp256k1 encoding (GAP-73,
// sig.PublicKeySize bytes). Order in Domain.Participants is the P order
// frozen at Init (DATA-013/GAP-08): position i is Participants[i].
type Participant struct {
	ID        string `json:"id"`
	PublicKey []byte `json:"-"`
	Endpoint  string `json:"endpoint"`

	PublicKeyHex string `json:"public_key"`
}

// Domain is one deployment's fixed configuration.
type Domain struct {
	// Theta is DATA-007's required batch size; GAP-55 rules out a global
	// default. A zero or missing value is a configuration error.
	Theta int `json:"theta"`

	Participants []Participant `json:"participants"`
}

// Load parses a domain config file (JSON; a local tooling format, not a
// DELIA wire message). It validates that theta and every participant's
// public key are present and well-formed (SYS-002/GAP-49: P and keys are
// pre-negotiated out of band, not invented defaults).
func Load(path string) (*Domain, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(raw)
}

// Parse parses domain config bytes (see Load).
func Parse(raw []byte) (*Domain, error) {
	var d Domain
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	return &d, nil
}

// Validate checks the required-configuration invariants (DATA-007/GAP-55: no
// theta default; SYS-002/GAP-49: every participant needs a valid canonical
// public key).
func (d *Domain) Validate() error {
	if d.Theta <= 0 {
		return errors.New("config: theta is required and must be > 0 (GAP-55: no global default)")
	}
	if len(d.Participants) == 0 {
		return errors.New("config: at least one participant is required")
	}
	for i := range d.Participants {
		p := &d.Participants[i]
		if p.ID == "" {
			return fmt.Errorf("config: participant %d missing id", i)
		}
		key, err := hex.DecodeString(p.PublicKeyHex)
		if err != nil {
			return fmt.Errorf("config: participant %s: invalid public_key hex: %w", p.ID, err)
		}
		if len(key) != sig.PublicKeySize {
			return fmt.Errorf("config: participant %s: public key length %d, want %d (canonical compressed secp256k1, GAP-73)", p.ID, len(key), sig.PublicKeySize)
		}
		if _, err := sig.DecodePublicKey(key); err != nil {
			return fmt.Errorf("config: participant %s: invalid public key encoding: %w", p.ID, err)
		}
		p.PublicKey = key
	}
	return nil
}

// Index returns the frozen P-order position of the participant with the
// given id, or -1 if not present.
func (d *Domain) Index(id string) int {
	for i, p := range d.Participants {
		if p.ID == id {
			return i
		}
	}
	return -1
}
