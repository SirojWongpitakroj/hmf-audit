package verifier

import (
	"context"
	"errors"
	"fmt"
	"sort"

	contract "canonicalingestion"
	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
)

type preparedGroup struct {
	logType  string
	ancestor uint64
}

// PreparedBatch contains an audit request whose records, proof envelopes and
// trusted anchor have already been acquired. Its fields are intentionally
// private: callers can time VerifyPreparedBatch without bypassing validation.
//
// Preparation includes storage, DHT authorization/lookups and the blockchain
// root read. The persistence-inclusive shared benchmark times this step.
type PreparedBatch struct {
	levels       uint64
	stride       uint64
	epoch        uint64
	anchored     hashing.Digest
	work         []leafWork
	parents      map[hashing.Digest][]byte
	certificates map[preparedGroup][]byte
	roots        map[string][]byte
	logTypes     []string
}

// PrepareBatch acquires all external evidence needed by VerifyPreparedBatch.
// It deliberately leaves parsing, proof validation, hashing and anchor
// reconciliation to VerifyPreparedBatch.
func (v *Verifier) PrepareBatch(ctx context.Context, tok *auth.Token, recordIDs []string) (*PreparedBatch, error) {
	if err := v.checkBounds(len(recordIDs)); err != nil {
		return nil, err
	}
	if len(recordIDs) == 0 {
		return nil, errors.New("verifier: empty batch")
	}
	levels, stride := v.geometry()
	prepared := &PreparedBatch{
		levels: levels, stride: stride,
		work:         make([]leafWork, 0, len(recordIDs)),
		parents:      make(map[hashing.Digest][]byte),
		certificates: make(map[preparedGroup][]byte),
		roots:        make(map[string][]byte),
		logTypes:     append([]string(nil), v.cfg.LogTypes...),
	}

	records, err := v.loadRecords(ctx, recordIDs)
	if err != nil {
		return nil, err
	}
	for i, rec := range records {
		if i == 0 {
			prepared.epoch = rec.Epoch
		} else if rec.Epoch != prepared.epoch {
			return nil, ErrMixedEpochs
		}
		prepared.work = append(prepared.work, leafWork{
			record:   rec,
			treeID:   dht.TreeID(rec.Epoch, rec.LogType),
			ancestor: mktree.CoveringAncestorIndex(rec.LeafIndex, levels, stride),
		})
	}

	prepared.anchored, err = v.requireAnchored(ctx, prepared.epoch)
	if err != nil {
		return nil, err
	}
	sess := newSession()
	defer sess.end()
	if err = v.prefetch(tok, sess, prepared.epoch, prepared.work, levels, stride); err != nil {
		return nil, err
	}

	groups := make(map[preparedGroup]struct{})
	for _, work := range prepared.work {
		idx := work.record.LeafIndex
		for level := levels; level > stride; level-- {
			key := dht.KeyP(work.treeID, level, idx^1)
			if _, found := prepared.parents[key]; !found {
				raw, err := v.preparedParent(tok, sess, dht.ParentRequest{
					Epoch: prepared.epoch, LogType: work.record.LogType,
					J: level, K: idx ^ 1, Key: key,
				})
				if err != nil {
					return nil, fmt.Errorf("%w: P_%d,%d: %v", errProofUnavailable, level, idx^1, err)
				}
				prepared.parents[key] = raw
			}
			idx >>= 1
		}
		groups[preparedGroup{logType: work.record.LogType, ancestor: work.ancestor}] = struct{}{}
	}

	for group := range groups {
		treeID := dht.TreeID(prepared.epoch, group.logType)
		raw, err := v.preparedCert(tok, sess, dht.CertRequest{
			Epoch: prepared.epoch, LogType: group.logType, S: stride,
			K: group.ancestor, Key: dht.KeyC(treeID, stride, group.ancestor),
		})
		if err != nil {
			return nil, fmt.Errorf("%w: C_%d,%d: %v", errProofUnavailable, stride, group.ancestor, err)
		}
		prepared.certificates[group] = raw
	}

	sort.Strings(prepared.logTypes)
	for _, logType := range prepared.logTypes {
		treeID := dht.TreeID(prepared.epoch, logType)
		raw, err := v.preparedRoot(tok, sess, dht.RootRequest{
			Epoch: prepared.epoch, LogType: logType, Key: dht.KeyR(treeID),
		})
		if err != nil {
			return nil, fmt.Errorf("%w: root record for %s: %v", errProofUnavailable, logType, err)
		}
		prepared.roots[logType] = raw
	}
	return prepared, nil
}

