package query

import (
	"fmt"

	"blas/alis"
	"blas/chain"
	"blas/optimize"
)

// FullNode answers Q_on (R-FUNC-012): it is untrusted by the auditor (G5 in
// DECISIONS.md — a full node's query response is untrusted; only
// blockchain-committed state and root hashes are trusted), so every VO it
// produces here is re-derived and checked by the `verify` package, never
// taken on faith.
type FullNode struct {
	Chain *chain.Chain
	view  ChainView // when set, segments, blocks and header times come from it, not from Chain
}

// NewFullNode wraps a chain for answering queries.
func NewFullNode(c *chain.Chain) *FullNode { return &FullNode{Chain: c} }

// SegmentBlockResult is one segment's contribution to Stage 1.
type SegmentBlockResult struct {
	SegmentBase  uint64
	AnchorHeight uint64 // the block whose header carries this segment's committed block-level roots (E2)
	VO           alis.BlockLevelVO
}

// Stage1Result is the full node's block-level answer (R-QUERY-013): the
// per-segment evidence plus the combined, ascending, global candidate
// BlockHeight set. This baseline queries every segment (the "unpartitioned
// run" of T-049); the optimized backward scan that visits only intersecting
// segments is layered on top in the optimize package (M10) and must produce
// the same CandidateHeights.
type Stage1Result struct {
	Segments         []SegmentBlockResult
	CandidateHeights []uint64
}

// Stage1 implements R-QUERY-009..013, querying every segment. This is the
// "unpartitioned run" baseline (T-049): correct regardless of Optimization
// 1, and used as the cross-check ground truth for Stage1Segments.
func (fn *FullNode) Stage1(qon QOn) (Stage1Result, error) {
	if fn.view != nil {
		return Stage1Result{}, fmt.Errorf("query: Stage1 over every segment needs the in-memory chain; use Stage1Optimized")
	}
	all := fn.Chain.Segments()
	indices := make([]uint64, len(all))
	for i := range all {
		indices[i] = uint64(i)
	}
	return fn.stage1Over(qon, indices)
}

// Stage1Segments implements R-OPT-002/R-OPT-009: answers Stage 1 using
// only the given segment indices (the auditor's backward-scan result, M10)
// rather than every segment. With segmentIndices covering every segment,
// this produces byte-for-byte the same Stage1Result as Stage1.
func (fn *FullNode) Stage1Segments(qon QOn, segmentIndices []uint64) (Stage1Result, error) {
	return fn.stage1Over(qon, segmentIndices)
}

func (fn *FullNode) stage1Over(qon QOn, segmentIndices []uint64) (Stage1Result, error) {
	var res Stage1Result
	selected, err := fn.segments(segmentIndices)
	if err != nil {
		return Stage1Result{}, err
	}
	for _, seg := range selected {
		n := uint64(len(seg.Summaries))
		vo, err := alis.ProcessBlockLevelQuery(seg.Store, n, alis.BlockLevelQuery{
			LG: qon.LG, FK: qon.FK, HasRange: qon.HasRange, TS: qon.TS, TE: qon.TE,
		})
		if err != nil {
			return Stage1Result{}, err
		}
		anchor := seg.Base + uint64(len(seg.Summaries)) - 1
		res.Segments = append(res.Segments, SegmentBlockResult{SegmentBase: seg.Base, AnchorHeight: anchor, VO: vo})
		for _, h := range vo.CandidateHeights {
			res.CandidateHeights = append(res.CandidateHeights, seg.Base+h)
		}
	}
	return res, nil
}

// BlockTxResult is one candidate block's transaction-level answer.
type BlockTxResult struct {
	Height uint64
	VO     alis.TxLevelVO
}

// Stage2Result is the full node's transaction-level answer (R-QUERY-016,
// R-QUERY-018): per candidate block, the matching transactions plus VO.
type Stage2Result struct {
	Blocks []BlockTxResult
}

// Stage2 implements R-QUERY-014..018.
func (fn *FullNode) Stage2(qon QOn, candidateHeights []uint64) (Stage2Result, error) {
	var res Stage2Result
	selected, err := fn.blocks(candidateHeights)
	if err != nil {
		return Stage2Result{}, err
	}
	for i, h := range candidateHeights {
		b := selected[i]
		vo, err := alis.ProcessTxLevelQuery(b.TxIndex, b.Transactions, alis.TxQuery{
			LG: qon.LG, FK: qon.FK, HasRange: qon.HasRange, TS: qon.TS, TE: qon.TE,
		})
		if err != nil {
			return Stage2Result{}, err
		}
		res.Blocks = append(res.Blocks, BlockTxResult{Height: h, VO: vo})
	}
	return res, nil
}

