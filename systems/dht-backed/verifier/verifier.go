// Package verifier is the auditor-side Verifier of Phase 4 (R-SYS-06). It has
// exactly two modes: Inclusion Check and Batch Check.
//
// It never decrypts (R-VER-09), never trusts a DHT-returned value in place of
// its own recomputation (Q18, R-DHT-07), and terminates every check at the
// on-chain anchor (R-VER-05).
package verifier

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"dhtbacked/auth"
	"dhtbacked/blockchain"
	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/storage"
)

// The verification result is binary accept/reject (R-VER-06). Per the GAP-4
// decision (user, 2026-09-11) the public surface exposes only that: every
// rejection wraps the single exported ErrRejected, and Result.Accepted is false.
// No error taxonomy is part of the API.
//
// Two exported errors are not verification results at all, and so stay
// distinct: ErrEpochNotAnchored is the Q52 refusal — the verifier declines to
// verify an epoch that is not finalized and anchored, and Q52 makes testing
// that refusal mandatory — and ErrMixedEpochs rejects an invalid request.
var (
	ErrRejected         = errors.New("verifier: rejected")
	ErrEpochNotAnchored = errors.New("verifier: refused: the epoch is not finalized and anchored")
	ErrMixedEpochs      = errors.New("verifier: a batch spans more than one epoch")
)

// The named rejection reasons are internal. Each wraps ErrRejected, so a caller
// sees only "rejected", while the verifier's own tests can still confirm which
// recomputation caught a tamper (see export_test.go).
var (
	errRecordNotFound   = fmt.Errorf("%w: no such record", ErrRejected)
	errProofUnavailable = fmt.Errorf("%w: proof material is unavailable", ErrRejected)
	errProofMalformed   = fmt.Errorf("%w: proof material is malformed", ErrRejected)
	errPathMismatch     = fmt.Errorf("%w: recomputed path does not match", ErrRejected)
	errAnchorMismatch   = fmt.Errorf("%w: recomputed root-of-roots does not match the anchor", ErrRejected)
)

// Verifier holds the components an auditor talks to. All calls are synchronous
// and single-goroutine (Q32).
type Verifier struct {
	store  storage.Store
	proof  ProofSource
	chain  blockchain.RootSource
	cfg    mktree.Config
	bounds *RequestBounds
}

// ProofSource is the authorized proof-record service contacted by an auditor.
// The native in-process DHT and persistence-backed benchmark adapters both
// implement this contract.
type ProofSource interface {
	GetParent(tok *auth.Token, req dht.ParentRequest) ([]byte, error)
	GetCert(tok *auth.Token, req dht.CertRequest) ([]byte, error)
	GetRoot(tok *auth.Token, req dht.RootRequest) ([]byte, error)
	GetMicroRoot(tok *auth.Token, req dht.MicroRootRequest) ([]byte, error)
}

// ProofKind names which of the Q41 typed envelopes a ProofRequest carries.
type ProofKind uint8

// The three proof-record kinds a batch verification reads. Micro-root records
// are not read by Batch Check, so they have no kind here.
const (
	ProofParent ProofKind = iota
	ProofCert
	ProofRoot
)

// ProofRequest is one Q41 envelope in a batched proof fetch. A and B carry
// (J, K) for a parent node and (S, K) for a certificate; a root record uses
// neither.
type ProofRequest struct {
	Kind    ProofKind
	Epoch   uint64
	LogType string
	A, B    uint64
	Key     hashing.Digest
}

// BatchProofSource is an optional extension of ProofSource. A source that
// implements it is handed every proof record a batch will need in one call and
// may retrieve them concurrently and out of order; the verifier then folds the
// returned records itself, in its own deterministic order, and still
// recomputes every hash (Q18, R-DHT-07). The verifier itself starts no
// goroutine: any parallelism lives inside the source.
//
// The returned map is keyed by ProofRequest.Key. A request the source cannot
// serve is an error, exactly as a failed single lookup is.
type BatchProofSource interface {
	ProofSource
	GetMany(tok *auth.Token, requests []ProofRequest) (map[hashing.Digest][]byte, error)
}

