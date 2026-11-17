package verifier

import (
	"context"
	"errors"
	"fmt"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/storage"
)

// errNotCovered reports that the published micro-root does not yet cover the
// record's leaf index.
var errNotCovered = fmt.Errorf("%w: the published micro-root does not cover this record", ErrRejected)

// MicroRootResult is the outcome of a provisional real-time check.
//
// It is explicitly NOT an anchored verification (Q22, Q52): a micro-root check
// against an open epoch carries no blockchain guarantee and does not satisfy
// R-SEC-04, R-SEC-05 or R-SEC-06.
type MicroRootResult struct {
	Accepted bool
	// Provisional is always true: the result is provisional until the epoch is
	// finalized and anchored.
	Provisional bool
	// WindowID, IMin and IMax describe the range the published value covers.
	WindowID uint64
	IMin     uint64
	IMax     uint64
	// Covered is the number of real records the micro-root covers (k).
	Covered uint64
}

// VerifyMicroRoot performs the provisional real-time check of Phase 5(b).
//
// It recomputes R^(delta)_tau = H(d_i1 || ... || d_ik) over the real record
// digests in the published range and compares it with the DHT value. Dummy
// padding leaves never participate (Q53b). The result is provisional and is
// the only check available while an epoch is still open (Q52).
func (v *Verifier) VerifyMicroRoot(ctx context.Context, tok *auth.Token, recordID string) (MicroRootResult, error) {
	out := MicroRootResult{Provisional: true}

	rec, err := v.store.Get(ctx, recordID)
	if errors.Is(err, storage.ErrNotFound) {
		return out, fmt.Errorf("%w: %s", errRecordNotFound, recordID)
	}
	if err != nil {
		return out, err
	}
	if err := validateRequestedRecord(recordID, rec); err != nil {
		return out, err
	}
	treeID := dht.TreeID(rec.Epoch, rec.LogType)
	windowID := mktree.WindowID(rec.LeafIndex, v.cfg.Window)
	out.WindowID = windowID

	raw, err := v.proof.GetMicroRoot(tok, dht.MicroRootRequest{
		Epoch: rec.Epoch, LogType: rec.LogType, WindowID: windowID, Key: dht.KeyM(treeID, windowID),
	})
	if err != nil {
		return out, fmt.Errorf("%w: micro-root window %d: %v", errProofUnavailable, windowID, err)
	}
	m, err := dht.ParseMicroRootRecord(raw)
	if err != nil {
		return out, fmt.Errorf("%w: micro-root window %d", errProofMalformed, windowID)
	}
	if m.TreeID != treeID || m.WindowID != windowID || m.IMin != mktree.WindowStart(windowID, v.cfg.Window) {
		return out, fmt.Errorf("%w: micro-root names another window", errProofMalformed)
	}
	out.IMin, out.IMax = m.IMin, m.IMax
	if m.IMax < m.IMin || m.IMax-m.IMin+1 > v.cfg.Window {
		return out, fmt.Errorf("%w: micro-root range is impossible", errProofMalformed)
	}
	if rec.LeafIndex < m.IMin || rec.LeafIndex > m.IMax {
		return out, errNotCovered
	}
	out.Covered = m.IMax - m.IMin + 1

	// Recompute every covered digest from its stored (c, t, AD): the stored d
	// is never taken on trust, and no decryption occurs.
	digests := make([]hashing.Digest, 0, out.Covered)
	for i := m.IMin; i <= m.IMax; i++ {
		covered, err := v.store.GetByLeaf(ctx, rec.Epoch, rec.LogType, i)
		if errors.Is(err, storage.ErrNotFound) {
			return out, fmt.Errorf("%w: leaf %d of the window is absent", errProofMalformed, i)
		}
		if err != nil {
			return out, err
		}
		if err := validateRequestedRecord(covered.RecordID, covered); err != nil ||
			covered.LeafIndex != i || covered.Epoch != rec.Epoch || covered.LogType != rec.LogType {
			return out, errProofMalformed
		}
		digests = append(digests, encryptor.LeafDigest(covered.C, covered.T, covered.AD))
	}
	if mktree.MicroRoot(digests) != m.MicroRoot {
		return out, errPathMismatch
	}
	out.Accepted = true
	return out, nil
}
