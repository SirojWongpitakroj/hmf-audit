package query

import (
	"fmt"

	"blas/chain"
)

// ChainView is where a full node finds the on-chain index state it answers Stage 1 and Stage 2
// from. The default (nil view) is the in-memory *chain.Chain; a storage-backed view (see
// PostgresChain) reads only the segments and blocks a query needs, so a full node does not have to
// keep the block-level and transaction-level indexes in RAM.
type ChainView interface {
	SegmentSize() uint64
	// IntersectingSegments is optimize.IntersectingSegments over the chain's block headers.
	IntersectingSegments(hasRange bool, ts, te int64) ([]uint64, error)
	// Segments returns the segments with the given indexes, in the order given.
	Segments(indexes []uint64) ([]*chain.Segment, error)
	// Blocks returns the blocks at the given heights, in the order given.
	Blocks(heights []uint64) ([]*chain.Block, error)
	KeywordRegistry() map[uint64][]string
	// BeginRequest starts a new request: whatever the view kept for the previous one is dropped.
	BeginRequest()
}

// NewFullNodeFromView answers queries from a storage-backed view instead of an in-memory chain.
// Methods that need the whole chain (Headers, Stage1 over every segment) are not available on such a
// node; the Optimization-1 path (Stage1Optimized, Stage2, Stage2Aggregated) is.
func NewFullNodeFromView(view ChainView) *FullNode { return &FullNode{view: view} }

// BeginRequest tells a storage-backed view that a new request starts; it is a no-op for a chain.
func (fn *FullNode) BeginRequest() {
	if fn.view != nil {
		fn.view.BeginRequest()
	}
}

func (fn *FullNode) segments(indexes []uint64) ([]*chain.Segment, error) {
	if fn.view != nil {
		return fn.view.Segments(indexes)
	}
	all := fn.Chain.Segments()
	out := make([]*chain.Segment, len(indexes))
	for i, index := range indexes {
		if index >= uint64(len(all)) {
			return nil, fmt.Errorf("query: segment index %d out of range", index)
		}
		out[i] = all[index]
	}
	return out, nil
}

func (fn *FullNode) blocks(heights []uint64) ([]*chain.Block, error) {
	if fn.view != nil {
		return fn.view.Blocks(heights)
	}
	all := fn.Chain.Blocks()
	out := make([]*chain.Block, len(heights))
	for i, height := range heights {
		if height >= uint64(len(all)) {
			return nil, fmt.Errorf("query: candidate height %d out of range", height)
		}
		out[i] = all[height]
	}
	return out, nil
}

// ChainStats reports the database round trips and payload bytes a storage-backed view read since
// BeginRequest; both are zero for an in-memory chain.
func (fn *FullNode) ChainStats() (reads, payloadBytes int64) {
	if stats, ok := fn.view.(interface{ Stats() (int64, int64) }); ok {
		return stats.Stats()
	}
	return 0, 0
}
