// Package offline implements the Tier-A slices of the offline/online
// client flow M7 of TRACKER.md licenses now: OfflineMsg's signer-set check
// in isolation (OFFLINE-005/006, TEST-48, DEV-03), the H(psi) active-queue
// hash it needs (GAP-42, DEV-17), and the rejoin-sync off-chain half of
// TEST-37 (OFFLINE-015, DEV-12): a returning participant's GlobalState/
// LogCacheQueue data synced from its DIRECT leader only.
//
// The full challenge-window mechanics (start-block storage, one-block
// cancellation window, permissionless FinalizeOffline) stay out of scope
// (OFFLINE-002/003/004/007/008/009/016, Tier B/C, GAP-85 residual).
package offline

import (
	"errors"

	"delia/globalstate"
	"delia/hash"
	"delia/internal/spec"
	"delia/sig"
)

// HashActiveQueue implements GAP-42's H(psi_{pi,li}): SHA-256 over the raw
// 32-byte digests of pi's current ACTIVE LogCacheQueue, including a partial
// batch, in record order (DEV-03, DEV-17) — concatenated as raw bytes, the
// same convention every other H(.) call site in this reproduction uses
// (CRYPTO-001/002, hash.Concat).
func HashActiveQueue(psi []hash.Digest) hash.Digest {
	parts := make([][]byte, len(psi))
	for i, d := range psi {
		parts[i] = d[:]
	}
	return hash.H(hash.Concat(parts...))
}

// SignerEntry is one signature supplied toward an OfflineMsg's signer set:
// the signing participant's canonical public key plus its signature over
// the OfflineMsg's digest.
type SignerEntry struct {
	PubKey    []byte
	Signature []byte
}

// CheckOfflineMsgSigners implements OFFLINE-005/006's signer-set check IN
// ISOLATION (TEST-48): the expected signer set is every member of p except
// the pending-offline participant pi itself (DEV-03 — pi is excluded from
// the expected signer set for OfflineMsg specifically, unlike an ordinary
// Update). It accepts only when signers is exactly that expected set: every
// non-pi member of p signs exactly once, every supplied signature verifies
// against digest, and no unexpected/duplicate signer appears. This is
// deliberately isolated from the still-blocked challenge-window mechanics
// (GAP-85) — it does not touch startBlock, cancellation or FinalizeOffline
// at all.
func CheckOfflineMsgSigners(p [][]byte, pi []byte, digest hash.Digest, signers []SignerEntry) bool {
	expected := make(map[string]bool)
	for _, pk := range p {
		if string(pk) != string(pi) {
			expected[string(pk)] = true
		}
	}
	if len(signers) != len(expected) {
		return false
	}
	seen := make(map[string]bool, len(expected))
	for _, entry := range signers {
		key := string(entry.PubKey)
		if !expected[key] || seen[key] {
			return false
		}
		if !sig.Vrfy(entry.PubKey, digest, entry.Signature) {
			return false
		}
		seen[key] = true
	}
	return len(seen) == len(expected)
}

// RejoinSyncRequest is OFFLINE-015/DEV-12's rejoin-sync request: a
// returning participant asks its DIRECT leader only (never any other
// server in the domain) for the GlobalState history and current-batch
// LogCacheQueue data it missed.
type RejoinSyncRequest struct {
	FromSerial uint64
	ToSerial   uint64
}

// RejoinSyncResponse is the direct leader's reply: the missing GlobalState
// entries (with the leader's signature on each, same verification shape as
// hmsc/confirm's desync recovery) plus the current-batch LogCacheQueue data
// for every server in the domain, copied as-is (DEV-12: LogCacheQueue sync
// piggybacks on the same rejoin exchange, not a separate protocol).
type RejoinSyncResponse struct {
	States        []RejoinStateEntry
	LogCacheQueue map[string][]hash.Digest // ServerID (string-keyed) -> current batch
}

// RejoinStateEntry mirrors hmsc/confirm.SyncResponseEntry's shape (state +
// leader signature); duplicated here rather than imported, since offline's
// rejoin sync and confirm's desync recovery are triggered by different
// protocol events even though the verification shape coincides.
type RejoinStateEntry struct {
	State     globalstate.GlobalState
	Signature []byte
}

// VerifyRejoinSync verifies a RejoinSyncResponse against req: serial
// continuity across req's range and the direct leader's signature on every
// returned state (same purpose/role pair hmsc/confirm.VerifySyncResponse
// uses, for the same documented reason: these are relayed prior signed
// round states, not the still-open no-signed-state signature purpose,
// GAP-86). On success it returns the verified states in order; the
// LogCacheQueue map is returned unverified (Rule-2/Alg.2-style
// verification against a signed root would require replaying LSG stage 2
// for the missed batches, out of scope for this narrow slice — the
// acceptance criterion TEST-37 checks is that the synced values equal the
// leader's, not an independent re-derivation).
func VerifyRejoinSync(leaderPubKey []byte, req RejoinSyncRequest, resp RejoinSyncResponse) ([]globalstate.GlobalState, error) {
	if req.ToSerial < req.FromSerial {
		return nil, errors.New("offline: empty rejoin range")
	}
	wantCount := req.ToSerial - req.FromSerial + 1
	if uint64(len(resp.States)) != wantCount {
		return nil, errors.New("offline: rejoin response does not cover the requested range")
	}
	want := req.FromSerial
	out := make([]globalstate.GlobalState, 0, len(resp.States))
	for _, e := range resp.States {
		if e.State.Nu != want {
			return nil, errors.New("offline: rejoin response serial continuity broken")
		}
		digest := stateDigest(e.State)
		if !sig.Vrfy(leaderPubKey, digest, e.Signature) {
			return nil, errors.New("offline: rejoin response signature invalid")
		}
		out = append(out, e.State)
		want++
	}
	return out, nil
}

// stateDigest is the signed-bytes digest for a relayed rejoin-sync state
// entry — see VerifyRejoinSync's doc comment for why PurposeSTATE/
// RoleSigner is the documented choice here.
func stateDigest(g globalstate.GlobalState) hash.Digest {
	return spec.SignedDigest(spec.PurposeSTATE, spec.RoleSigner, g.Encode())
}
