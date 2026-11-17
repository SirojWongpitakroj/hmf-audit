package optimize

import (
	"reflect"
	"testing"
)

// mkHeaders builds a synthetic header sequence with the given per-segment
// (BlockTsMin, BlockTsMax) at each segment's anchor, and a running
// PastTsMax computed from the segment BlockTsMax values (monotonic).
func mkHeaders(segmentSize uint64, segments [][2]int64) []HeaderTimes {
	var headers []HeaderTimes
	var pastMax int64
	first := true
	for _, seg := range segments {
		if first || seg[1] > pastMax {
			pastMax = seg[1]
		}
		first = false
		// Only the anchor (last) header of each segment matters to the
		// scan; fill the segment with segmentSize headers all carrying
		// the same running values for simplicity (real chains would have
		// per-block-until-here running values, but the scan only reads
		// the anchor).
		for b := uint64(0); b < segmentSize; b++ {
			headers = append(headers, HeaderTimes{
				BlockTsMin: seg[0], BlockTsMax: seg[1], PastTsMax: pastMax,
			})
		}
	}
	return headers
}

// TestIntersectingSegmentsBasic: three segments with disjoint time ranges;
// a query range hitting only the middle segment should select just that
// one, without needing to terminate early.
func TestIntersectingSegmentsBasic(t *testing.T) {
	headers := mkHeaders(2, [][2]int64{{0, 100}, {200, 300}, {400, 500}})
	got := IntersectingSegments(headers, 2, true, 250, 260)
	want := []uint64{1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

// TestIntersectingSegmentsClosedInterval: D-3 deviation — a query range
// whose endpoint exactly equals a segment boundary value must still select
// that segment (closed interval), not exclude it (which the paper's
// printed strict `<`/`>` would do).
func TestIntersectingSegmentsClosedInterval(t *testing.T) {
	headers := mkHeaders(2, [][2]int64{{0, 100}, {200, 300}})
	// te == BlockTsMin of segment 1 exactly (200).
	got := IntersectingSegments(headers, 2, true, 150, 200)
	want := []uint64{1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v (closed-interval boundary case)", got, want)
	}
	// ts == BlockTsMax of segment 0 exactly (100).
	got2 := IntersectingSegments(headers, 2, true, 100, 150)
	want2 := []uint64{0}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("got %v, want %v (closed-interval boundary case)", got2, want2)
	}
}

// TestIntersectingSegmentsEarlyTermination: a query range entirely above
// all segments' timestamps still terminates once PastTsMax < ts is
// impossible to reach (PastTsMax only grows going forward, so it must
// process every segment) — instead test the SYMMETRIC case: a query range
// below the whole chain's minimum timestamps, verifying the scan
// terminates at PastTsMax < ts without needing to check every segment (we
// verify this indirectly via correctness, not by instrumenting the scan).
func TestIntersectingSegmentsEarlyTermination(t *testing.T) {
	headers := mkHeaders(2, [][2]int64{{0, 50}, {100, 150}, {200, 250}, {300, 350}})
	// ts=500 is above every segment's PastTsMax (350): nothing should
	// match, and the scan must terminate rather than run forever/out of
	// bounds.
	got := IntersectingSegments(headers, 2, true, 500, 600)
	if len(got) != 0 {
		t.Fatalf("expected no intersecting segments, got %v", got)
	}
	// ts=40 should terminate the scan once PastTsMax < 40, i.e. as soon as
	// it walks back to segment 0 (PastTsMax=50 there still >= 40, so it
	// processes segment 0 then stops afterward): segment 0 intersects
	// [0,50] with [40, 45].
	got2 := IntersectingSegments(headers, 2, true, 40, 45)
	want2 := []uint64{0}
	if !reflect.DeepEqual(got2, want2) {
		t.Fatalf("got %v, want %v", got2, want2)
	}
}

// TestNoRangeSelectsAllSegments: G2 extended to Opt. 1 — a query without a
// time range must cover every segment.
func TestNoRangeSelectsAllSegments(t *testing.T) {
	headers := mkHeaders(2, [][2]int64{{0, 100}, {200, 300}, {400, 500}})
	got := IntersectingSegments(headers, 2, false, 0, 0)
	want := []uint64{0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestSegmentCountAndAnchorHeight(t *testing.T) {
	if SegmentCount(5, 2) != 3 {
		t.Fatalf("SegmentCount(5,2) = %d, want 3", SegmentCount(5, 2))
	}
	if AnchorHeight(0, 2, 5) != 1 {
		t.Fatalf("AnchorHeight(0,2,5) = %d, want 1", AnchorHeight(0, 2, 5))
	}
	if AnchorHeight(2, 2, 5) != 4 {
		t.Fatalf("AnchorHeight(2,2,5) = %d, want 4 (partial final segment)", AnchorHeight(2, 2, 5))
	}
}
