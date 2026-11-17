// Package blockchain is the chain-facing boundary of EVSEB (Phase 1 Step 2,
// Table 3). Only the four functions the paper names are exposed.
//
// Two backends implement BlockchainContracts:
//   - Ethereum (ethereum.go): the VerifyAccess / VerifyIntegrity Solidity
//     contracts (contracts/*.sol) on a local Hardhat chain (Q1/Q2 rulings).
//   - Memory: an in-process stand-in with the same check, for unit tests and
//     for the harness experiments that don't measure the chain.
//
// VerifyAccess on both backends follows the Q1 ruling. Tokenu is decrypted
// with Kauth off-chain; the claims (access.Claims) are checked by the
// contract against the on-chain policy record. So each backend value holds
// Kauth and Kindex for its off-chain half; the contract never sees them.
package blockchain

import (
	"bytes"
	"errors"
	"time"

	"evseb/internal/access"
	"evseb/internal/crypto"
)

// BlockchainContracts is the Section 6 contract interface.
type BlockchainContracts interface {
	// VerifyAccess(Tokenu, LType) -> Allow/Deny (Eq. 21).
	VerifyAccess(tokenCT []byte, lType string) (bool, error)
	// VerifyIntegrity() -> Merkle Root (Phase 1 Step 2 item 2).
	VerifyIntegrity() (merkleRoot []byte, err error)
	// UpdateMerkleRoot stores a new root (Table 3; B17 pass-through).
	UpdateMerkleRoot(newRoot []byte) error
	// GrantAccess stores a user's CTPolicy keyed by H(UserID) (Table 3,
	// Step 3.1 line 9 Blockchain_Store; B17 pass-through).
	GrantAccess(userID string, policyCT []byte) error
}

// ErrNoRoot is returned by VerifyIntegrity before any root was anchored.
var ErrNoRoot = errors.New("blockchain: no merkle root stored")

// policyRecord is the on-chain record of VerifyAccess.sol: CTPolicy (Eq. 6)
// and the commitment SHA-256(Policy) (QUESTIONS.md Q11).
type policyRecord struct {
	ct         []byte
	commitment [32]byte
}

// commitmentOf is the off-chain step grantAccess needs: decrypt CTPolicy with
// Kindex and commit to the Eq. 5 Policy bytes.
func commitmentOf(policyCT []byte, kindex [32]byte) ([32]byte, error) {
	p, err := access.DecryptPolicy(policyCT, kindex)
	if err != nil {
		return [32]byte{}, err
	}
	return p.Commitment(), nil
}

// Memory is an in-memory BlockchainContracts. Its "chain state" is exactly
// the two on-chain items of Section 11: the policy record per H(UserID) and
// the Merkle root R.
type Memory struct {
	kauth, kindex [32]byte
	now           func() time.Time
	root          []byte
	policies      map[[32]byte]policyRecord
}

// NewMemory deploys the in-memory VerifyAccess/VerifyIntegrity contracts. It
// holds Kauth/Kindex only for the off-chain half of VerifyAccess and GrantAccess.
func NewMemory(kauth, kindex [32]byte, now func() time.Time) *Memory {
	return &Memory{kauth: kauth, kindex: kindex, now: now, policies: map[[32]byte]policyRecord{}}
}

// VerifyAccess: off-chain, decrypt Tokenu (Kauth) and CTPolicy (Kindex) into
// claims; then the contract check access.CheckClaims. Undecodable tokens and
// unknown users are denied, not reported as errors.
func (m *Memory) VerifyAccess(tokenCT []byte, lType string) (bool, error) {
	tok, err := access.DecryptToken(tokenCT, m.kauth)
	if err != nil {
		return false, nil
	}
	rec, ok := m.policies[crypto.H([]byte(tok.UserID))]
	if !ok {
		return false, nil
	}
	claims, err := access.BuildClaims(tok, rec.ct, m.kindex)
	if err != nil {
		return false, nil
	}
	return access.CheckClaims(claims, rec.commitment, lType, m.now().Unix()), nil
}

func (m *Memory) VerifyIntegrity() ([]byte, error) {
	if m.root == nil {
		return nil, ErrNoRoot
	}
	return bytes.Clone(m.root), nil
}

func (m *Memory) UpdateMerkleRoot(newRoot []byte) error {
	m.root = bytes.Clone(newRoot)
	return nil
}

func (m *Memory) GrantAccess(userID string, policyCT []byte) error {
	c, err := commitmentOf(policyCT, m.kindex)
	if err != nil {
		return err
	}
	m.policies[crypto.H([]byte(userID))] = policyRecord{ct: bytes.Clone(policyCT), commitment: c}
	return nil
}

// StoredPolicy reads back a stored CTPolicy (Blockchain_Retrieve, Step 3.2
// line 2).
func (m *Memory) StoredPolicy(userID string) ([]byte, bool) {
	r, ok := m.policies[crypto.H([]byte(userID))]
	return bytes.Clone(r.ct), ok
}