// VerifyPreparedBatch measures only the system's verification novelty:
// envelope validation, digest/path recomputation, certificate reuse and
// reconciliation with an already-trusted anchor.
func (v *Verifier) VerifyPreparedBatch(prepared *PreparedBatch) (Result, error) {
	if prepared == nil || len(prepared.work) == 0 {
		return Result{}, errors.New("verifier: empty prepared batch")
	}
	result := Result{}
	ancestorOf := make(map[preparedGroup]hashing.Digest)
	groupOrder := make([]preparedGroup, 0, len(prepared.work))
	for _, work := range prepared.work {
		leaf := encryptor.LeafDigest(work.record.C, work.record.T, work.record.AD)
		ancestor, err := verifyPreparedLeaf(prepared, work, leaf, &result.HashCount)
		if err != nil {
			return Result{}, err
		}
		group := preparedGroup{logType: work.record.LogType, ancestor: work.ancestor}
		if previous, found := ancestorOf[group]; found {
			if previous != ancestor {
				return Result{}, errPathMismatch
			}
			continue
		}
		ancestorOf[group] = ancestor
		groupOrder = append(groupOrder, group)
	}
	result.Ancestors = len(groupOrder)

	perTypeRoot := make(map[string]hashing.Digest)
	for _, group := range groupOrder {
		root, err := verifyPreparedCertificate(prepared, group, ancestorOf[group], &result.HashCount)
		if err != nil {
			return Result{}, err
		}
		result.AncestorPathRecomputations++
		if previous, found := perTypeRoot[group.logType]; found && previous != root {
			return Result{}, errPathMismatch
		}
		perTypeRoot[group.logType] = root
	}
	if err := verifyPreparedAnchor(prepared, perTypeRoot); err != nil {
		return Result{}, err
	}
	result.Accepted = true
	return result, nil
}

// VerifyPreparedBatchConcurrent schedules only independent leaf paths. The
// native ordered ancestor fold, certificate reuse and anchor checks are unchanged.
// Prepared evidence is private and immutable until all workers have joined.
func (v *Verifier) VerifyPreparedBatchConcurrent(ctx context.Context, prepared *PreparedBatch, workers int) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if workers == 1 {
		return v.VerifyPreparedBatch(prepared)
	}
	if prepared == nil || len(prepared.work) == 0 {
		return Result{}, errors.New("verifier: empty prepared batch")
	}
	result := Result{}
	ancestorOf := make(map[preparedGroup]hashing.Digest)
	groupOrder := make([]preparedGroup, 0, len(prepared.work))
	type leafResult struct {
		ancestor hashing.Digest
		hashes   uint64
	}
	leaves := make([]leafResult, len(prepared.work))
	if err := contract.AuditParallel(ctx, len(prepared.work), workers, func(i int) error {
		work := prepared.work[i]
		leaf := encryptor.LeafDigest(work.record.C, work.record.T, work.record.AD)
		var err error
		leaves[i].ancestor, err = verifyPreparedLeaf(prepared, work, leaf, &leaves[i].hashes)
		return err
	}); err != nil {
		return Result{}, err
	}
	for i, work := range prepared.work {
		ancestor := leaves[i].ancestor
		result.HashCount += leaves[i].hashes
		group := preparedGroup{logType: work.record.LogType, ancestor: work.ancestor}
		if previous, found := ancestorOf[group]; found {
			if previous != ancestor {
				return Result{}, errPathMismatch
			}
			continue
		}
		ancestorOf[group] = ancestor
		groupOrder = append(groupOrder, group)
	}
	result.Ancestors = len(groupOrder)

	perTypeRoot := make(map[string]hashing.Digest)
	for _, group := range groupOrder {
		root, err := verifyPreparedCertificate(prepared, group, ancestorOf[group], &result.HashCount)
		if err != nil {
			return Result{}, err
		}
		result.AncestorPathRecomputations++
		if previous, found := perTypeRoot[group.logType]; found && previous != root {
			return Result{}, errPathMismatch
		}
		perTypeRoot[group.logType] = root
	}
	if err := verifyPreparedAnchor(prepared, perTypeRoot); err != nil {
		return Result{}, err
	}
	result.Accepted = true
	return result, nil
}

