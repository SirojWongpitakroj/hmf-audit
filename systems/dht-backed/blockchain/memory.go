package blockchain

import (
	"context"
	"fmt"

	"dhtbacked/internal/hashing"
)

// Memory is a test double for the anchoring chain, used only so that
// pure-logic tests can run without a Hardhat process.
//
// The fidelity reproduction anchors on the real external Hardhat v2.20 chain
// (Q38); this type is never used on any measured path and never substitutes
// for it in a reported result. It reproduces the contract's two enforced
// behaviours: append-only storage and rejection of an overwrite.
type Memory struct {
	roots map[uint64]hashing.Digest
}

// NewMemory constructs the in-memory test double.
func NewMemory() *Memory { return &Memory{roots: make(map[uint64]hashing.Digest)} }

// Commit anchors an epoch root, rejecting an overwrite.
func (m *Memory) Commit(_ context.Context, epoch uint64, root hashing.Digest) error {
	if _, ok := m.roots[epoch]; ok {
		return fmt.Errorf("blockchain: epoch %d already committed", epoch)
	}
	m.roots[epoch] = root
	return nil
}

// GetRoot reads an anchored root.
func (m *Memory) GetRoot(_ context.Context, epoch uint64) (hashing.Digest, error) {
	r, ok := m.roots[epoch]
	if !ok {
		return hashing.Digest{}, ErrNotAnchored
	}
	return r, nil
}

// Overwrite replaces an anchored root without the contract's append-only
// check. It exists so tests can model an adversary that reaches the anchor;
// the contract itself rejects this.
func (m *Memory) Overwrite(epoch uint64, root hashing.Digest) { m.roots[epoch] = root }
