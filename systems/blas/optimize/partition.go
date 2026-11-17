// Package optimize implements both BLAS design optimizations (VI):
// Optimization 1, the block-level index partition (segments, backward
// header scan), and Optimization 2, on-chain index aggregation.
//
// This package intentionally does NOT import `blas/chain`, even though its
// functions operate on header-derived data: `chain` imports `alis`, and
// closing GAP-32 requires `alis` to import `optimize` (for the aggregated
// MHT types), which would make chain -> alis -> optimize -> chain a cycle.
// Callers pass the minimal header fields each function needs (HeaderTimes)
// instead of a full chain.BlockHeader.
package optimize

// HeaderTimes is the minimal per-header data IntersectingSegments needs
// (BlockTsMin, BlockTsMax, PastTsMax — the Opt. 1 header fields, VI),
// decoupling this package from `blas/chain`'s BlockHeader type.
type HeaderTimes struct {
	BlockTsMin int64
	BlockTsMax int64
	PastTsMax  int64
}

// SegmentCount returns the number of segments (completed or open) for a
// chain with numBlocks blocks and the given segment size S.
func SegmentCount(numBlocks int, segmentSize uint64) uint64 {
	if numBlocks == 0 {
		return 0
	}
	return (uint64(numBlocks) + segmentSize - 1) / segmentSize
}

// AnchorHeight returns the global BlockHeight of the header carrying
// segment i's committed block-level roots (E2: the segment's roots as
// committed by its most recent block so far).
func AnchorHeight(segmentIndex, segmentSize uint64, numBlocks int) uint64 {
	anchor := segmentIndex*segmentSize + segmentSize - 1
	if anchor >= uint64(numBlocks) {
		anchor = uint64(numBlocks) - 1
	}
	return anchor
}

// IntersectingSegments implements the R-OPT-004..007 backward scan: the
// auditor walks segment-final headers newest → oldest and selects every
// segment whose [BlockTsMin, BlockTsMax] intersects [ts, te], using the
// CLOSED-interval semantics of deviation D-3 (BlockTsMin <= te AND
// BlockTsMax >= ts — the paper prints strict `<`/`>`, which would drop a
// block whose timestamp exactly equals an interval endpoint). The scan
// terminates once PastTsMax < ts (R-OPT-007): no earlier segment can
// possibly intersect, since PastTsMax is a running maximum and therefore
// non-increasing as segments get older.
//
// If hasRange is false, a query without a time range covers all relevant
// segments (G2 in DECISIONS.md, extended: no time predicate means no
// segment can be excluded by one).
//
// The returned slice is sorted ascending by segment index (the scan order
// itself is newest-to-oldest, but the returned set is order-independent —
// only membership matters for the coverage-equality check, R-OPT-008/F3).
func IntersectingSegments(headers []HeaderTimes, segmentSize uint64, hasRange bool, ts, te int64) []uint64 {
	numSegments := SegmentCount(len(headers), segmentSize)
	if !hasRange {
		all := make([]uint64, numSegments)
		for i := range all {
			all[i] = uint64(i)
		}
		return all
	}
	var selected []uint64
	for i := numSegments; i > 0; i-- {
		segIndex := i - 1
		anchor := AnchorHeight(segIndex, segmentSize, len(headers))
		h := headers[anchor]
		if h.BlockTsMin <= te && h.BlockTsMax >= ts {
			selected = append(selected, segIndex)
		}
		if h.PastTsMax < ts {
			break
		}
	}
	// Sort ascending (the scan walks newest-first, so `selected` is
	// currently descending).
	for i, j := 0, len(selected)-1; i < j; i, j = i+1, j-1 {
		selected[i], selected[j] = selected[j], selected[i]
	}
	return selected
}