// New constructs the auditor-side verifier.
func New(store storage.Store, proofs ProofSource, chain blockchain.RootSource, cfg mktree.Config) *Verifier {
	return &Verifier{store: store, proof: proofs, chain: chain, cfg: cfg}
}

// Result is the outcome of one verification request.
type Result struct {
	// Accepted is the binary verification outcome (R-VER-06).
	Accepted bool
	// HashCount is the number of internal-node recomputations performed while
	// walking leaf -> ancestor -> root. Theorem G.1 bounds this quantity by
	// n(L - s) + gs (R-VER-08).
	HashCount uint64
	// Ancestors is g: the number of distinct level-s ancestors the batch spans.
	Ancestors int
	// AncestorPathRecomputations counts how many times the ancestor-to-root
	// path was walked. Under certificate reuse it equals Ancestors, not the
	// number of leaves (R-VER-07 acceptance).
	AncestorPathRecomputations int
}

// session carries the request-local state of one audit request.
//
// Q32 authorizes exactly one cache: within a single audit request a repeated
// P_j,k record may be reused from this map. The map lives and dies with the
// request; there is no persistent cross-query cache, no parallel lookup and no
// automatic retry.
type session struct {
	parents map[hashing.Digest][]byte
	// fetched holds the records a BatchProofSource returned for this request.
	// Like parents it lives and dies with the request.
	fetched map[hashing.Digest][]byte
}

func newSession() *session {
	return &session{parents: make(map[hashing.Digest][]byte)}
}

// end clears the request-local cache when the request ends (Q32).
func (s *session) end() { s.parents, s.fetched = nil, nil }

// geometry returns L and s for the configured epoch trees.
func (v *Verifier) geometry() (levels, stride uint64) {
	width := mktree.PaddedWidth(v.cfg.EpochCapacity)
	for w := width; w > 1; w >>= 1 {
		levels++
	}
	return levels, v.cfg.Stride
}

// requireAnchored enforces the Q52 precondition: full MK-Tree inclusion and
// batch verification require a finalized, anchored epoch. Verification against
// an open epoch is refused.
func (v *Verifier) requireAnchored(ctx context.Context, epoch uint64) (hashing.Digest, error) {
	anchored, err := v.chain.GetRoot(ctx, epoch)
	if errors.Is(err, blockchain.ErrNotAnchored) {
		return hashing.Digest{}, fmt.Errorf("%w: epoch %d", ErrEpochNotAnchored, epoch)
	}
	if err != nil {
		return hashing.Digest{}, err
	}
	return anchored, nil
}

// VerifyInclusion performs the four Phase 4 steps for a single record.
func (v *Verifier) VerifyInclusion(ctx context.Context, tok *auth.Token, recordID string) (Result, error) {
	return v.VerifyBatch(ctx, tok, []string{recordID})
}

// leafWork is one record's resolved position within its tree.
type leafWork struct {
	record   storage.Record
	treeID   hashing.Digest
	ancestor uint64
}