// AnswerQOn runs Stage 1 then Stage 2 (Fig. 4 step (4): "full node sends
// the auditor the resulting transactions together with the block-level and
// transaction-level VOs", R-QUERY-018).
func (fn *FullNode) AnswerQOn(qon QOn) (Stage1Result, Stage2Result, error) {
	s1, err := fn.Stage1(qon)
	if err != nil {
		return Stage1Result{}, Stage2Result{}, err
	}
	s2, err := fn.Stage2(qon, s1.CandidateHeights)
	if err != nil {
		return Stage1Result{}, Stage2Result{}, err
	}
	return s1, s2, nil
}

// Headers returns the header of every committed block, in ascending
// BlockHeight order — used by the backward-scan optimization (M10) to
// decide which segments to search.
func (fn *FullNode) Headers() []chain.BlockHeader {
	if fn.view != nil {
		return nil // a storage-backed node does not hold the headers; its view runs the backward scan
	}
	blocks := fn.Chain.Blocks()
	out := make([]chain.BlockHeader, len(blocks))
	for i, b := range blocks {
		out[i] = b.Header
	}
	return out
}

// SegmentSize returns S.
func (fn *FullNode) SegmentSize() uint64 {
	if fn.view != nil {
		return fn.view.SegmentSize()
	}
	return fn.Chain.SegmentSize()
}

// AnswerQOnOptimized implements Optimization 1's query-side restriction
// (R-OPT-002/R-OPT-009): the full node performs its own backward scan
// (optimize.IntersectingSegments) to decide which segments to search,
// instead of every segment. With Optimization 1 "disabled" (segment size
// effectively unbounded, one giant segment), this scans that single
// segment — no separate code path.
func (fn *FullNode) AnswerQOnOptimized(qon QOn) (Stage1Result, Stage2Result, error) {
	s1, err := fn.Stage1Optimized(qon)
	if err != nil {
		return Stage1Result{}, Stage2Result{}, err
	}
	s2, err := fn.Stage2(qon, s1.CandidateHeights)
	if err != nil {
		return Stage1Result{}, Stage2Result{}, err
	}
	return s1, s2, nil
}

// Stage1Optimized is Stage1Segments restricted to the backward-scan result
// (optimize.IntersectingSegments) — the Stage-1-only half of
// AnswerQOnOptimized, exposed so callers (e.g. the Optimization-2 aggregated
// path in `verify`) can run Stage 1 alone before choosing which Stage 2
// path to take.
func (fn *FullNode) Stage1Optimized(qon QOn) (Stage1Result, error) {
	if fn.view != nil {
		indices, err := fn.view.IntersectingSegments(qon.HasRange, qon.TS, qon.TE)
		if err != nil {
			return Stage1Result{}, err
		}
		return fn.Stage1Segments(qon, indices)
	}
	indices := optimize.IntersectingSegments(headerTimes(fn.Headers()), fn.SegmentSize(), qon.HasRange, qon.TS, qon.TE)
	return fn.Stage1Segments(qon, indices)
}

// headerTimes projects the Opt. 1 header fields optimize.IntersectingSegments
// needs, decoupling `optimize` from `blas/chain` (see optimize/partition.go's
// doc comment on why).
func headerTimes(headers []chain.BlockHeader) []optimize.HeaderTimes {
	out := make([]optimize.HeaderTimes, len(headers))
	for i, h := range headers {
		out[i] = optimize.HeaderTimes{BlockTsMin: h.BlockTsMin, BlockTsMax: h.BlockTsMax, PastTsMax: h.PastTsMax}
	}
	return out
}

// KeywordRegistry returns the on-chain keyword-set-file registry
// (R-FUNC-006/E4) from the underlying chain — see chain.Chain.KeywordRegistry.
func (fn *FullNode) KeywordRegistry() map[uint64][]string {
	if fn.view != nil {
		return fn.view.KeywordRegistry()
	}
	return fn.Chain.KeywordRegistry()
}
