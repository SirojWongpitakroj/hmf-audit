// Block-level ALIS (R-ALIS-018..024, global — Opt. 1 segmentation is a
// later milestone): a log-generator IIMT (lid -> BlockHeights), a
// file-keyword IIMT (file keyword -> BlockHeights), and two RIMBTs
// (FileTsMin, FileTsMax) over BlockHeight. Unlike the IOMF-level Algorithm 3
// and the transaction level, the block level has no OMT of its own — it is
// "a coarse filter that identifies blocks that may contain satisfying
// transactions" (R-ALIS-022); the actual blocks are already held locally by
// the auditor (light-node header sync), not retrieved through a proof at
// this layer.
package alis

import (
	"fmt"
	"math"
	"sort"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/iomf"
	"blas/rimbt"
)

// lidKey encodes a log generator id as the IIMT's generic string-keyed
// leaf label, using the SAME fixed 8-byte big-endian representation as any
// other hashed lid (R-GO-008): a Go string is just a byte sequence, so
// string(Uint64BE(lid)) is exactly those 8 raw bytes, not a decimal-text
// encoding. iimt's own leaf framing (FramedString) then length-prefixes it
// identically to a textual keyword.
func lidKey(lid uint64) string { return string(bhash.Uint64BE(lid)) }

func lidFromKey(k string) uint64 {
	b := []byte(k)
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// LIDKey exports lidKey for other packages (e.g. the Optimization-2
// aggregation wiring in `chain`) that need to address the SAME block-level
// LG IIMT leaf label for a given lid, without duplicating the encoding
// rule.
func LIDKey(lid uint64) string { return lidKey(lid) }

// LIDUniverse returns the distinct lids across the given block summaries,
// sorted ascending — the SAME deterministic universe-collection rule
// BuildBlockLevel uses internally for the LG IIMT's kid order (C1 in
// DECISIONS.md), exported so a second, extended tree over the identical
// universe (e.g. Optimization 2's aggregated LG IIMT) can be built with
// guaranteed-matching kid positions.
func LIDUniverse(blocks []BlockSummary) []uint64 {
	seen := map[uint64]bool{}
	var universe []uint64
	for _, b := range blocks {
		for _, l := range b.LIDs {
			if !seen[l] {
				seen[l] = true
				universe = append(universe, l)
			}
		}
	}
	sort.Slice(universe, func(i, j int) bool { return universe[i] < universe[j] })
	return universe
}

// FileKeywordUniverse returns the distinct file keywords across the given
// block summaries, sorted ascending — the same rule BuildBlockLevel uses
// for the FK IIMT's kid order.
func FileKeywordUniverse(blocks []BlockSummary) []string {
	seen := map[string]bool{}
	var universe []string
	for _, b := range blocks {
		for _, w := range b.FileKeywords {
			if !seen[w] {
				seen[w] = true
				universe = append(universe, w)
			}
		}
	}
	sort.Strings(universe)
	return universe
}

// BlockSummary is the block-level-index input for one block: its header's
// R-ALIS-023 fields (FileTsMin, FileTsMax) plus the distinct lids and file
// keywords appearing among its transactions.
type BlockSummary struct {
	BlockHeight  uint64
	LIDs         []uint64
	FileKeywords []string
	FileTsMin    int64
	FileTsMax    int64
}

// BlockLevelStore bundles the four block-level trees.
type BlockLevelStore struct {
	LG        *iimt.Tree
	FK        *iimt.Tree
	FileTsMin *rimbt.Tree
	FileTsMax *rimbt.Tree
}

// BuildBlockLevel builds the global block-level index (R-ALIS-018..021)
// over blocks 0..len(blocks)-1 (BlockHeight from 0, D6 in DECISIONS.md).
// blocks must be supplied in ascending BlockHeight order, sequential from 0.
func BuildBlockLevel(fanout int, blocks []BlockSummary) (*BlockLevelStore, error) {
	for i, b := range blocks {
		if b.BlockHeight != uint64(i) {
			return nil, fmt.Errorf("alis: blocks must be sequential from 0; block %d has BlockHeight=%d", i, b.BlockHeight)
		}
	}

	lgTree, err := iimt.NewEmpty(fanout)
	if err != nil {
		return nil, err
	}
	fkTree, err := iimt.NewEmpty(fanout)
	if err != nil {
		return nil, err
	}
	ftsMinTree, err := rimbt.NewEmpty(fanout)
	if err != nil {
		return nil, err
	}
	ftsMaxTree, err := rimbt.NewEmpty(fanout)
	if err != nil {
		return nil, err
	}

	lidUniverse := LIDUniverse(blocks)
	kwUniverse := FileKeywordUniverse(blocks)
	for _, l := range lidUniverse {
		lgTree.AddKeyword(lidKey(l))
	}
	for _, w := range kwUniverse {
		fkTree.AddKeyword(w)
	}

	for i, b := range blocks {
		height := uint64(i)
		var lidKeys []string
		for _, l := range b.LIDs {
			lidKeys = append(lidKeys, lidKey(l))
		}
		if err := lgTree.Insert(height, lidKeys); err != nil {
			return nil, err
		}
		if err := fkTree.Insert(height, b.FileKeywords); err != nil {
			return nil, err
		}
		if err := ftsMinTree.Insert(height, b.FileTsMin); err != nil {
			return nil, err
		}
		if err := ftsMaxTree.Insert(height, b.FileTsMax); err != nil {
			return nil, err
		}
	}

	return &BlockLevelStore{LG: lgTree, FK: fkTree, FileTsMin: ftsMinTree, FileTsMax: ftsMaxTree}, nil
}

// BlockLevelQuery is Q_on's block-level share (R-QUERY-009..013): LG single
// -keyword lookup, FK Boolean expression, and time-range via two one-sided
// RIMBT range queries (R-ALIS-015, R-QUERY-012) — reusing rimbt.RangeQuery
// with an unbounded side (math.MinInt64/MaxInt64) rather than a bespoke
// one-sided procedure, since a one-sided cumulative-bitmap query is exactly
// what Algorithm 5 already computes when one boundary falls outside the
// stored domain (B6 in DECISIONS.md): FileTsMax>=ts is RangeQuery(ts,
// MaxInt64) on the FileTsMax RIMBT (¬bm_ln1 ∧ bm_rightmost); FileTsMin<=te
// is RangeQuery(MinInt64, te) on the FileTsMin RIMBT (bm at the te
// boundary, with the virtual-all-zero lower bound cancelling ¬ to identity).
type BlockLevelQuery struct {
	LG       uint64
	FK       *iomf.Expr // nil => no file-keyword predicate
	HasRange bool
	TS, TE   int64
}

// BlockLevelVO is the block-level verification object (R-QUERY-013).
// LGPresent/FKAbsentKeywords are accompanied by complete-tree absence proofs.
type BlockLevelVO struct {
	LGPresent        bool
	LGProof          iimt.Proof
	LGAbsence        iimt.AbsenceProof
	FKEvidence       []iomf.KeywordEvidence
	FKAbsentKeywords []string
	FKAbsenceProofs  []iimt.AbsenceProof
	TsMaxRangeUsed   bool
	TsMaxRange       rimbt.RangeResult
	TsMinRangeUsed   bool
	TsMinRange       rimbt.RangeResult
	CandidateHeights []uint64
}

// ProcessBlockLevelQuery implements R-QUERY-009..013.
//
// An absent lg or file keyword contributes an all-zero bitmap. Since IIMT
// leaves are ordered by insertion ID, absence is proven by disclosing every
// leaf and recomputing the committed root, rather than trusting the full node.
func ProcessBlockLevelQuery(s *BlockLevelStore, n uint64, q BlockLevelQuery) (BlockLevelVO, error) {
	return ProcessBlockLevelReaders(BlockReaders{LG: s.LG, FK: s.FK, FileTsMin: s.FileTsMin, FileTsMax: s.FileTsMax}, n, q)
}
func ProcessBlockLevelReaders(s BlockReaders, n uint64, q BlockLevelQuery) (BlockLevelVO, error) {
	var vo BlockLevelVO

	_, lgProof, ok := s.LG.Query(lidKey(q.LG))
	if ok {
		vo.LGPresent = true
		vo.LGProof = lgProof
	} else {
		vo.LGAbsence, _ = s.LG.ProveAbsent(lidKey(q.LG))
	}
	lgBM := bitmap.New(n)
	if ok {
		lgBM = lgProof.Bitmap
	}

	fkBM := bitmap.Not(bitmap.New(n))
	if q.FK != nil {
		bmLookup := map[string]bitmap.Bitmap{}
		for _, w := range iomf.Keywords(q.FK) {
			_, proof, ok := s.FK.Query(w)
			if !ok {
				vo.FKAbsentKeywords = append(vo.FKAbsentKeywords, w)
				absence, _ := s.FK.ProveAbsent(w)
				vo.FKAbsenceProofs = append(vo.FKAbsenceProofs, absence)
				bmLookup[w] = bitmap.New(n)
				continue
			}
			vo.FKEvidence = append(vo.FKEvidence, iomf.KeywordEvidence{Keyword: w, Proof: proof})
			bmLookup[w] = proof.Bitmap
		}
		fkBM = iomf.Evaluate(q.FK, func(w string) bitmap.Bitmap { return bmLookup[w] })
	}

	rangeBM := bitmap.Not(bitmap.New(n))
	if q.HasRange {
		maxRes, err := s.FileTsMax.RangeQuery(q.TS, math.MaxInt64)
		if err != nil {
			return BlockLevelVO{}, err
		}
		vo.TsMaxRangeUsed = true
		vo.TsMaxRange = maxRes

		minRes, err := s.FileTsMin.RangeQuery(math.MinInt64, q.TE)
		if err != nil {
			return BlockLevelVO{}, err
		}
		vo.TsMinRangeUsed = true
		vo.TsMinRange = minRes

		maxBM := bitmap.New(n)
		for _, h := range maxRes.QR {
			maxBM.Set(h)
		}
		minBM := bitmap.New(n)
		for _, h := range minRes.QR {
			minBM.Set(h)
		}
		rangeBM = bitmap.And(maxBM, minBM)
	}

	finalBM := bitmap.And(bitmap.And(lgBM, fkBM), rangeBM)
	vo.CandidateHeights = finalBM.SetIndices()
	return vo, nil
}

// BlockLevelRoots are the committed block-level roots (part of RH, stored
// in block headers per R-ALIS-022; the concrete header field arrangement
// under Opt. 1 is a later milestone).
type BlockLevelRoots struct {
	LGRoot        bhash.Digest
	FKRoot        bhash.Digest
	FileTsMinRoot bhash.Digest
	FileTsMaxRoot bhash.Digest
}

// VerifyBlockLevelQuery implements the block-level share of R-VER-008.
// Absent-leaf claims are checked against complete-tree witnesses. IIMT
// keywords are ordered by insertion ID, so a neighboring leaf alone cannot
// prove keyword absence.
func VerifyBlockLevelQuery(rh BlockLevelRoots, n uint64, q BlockLevelQuery, vo BlockLevelVO) bool {
	var lgBM bitmap.Bitmap
	if vo.LGPresent {
		if vo.LGProof.Keyword != lidKey(q.LG) || vo.LGProof.Bitmap.Len() != n || !iimt.Verify(rh.LGRoot, vo.LGProof) {
			return false
		}
		lgBM = vo.LGProof.Bitmap
	} else {
		if !iimt.VerifyAbsent(rh.LGRoot, n, lidKey(q.LG), vo.LGAbsence) {
			return false
		}
		lgBM = bitmap.New(n)
	}
	for _, ke := range vo.FKEvidence {
		if ke.Keyword != ke.Proof.Keyword || ke.Proof.Bitmap.Len() != n || !iimt.Verify(rh.FKRoot, ke.Proof) {
			return false
		}
	}
	if len(vo.FKAbsentKeywords) != len(vo.FKAbsenceProofs) {
		return false
	}
	for i, w := range vo.FKAbsentKeywords {
		if !iimt.VerifyAbsent(rh.FKRoot, n, w, vo.FKAbsenceProofs[i]) {
			return false
		}
	}
	if q.HasRange {
		if !rimbt.VerifyRangeResult(rh.FileTsMaxRoot, n, q.TS, math.MaxInt64, vo.TsMaxRange) {
			return false
		}
		if !rimbt.VerifyRangeResult(rh.FileTsMinRoot, n, math.MinInt64, q.TE, vo.TsMinRange) {
			return false
		}
	}

	fkBM := bitmap.Not(bitmap.New(n))
	if q.FK != nil {
		bmLookup := map[string]bitmap.Bitmap{}
		for _, ke := range vo.FKEvidence {
			bmLookup[ke.Keyword] = ke.Proof.Bitmap
		}
		for _, w := range vo.FKAbsentKeywords {
			bmLookup[w] = bitmap.New(n)
		}
		for _, w := range iomf.Keywords(q.FK) {
			if _, ok := bmLookup[w]; !ok {
				return false
			}
		}
		fkBM = iomf.Evaluate(q.FK, func(w string) bitmap.Bitmap { return bmLookup[w] })
	}
	rangeBM := bitmap.Not(bitmap.New(n))
	if q.HasRange {
		maxBM, ok := recomputeRangeBitmap(n, vo.TsMaxRange)
		if !ok {
			return false
		}
		minBM, ok := recomputeRangeBitmap(n, vo.TsMinRange)
		if !ok {
			return false
		}
		rangeBM = bitmap.And(maxBM, minBM)
	}
	expected := bitmap.And(bitmap.And(lgBM, fkBM), rangeBM).SetIndices()
	if len(expected) != len(vo.CandidateHeights) {
		return false
	}
	for i := range expected {
		if expected[i] != vo.CandidateHeights[i] {
			return false
		}
	}
	return true
}

// recomputeRangeBitmap reconstructs a RIMBT range sub-query's result bitmap
// directly from the VERIFIED cumulative bitmaps carried in a
// rimbt.RangeResult (IV-C semantics: bm(v)[i] = 1 iff value(i) <= v, so a
// range result is ¬lower ∧ upper) — never from res.QR itself, which is
// server-supplied and, on its own, cryptographically unauthenticated. This
// mirrors the pattern already used by iomf.Verify at the generic IOMF
// level; block/transaction-level range verification (VerifyBlockLevelQuery,
// verifyTxLevelCommon) must use it instead of trusting res.QR, or a
// malicious full node can claim an arbitrary candidate set for the
// time-range sub-query while shipping real but unrelated boundary proofs
// (security regression: see alis.TestVerifyBlockLevelRejectsForgedRangeQR /
// alis.TestVerifyTxLevelRejectsForgedRangeQR).
//
// ok is false if a present boundary bitmap's length doesn't match the
// verifier's own authenticated n (R-GO-016/T-080 discipline: reject, never
// panic — bitmap.And/Not panic on a length mismatch).
func recomputeRangeBitmap(n uint64, res rimbt.RangeResult) (bitmap.Bitmap, bool) {
	lower := bitmap.New(n)
	if res.LowerReal {
		if res.LowerProof.Bitmap.Len() != n {
			return bitmap.Bitmap{}, false
		}
		lower = res.LowerProof.Bitmap
	}
	upper := bitmap.New(n)
	if res.UpperReal {
		if res.UpperProof.Bitmap.Len() != n {
			return bitmap.Bitmap{}, false
		}
		upper = res.UpperProof.Bitmap
	}
	return bitmap.And(bitmap.Not(lower), upper), true
}
