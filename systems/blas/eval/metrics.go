package eval

import (
	"blas/alis"
	"blas/bitmap"
	"blas/iimt"
	"blas/omt"
	"blas/optimize"
	"blas/query"
	"blas/rimbt"
)

func bmBytes(bm bitmap.Bitmap) int { return len(bm.Bytes()) }

// KB is 1024 bytes (H3: "KB = 1024 bytes, consistently").
const KB = 1024

// VOSize computes the semantic serialized payload size (G4: "raw
// uncompressed bitmaps; sorted identifiers; ... Reported VO size counts
// semantic serialized payload bytes, not Go object/allocator overhead") of
// a full three-stage answer: Stage 1 + Stage 2 (from the full node) plus
// every entry-level VO returned by the SP. This is the harness's own
// accounting convention for benchmarking — the wire encoding itself is
// GAP-38 territory and unresolved by DECISIONS.md; this function does not
// invent one, it only counts bytes semantically for a size metric.
//
// bindings carries the Optimization-2 per-segment LG aggregation bindings
// (query.FullNode.Stage2Aggregated's second return value) and must be nil
// when s2 was produced by the plain (non-aggregated) Stage2 path — each
// TxLevelVO already reports either ResultProofs (plain) or
// AggregatedResultTxs (aggregated), so txLevelVOSize accounts for whichever
// is populated on its own; bindings only adds the per-segment binding
// proof's own bytes, which exist exactly once per segment regardless of how
// many of that segment's blocks are candidates.
func VOSize(s1 query.Stage1Result, s2 query.Stage2Result, fileResults []query.FileResult, bindings map[uint64]query.AggregatedLGBinding) int {
	total := 0
	for _, seg := range s1.Segments {
		total += blockLevelVOSize(seg.VO)
	}
	for _, b := range s2.Blocks {
		total += txLevelVOSize(b.VO)
	}
	for _, binding := range bindings {
		total += aggregatedLGBindingSize(binding)
	}
	for _, fr := range fileResults {
		total += entryVOSize(fr.VO)
	}
	return total
}

const digestSize = 32
const uint64Size = 8

func iimtProofSize(p iimt.Proof) int {
	sz := len(p.Keyword) + bmBytes(p.Bitmap) + uint64Size // keyword bytes + bitmap bytes + a length field
	for _, step := range p.Steps {
		sz += uint64Size // child index
		sz += len(step.SiblingHashes) * digestSize
	}
	return sz
}

func omtProofSize(p omt.Proof) int {
	sz := uint64Size + len(p.Object)
	for _, step := range p.Steps {
		sz += uint64Size
		sz += len(step.SiblingHashes) * digestSize
	}
	return sz
}

func rimbtProofSize(p rimbt.Proof) int {
	sz := uint64Size + bmBytes(p.Bitmap)
	for _, step := range p.Steps {
		sz += uint64Size
		sz += len(step.SiblingHashes) * digestSize
		sz += len(step.V) * uint64Size
	}
	return sz
}

func aggProofSize(p optimize.AggProof) int {
	sz := uint64Size + digestSize // leaf index + leaf hash
	for _, step := range p.Steps {
		sz += uint64Size // child index
		sz += len(step.SiblingHashes) * digestSize
	}
	return sz
}

// aggregatedTxProofSize sizes one Optimization-2 matched-transaction proof:
// the block height it was found in, the transaction object itself
// (encoded the same way it is hashed, per bhash's field-order/encoding
// rules), and the proof against the segment-wide aggregation root.
func aggregatedTxProofSize(p alis.AggregatedTxProof) int {
	return uint64Size + len(alis.EncodeTransaction(p.Transaction)) + aggProofSize(p.Proof)
}

// aggregatedLGBindingSize sizes one segment's LG-aggregation binding
// evidence (query.AggregatedLGBinding): the bitmap Stage 1 already
// authenticated for this segment, re-carried here so the extended IIMT leaf
// hash can be recomputed, plus the aggregation-root digest and the proof
// binding that leaf into the segment's AggBlockLGRoot.
func aggregatedLGBindingSize(b query.AggregatedLGBinding) int {
	return bmBytes(b.Bitmap) + digestSize + aggProofSize(b.LeafProof)
}

func rimbtRangeResultSize(r rimbt.RangeResult) int {
	sz := 0
	if r.LowerReal {
		sz += rimbtProofSize(r.LowerProof)
	}
	if r.AlphaNeighborReal {
		sz += rimbtProofSize(r.AlphaNeighborProof)
	}
	if r.UpperReal {
		sz += rimbtProofSize(r.UpperProof)
	}
	if r.BetaNeighborReal {
		sz += rimbtProofSize(r.BetaNeighborProof)
	}
	return sz
}

func blockLevelVOSize(vo alis.BlockLevelVO) int {
	sz := 0
	if vo.LGPresent {
		sz += iimtProofSize(vo.LGProof)
	}
	for _, ke := range vo.FKEvidence {
		sz += iimtProofSize(ke.Proof)
	}
	if vo.TsMaxRangeUsed {
		sz += rimbtRangeResultSize(vo.TsMaxRange)
	}
	if vo.TsMinRangeUsed {
		sz += rimbtRangeResultSize(vo.TsMinRange)
	}
	sz += len(vo.CandidateHeights) * uint64Size
	return sz
}

func txLevelVOSize(vo alis.TxLevelVO) int {
	sz := 0
	for _, p := range vo.LGProofs {
		sz += omtProofSize(p)
	}
	if vo.LGLeftNeighbor != nil {
		sz += omtProofSize(*vo.LGLeftNeighbor)
	}
	if vo.LGRightNeighbor != nil {
		sz += omtProofSize(*vo.LGRightNeighbor)
	}
	for _, ke := range vo.FKEvidence {
		sz += iimtProofSize(ke.Proof)
	}
	if vo.HasRange {
		sz += rimbtRangeResultSize(vo.TsMinRange)
		sz += rimbtRangeResultSize(vo.TsMaxRange)
	}
	for _, p := range vo.ResultProofs {
		sz += omtProofSize(p)
	}
	for _, e := range vo.AggregatedResultTxs {
		sz += aggregatedTxProofSize(e)
	}
	return sz
}

func entryVOSize(vo alis.EntryVO) int {
	sz := 0
	for _, ke := range vo.KeywordEvidence {
		sz += iimtProofSize(ke.Proof)
	}
	if vo.LowerBoundary != nil {
		sz += omtProofSize(*vo.LowerBoundary)
	}
	if vo.UpperBoundary != nil {
		sz += omtProofSize(*vo.UpperBoundary)
	}
	for _, p := range vo.ResultProofs {
		sz += omtProofSize(p)
	}
	sz += omtProofSize(vo.ExtentProof)
	return sz
}
