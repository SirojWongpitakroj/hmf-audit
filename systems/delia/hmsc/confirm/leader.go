// Package confirm implements the off-chain-only slice of the state
// confirmation protocol (§4.3.2) that M6 of TRACKER.md licenses as Tier A:
// leader election (CONFIRM-001..004's basic rotation rule, TEST-17/17b),
// desync recovery (CONFIRM-016/017, TEST-21), and beta as required
// per-instance configuration with no protocol default (ARCH-009/010).
//
// This package deliberately does NOT implement:
//   - the SCC on-chain "expected leader derived from the latest accepted
//     signed-round anchor" mechanic (CONFIRM-003/004's on-chain half,
//     ARCH-004/008/011 — Tier C/B, GAP-60/81/82);
//   - parent/child upward-propagation recursion (CONFIRM-010/011/012,
//     GAP-24 OPEN);
//   - the leader's (G_nu, S_leader) signing/broadcast step or a recipient's
//     countersignature step (CONFIRM-008/009 — Tier B, GAP-86 residual;
//     TEST-18, the test that would exercise it, is not Tier A either).
//
// No alternative election/consensus library is used (SRS Section 26 item
// 10): this is exactly the paper's min-key rotation rule, nothing else.
package confirm

import (
	"bytes"
	"errors"
	"sort"
)

// PublicKey is the canonical compressed secp256k1 public-key encoding
// (sig.PublicKeySize bytes, GAP-73). CONFIRM-003/004: keys are compared as
// fixed-width unsigned big-endian values, which — because the canonical
// encoding is already a big-endian byte sequence — is exactly ordinary
// lexicographic byte comparison (bytes.Compare), used throughout this file.
type PublicKey []byte

// Eligible returns the eligible-for-leadership subset of p (CONFIRM-003/004):
// every key in p that is NOT in finalizedOffList, sorted ascending by the
// GAP-73 canonical big-endian key comparison. A key in finalizedOffList that
// is not actually in p is ignored. Excluded participants would also be
// filtered here (GAP-65) once DisputeResolving exclusion exists; this
// milestone does not build that, so only the finalizedOffList filter is
// applied.
//
// DEV-11/TEST-17b: only a FINALIZED on-chain OffList entry excludes a
// participant from eligibility. A merely pending, unfinalized OfflineMsg
// must not affect eligibility — callers enforce this simply by not passing
// pending entries into finalizedOffList at all.
func Eligible(p []PublicKey, finalizedOffList []PublicKey) []PublicKey {
	offline := make(map[string]bool, len(finalizedOffList))
	for _, k := range finalizedOffList {
		offline[string(k)] = true
	}
	out := make([]PublicKey, 0, len(p))
	for _, k := range p {
		if !offline[string(k)] {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i], out[j]) < 0 })
	return out
}

// NextLeader implements CONFIRM-003's rotation rule over an already-sorted
// eligible set (as returned by Eligible): if previousLeader is nil, the
// first leader is the minimum eligible key. Otherwise the next leader is
// the smallest eligible key strictly greater than previousLeader; if none
// exists (previousLeader was the maximum), rotation restarts at the
// minimum of the eligible set.
//
// It is an error to call this with an empty eligible set.
func NextLeader(eligible []PublicKey, previousLeader PublicKey) (PublicKey, error) {
	if len(eligible) == 0 {
		return nil, errors.New("confirm: no eligible participants")
	}
	if previousLeader == nil {
		return eligible[0], nil
	}
	for _, k := range eligible {
		if bytes.Compare(k, previousLeader) > 0 {
			return k, nil
		}
	}
	return eligible[0], nil // wrap
}

// RotationSequence computes `rounds` consecutive leaders starting from
// round 1, over the fixed eligible set derived from p and finalizedOffList
// (TEST-17/17b: the eligible set is evaluated once, up front — a caller
// that needs the OffList to change mid-sequence calls Eligible/NextLeader
// directly per round instead).
func RotationSequence(p []PublicKey, finalizedOffList []PublicKey, rounds int) ([]PublicKey, error) {
	eligible := Eligible(p, finalizedOffList)
	if len(eligible) == 0 {
		return nil, errors.New("confirm: no eligible participants")
	}
	out := make([]PublicKey, 0, rounds)
	var prev PublicKey
	for i := 0; i < rounds; i++ {
		next, err := NextLeader(eligible, prev)
		if err != nil {
			return nil, err
		}
		out = append(out, next)
		prev = next
	}
	return out, nil
}
