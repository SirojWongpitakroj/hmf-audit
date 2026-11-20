package query

import (
	"testing"

	"blas/optimize"
)

// TestBackwardScanMatchesOptimizeIntersectingSegments: the paged scan over stored anchor headers
// selects exactly what optimize.IntersectingSegments selects over the full header list.
func TestBackwardScanMatchesOptimizeIntersectingSegments(t *testing.T) {
	const segmentSize = 3
	for _, blocks := range []int{1, 2, 3, 4, 7, 9, 10, 40} {
		headers := make([]optimize.HeaderTimes, blocks)
		var pastMax int64
		for i := range headers {
			start := int64(i)*100 + int64((i*37)%90) // mostly increasing, with overlap and a few late blocks
			end := start + 150
			if end > pastMax {
				pastMax = end
			}
			segStart := (i / segmentSize) * segmentSize
			segMin, segMax := start, end
			for j := segStart; j < i; j++ {
				segMin = min(segMin, headers[j].BlockTsMin)
				segMax = max(segMax, headers[j].BlockTsMax)
			}
			headers[i] = optimize.HeaderTimes{BlockTsMin: segMin, BlockTsMax: segMax, PastTsMax: pastMax}
		}
		segments := optimize.SegmentCount(blocks, segmentSize)
		anchors := make([]AnchorRow, segments)
		for i := range anchors {
			anchors[i] = AnchorRow{Seg: uint64(i), Times: headers[optimize.AnchorHeight(uint64(i), segmentSize, blocks)]}
		}
		for _, hasRange := range []bool{false, true} {
			for ts := int64(-50); ts < int64(blocks)*100+300; ts += 37 {
				for _, width := range []int64{0, 60, 400, 100000} {
					te := ts + width
					want := optimize.IntersectingSegments(headers, segmentSize, hasRange, ts, te)
					for _, page := range []int64{1, 2, 64} {
						got, err := BackwardScan(segments, hasRange, ts, te, func(upper int64) ([]AnchorRow, error) {
							var rows []AnchorRow
							for seg := upper; seg >= 0 && seg > upper-page; seg-- {
								rows = append(rows, anchors[seg])
							}
							return rows, nil
						})
						if err != nil {
							t.Fatal(err)
						}
						if len(got) != len(want) {
							t.Fatalf("blocks=%d range=%v [%d,%d] page=%d: got %v, want %v", blocks, hasRange, ts, te, page, got, want)
						}
						for i := range want {
							if got[i] != want[i] {
								t.Fatalf("blocks=%d range=%v [%d,%d] page=%d: got %v, want %v", blocks, hasRange, ts, te, page, got, want)
							}
						}
					}
				}
			}
		}
	}
}
