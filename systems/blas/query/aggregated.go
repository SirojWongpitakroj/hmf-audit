package query

import (
	"fmt"

	"blas/alis"
	"blas/bhash"
	"blas/bitmap"
	"blas/optimize"
)

// AggregatedLGBinding is the per-segment evidence binding the Stage-1
// -verified LG bitmap to the segment's aggregated generator root (GAP-32
// closure, D-4): a proof that the block-level LG IIMT's extended leaf
// H(w‖H(bm)‖RH_i) for lg is genuinely committed in the segment's
// AggBlockLGRoot.
type AggregatedLGBinding struct {
	SegmentIndex uint64
	LID          uint64
	Bitmap       bitmap.Bitmap
	AggRoot      bhash.Digest
	LeafProof    optimize.AggProof
}

// Stage2Aggregated is Stage2's Optimization-2 counterpart: for every
// segment touched by s1's candidate blocks, it builds the LG aggregation
// binding once, then answers each candidate block using
// alis.ProcessTxLevelQueryAggregated instead of alis.ProcessTxLevelQuery
// (GAP-32 closure — see TRACKER.md for the documented scope: this replaces
// the per-block OMT membership proofs for the final matched transactions,
// not the LG-run neighbour completeness evidence, which still needs each
// block's own transaction-level OMT root).
func (fn *FullNode) Stage2Aggregated(qon QOn, s1 Stage1Result) (Stage2Result, map[uint64]AggregatedLGBinding, error) {
	segs := fn.Chain.Segments()
	bindings := map[uint64]AggregatedLGBinding{}

	for _, segRes := range s1.Segments {
		segIndex := segRes.SegmentBase / fn.SegmentSize()
		if segIndex >= uint64(len(segs)) {
			return Stage2Result{}, nil, fmt.Errorf("query: segment index %d out of range", segIndex)
		}
		seg := segs[segIndex]
		if seg.AggLGIIMT == nil {
			return Stage2Result{}, nil, fmt.Errorf("query: segment %d has no aggregated LG IIMT (Optimization 2 not enabled on this chain?)", segIndex)
		}
		kid, ok := seg.Store.LG.KID(alis.LIDKey(qon.LG))
		if !ok {
			continue // lg has no leaf in this segment at all (disclosed non-membership gap, see alis.ProcessBlockLevelQuery)
		}
		leafProof, ok := seg.AggLGIIMT.Prove(int(kid))
		if !ok {
			return Stage2Result{}, nil, fmt.Errorf("query: no aggregated LG IIMT proof at kid %d", kid)
		}
		aggTree, ok := seg.LGAggTrees[qon.LG]
		if !ok {
			return Stage2Result{}, nil, fmt.Errorf("query: no generator-aggregation tree for lid %d in segment %d", qon.LG, segIndex)
		}
		bindings[segIndex] = AggregatedLGBinding{
			SegmentIndex: segIndex, LID: qon.LG,
			Bitmap: segRes.VO.LGProof.Bitmap, AggRoot: aggTree.Root(), LeafProof: leafProof,
		}
	}

	var res Stage2Result
	blocks := fn.Chain.Blocks()
	for _, h := range s1.CandidateHeights {
		if h >= uint64(len(blocks)) {
			return Stage2Result{}, nil, fmt.Errorf("query: candidate height %d out of range", h)
		}
		segIndex := h / fn.SegmentSize()
		seg := segs[segIndex]
		aggTree, ok := seg.LGAggTrees[qon.LG]
		if !ok {
			return Stage2Result{}, nil, fmt.Errorf("query: no generator-aggregation tree for lid %d in segment %d", qon.LG, segIndex)
		}
		aggIndex := seg.LGAggIndex[qon.LG]
		b := blocks[h]
		vo, err := alis.ProcessTxLevelQueryAggregated(b.TxIndex, b.Transactions, h, aggTree, aggIndex, alis.TxQuery{
			LG: qon.LG, FK: qon.FK, HasRange: qon.HasRange, TS: qon.TS, TE: qon.TE,
		})
		if err != nil {
			return Stage2Result{}, nil, err
		}
		res.Blocks = append(res.Blocks, BlockTxResult{Height: h, VO: vo})
	}
	return res, bindings, nil
}