func verifyPreparedLeaf(prepared *PreparedBatch, work leafWork, leaf hashing.Digest, count *uint64) (hashing.Digest, error) {
	current := leaf
	index := work.record.LeafIndex
	for level := prepared.levels; level > prepared.stride; level-- {
		key := dht.KeyP(work.treeID, level, index^1)
		raw, found := prepared.parents[key]
		if !found {
			return hashing.Digest{}, fmt.Errorf("%w: P_%d,%d", errProofUnavailable, level, index^1)
		}
		parent, err := dht.ParseParentNode(raw)
		if err != nil {
			return hashing.Digest{}, fmt.Errorf("%w: P_%d,%d", errProofMalformed, level, index^1)
		}
		if parent.TreeID != work.treeID || parent.J != level || parent.K != index^1 {
			return hashing.Digest{}, fmt.Errorf("%w: P_%d,%d names another node", errProofMalformed, level, index^1)
		}
		if index%2 == 0 {
			current = mktree.Internal(current, parent.Node)
		} else {
			current = mktree.Internal(parent.Node, current)
		}
		*count++
		index >>= 1
	}
	return current, nil
}

func verifyPreparedCertificate(prepared *PreparedBatch, group preparedGroup,
	ancestor hashing.Digest, count *uint64) (hashing.Digest, error) {
	raw, found := prepared.certificates[group]
	if !found {
		return hashing.Digest{}, fmt.Errorf("%w: C_%d,%d", errProofUnavailable, prepared.stride, group.ancestor)
	}
	certificate, err := dht.ParseAncestorCert(raw)
	if err != nil {
		return hashing.Digest{}, fmt.Errorf("%w: C_%d,%d", errProofMalformed, prepared.stride, group.ancestor)
	}
	treeID := dht.TreeID(prepared.epoch, group.logType)
	if certificate.TreeID != treeID || certificate.Level != prepared.stride ||
		certificate.Index != group.ancestor ||
		certificate.AncestorID != dht.AncestorID(treeID, prepared.stride, group.ancestor) ||
		uint64(len(certificate.UpProof)) != prepared.stride {
		return hashing.Digest{}, fmt.Errorf("%w: C_%d,%d names another checkpoint",
			errProofMalformed, prepared.stride, group.ancestor)
	}
	if certificate.AncestorHash != ancestor {
		return hashing.Digest{}, errPathMismatch
	}
	return mktree.FoldUp(ancestor, group.ancestor, certificate.UpProof, count), nil
}

func verifyPreparedAnchor(prepared *PreparedBatch, perTypeRoot map[string]hashing.Digest) error {
	roots := make([]hashing.Digest, 0, len(prepared.logTypes))
	for _, logType := range prepared.logTypes {
		raw, found := prepared.roots[logType]
		if !found {
			return fmt.Errorf("%w: root record for %s", errProofUnavailable, logType)
		}
		record, err := dht.ParseRootRecord(raw)
		if err != nil {
			return fmt.Errorf("%w: root record for %s", errProofMalformed, logType)
		}
		treeID := dht.TreeID(prepared.epoch, logType)
		if record.TreeID != treeID || record.LogType != logType {
			return fmt.Errorf("%w: root record for %s names another tree", errProofMalformed, logType)
		}
		if recomputed, found := perTypeRoot[logType]; found && recomputed != record.Root {
			return errPathMismatch
		}
		roots = append(roots, record.Root)
	}
	if mktree.RootOfRoots(roots) != prepared.anchored {
		return errAnchorMismatch
	}
	return nil
}