// VerifyBatch performs Phase 4 for a batch of records (Eq. (27)).
//
// Proofs sharing a level-s ancestor reuse the single certificate C_s,k, so the
// ancestor-to-root path is recomputed once per distinct ancestor rather than
// once per leaf (R-VER-07).
func (v *Verifier) VerifyBatch(ctx context.Context, tok *auth.Token, recordIDs []string) (Result, error) {
	if err := v.checkBounds(len(recordIDs)); err != nil {
		return Result{}, err
	}
	sess := newSession()
	defer sess.end()

	if len(recordIDs) == 0 {
		return Result{}, errors.New("verifier: empty batch")
	}
	levels, stride := v.geometry()

	// Resolve every record by record_id and read (epoch, log_type, leaf_index)
	// (Q44).
	records, err := v.loadRecords(ctx, recordIDs)
	if err != nil {
		return Result{}, err
	}
	work := make([]leafWork, 0, len(recordIDs))
	var epoch uint64
	for i, rec := range records {
		if i == 0 {
			epoch = rec.Epoch
		} else if rec.Epoch != epoch {
			return Result{}, ErrMixedEpochs
		}
		work = append(work, leafWork{
			record:   rec,
			treeID:   dht.TreeID(rec.Epoch, rec.LogType),
			ancestor: mktree.CoveringAncestorIndex(rec.LeafIndex, levels, stride),
		})
	}

	anchored, err := v.requireAnchored(ctx, epoch)
	if err != nil {
		return Result{}, err
	}
	if err := v.prefetch(tok, sess, epoch, work, levels, stride); err != nil {
		return Result{}, err
	}

	result := Result{}

	// Step 1 and step 2, per leaf: recompute d, then walk leaf -> ancestor
	// using sibling hashes retrieved from the DHT.
	type groupKey struct {
		logType  string
		ancestor uint64
	}
	ancestorOf := make(map[groupKey]hashing.Digest)
	groupOrder := make([]groupKey, 0, len(work))
	for _, w := range work {
		d := encryptor.LeafDigest(w.record.C, w.record.T, w.record.AD)
		a, err := v.foldLeafToAncestor(ctx, sess, tok, w, d, levels, stride, &result.HashCount)
		if err != nil {
			return Result{}, err
		}
		k := groupKey{w.record.LogType, w.ancestor}
		if prev, seen := ancestorOf[k]; seen {
			if prev != a {
				return Result{}, errPathMismatch
			}
			continue
		}
		ancestorOf[k] = a
		groupOrder = append(groupOrder, k)
	}
	result.Ancestors = len(groupOrder)

	// Step 3, once per distinct ancestor: retrieve C_s,k and validate
	// A_s,k => R^(e)_tau using upProof_s,k. This is the certificate reuse of
	// Eq. (27).
	perTypeRoot := make(map[string]hashing.Digest)
	for _, k := range groupOrder {
		root, err := v.foldAncestorToRoot(ctx, sess, tok, epoch, k.logType, k.ancestor, ancestorOf[k], stride, &result.HashCount)
		if err != nil {
			return Result{}, err
		}
		result.AncestorPathRecomputations++
		if prev, seen := perTypeRoot[k.logType]; seen && prev != root {
			return Result{}, errPathMismatch
		}
		perTypeRoot[k.logType] = root
	}

	// Step 4: anchor reconciliation (Q26, R-VER-05).
	if err := v.reconcileAnchor(ctx, sess, tok, epoch, perTypeRoot, anchored); err != nil {
		return Result{}, err
	}
	result.Accepted = true
	return result, nil
}

// foldLeafToAncestor performs step 2. The DHT's P_j,k records are storage from
// which the sibling at each level is retrieved (position k xor 1); the verifier
// recomputes every parent itself and never adopts an on-path parent value
// returned by the DHT (Q18, R-DHT-07).
func (v *Verifier) foldLeafToAncestor(ctx context.Context, sess *session, tok *auth.Token,
	w leafWork, leaf hashing.Digest, levels, stride uint64, count *uint64) (hashing.Digest, error) {

	cur := leaf
	idx := w.record.LeafIndex
	for j := levels; j > stride; j-- {
		sibling, err := v.parentNode(ctx, sess, tok, w.record.Epoch, w.record.LogType, w.treeID, j, idx^1)
		if err != nil {
			return hashing.Digest{}, err
		}
		if idx%2 == 0 {
			cur = mktree.Internal(cur, sibling)
		} else {
			cur = mktree.Internal(sibling, cur)
		}
		*count++
		idx >>= 1
	}
	return cur, nil
}

// parentNode retrieves one P_j,k record through the Q41 typed envelope. Within
// one request a repeated record is reused from the request-local map (Q32).
func (v *Verifier) parentNode(ctx context.Context, sess *session, tok *auth.Token,
	epoch uint64, logType string, treeID hashing.Digest, j, k uint64) (hashing.Digest, error) {

	key := dht.KeyP(treeID, j, k)
	raw, cached := sess.parents[key]
	if !cached {
		if raw, cached = sess.fetched[key]; cached {
			sess.parents[key] = raw
		}
	}
	if !cached {
		var err error
		raw, err = v.proof.GetParent(tok, dht.ParentRequest{
			Epoch: epoch, LogType: logType, J: j, K: k, Key: key,
		})
		if err != nil {
			return hashing.Digest{}, fmt.Errorf("%w: P_%d,%d: %v", errProofUnavailable, j, k, err)
		}
		sess.parents[key] = raw
	}
	p, err := dht.ParseParentNode(raw)
	if err != nil {
		return hashing.Digest{}, fmt.Errorf("%w: P_%d,%d", errProofMalformed, j, k)
	}
	if p.TreeID != treeID || p.J != j || p.K != k {
		return hashing.Digest{}, fmt.Errorf("%w: P_%d,%d names another node", errProofMalformed, j, k)
	}
	return p.Node, nil
}

// foldAncestorToRoot performs step 3.
func (v *Verifier) foldAncestorToRoot(ctx context.Context, sess *session, tok *auth.Token, epoch uint64,
	logType string, k uint64, ancestor hashing.Digest, stride uint64, count *uint64) (hashing.Digest, error) {

	treeID := dht.TreeID(epoch, logType)
	key := dht.KeyC(treeID, stride, k)
	raw, ok := sess.fetched[key]
	if !ok {
		var err error
		raw, err = v.proof.GetCert(tok, dht.CertRequest{
			Epoch: epoch, LogType: logType, S: stride, K: k, Key: key,
		})
		if err != nil {
			return hashing.Digest{}, fmt.Errorf("%w: C_%d,%d: %v", errProofUnavailable, stride, k, err)
		}
	}
	c, err := dht.ParseAncestorCert(raw)
	if err != nil {
		return hashing.Digest{}, fmt.Errorf("%w: C_%d,%d", errProofMalformed, stride, k)
	}
	if c.TreeID != treeID || c.Level != stride || c.Index != k ||
		c.AncestorID != dht.AncestorID(treeID, stride, k) || uint64(len(c.UpProof)) != stride {
		return hashing.Digest{}, fmt.Errorf("%w: C_%d,%d names another checkpoint", errProofMalformed, stride, k)
	}
	// The certificate's ancestor hash must equal the value the verifier
	// recomputed from the leaf; the certificate is never taken on trust.
	if c.AncestorHash != ancestor {
		return hashing.Digest{}, errPathMismatch
	}
	return mktree.FoldUp(ancestor, k, c.UpProof, count), nil
}

// reconcileAnchor performs step 4: retrieve the Root Records for all log types
// in the epoch, order them lexicographically, recompute R^(e)_agg and compare
// it with the on-chain value (Q26, R-VER-05).
//
// A forged DHT Root Record is detected here, because the recomputed aggregate
// will not match the externally anchored root.
func (v *Verifier) reconcileAnchor(ctx context.Context, sess *session, tok *auth.Token, epoch uint64,
	perTypeRoot map[string]hashing.Digest, anchored hashing.Digest) error {

	types := append([]string(nil), v.cfg.LogTypes...)
	sort.Strings(types)
	roots := make([]hashing.Digest, 0, len(types))
	for _, lt := range types {
		treeID := dht.TreeID(epoch, lt)
		raw, ok := sess.fetched[dht.KeyR(treeID)]
		if !ok {
			var err error
			raw, err = v.proof.GetRoot(tok, dht.RootRequest{Epoch: epoch, LogType: lt, Key: dht.KeyR(treeID)})
			if err != nil {
				return fmt.Errorf("%w: root record for %s: %v", errProofUnavailable, lt, err)
			}
		}
		rr, err := dht.ParseRootRecord(raw)
		if err != nil {
			return fmt.Errorf("%w: root record for %s", errProofMalformed, lt)
		}
		if rr.TreeID != treeID || rr.LogType != lt {
			return fmt.Errorf("%w: root record for %s names another tree", errProofMalformed, lt)
		}
		// Where the batch recomputed a per-type root, the published record
		// must agree with it.
		if recomputed, ok := perTypeRoot[lt]; ok && recomputed != rr.Root {
			return errPathMismatch
		}
		roots = append(roots, rr.Root)
	}
	if mktree.RootOfRoots(roots) != anchored {
		return errAnchorMismatch
	}
	return nil
}
