package verify

import (
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/query"
)

// buildMultiSegmentFixture builds a 5-block chain with segment size 2 (so
// segments are {0,1}, {2,3}, {4}), one file per block, all from lid=1, with
// increasing timestamps chosen so only the middle segment intersects a
// narrow query range.
func buildMultiSegmentFixture(t *testing.T) fixture {
	t.Helper()
	c, err := chain.NewChain(2, 2) // segment size 2
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}
	gen := alis.NewLogGenerator(1)

	tsForBlock := []int64{100, 200, 2000, 2100, 5000}
	for i, ts := range tsForBlock {
		b, err := alis.NewEntryFileBuilder(gen, uint64(i), 2)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.AddEntry(ts, []string{"login"}, nil); err != nil {
			t.Fatal(err)
		}
		file, meta, err := b.Close()
		if err != nil {
			t.Fatal(err)
		}
		ref := query.FileRef{LID: 1, FID: uint64(i)}
		sp.Store(ref, file)
		registry[1] = gen.KeywordUniverse()
		if _, err := c.AddBlock([]alis.FileMetadata{meta}); err != nil {
			t.Fatal(err)
		}
	}

	fn := query.NewFullNode(c)
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, b := range c.Blocks() {
		headers[i] = b.Header
	}
	auditor := NewAuditor(headers, 2)
	return fixture{chain: c, fn: fn, sp: sp, auditor: auditor, registry: registry}
}

// TestOptimizedMatchesBaseline: T-049 — the optimized (segment-scan)
// pipeline and the baseline (query-every-segment) pipeline must agree on
// results and accept/reject for the same query.
func TestOptimizedMatchesBaseline(t *testing.T) {
	f := buildMultiSegmentFixture(t)
	q := query.Q{LG: 1, HasRange: true, TS: 1900, TE: 2200, EK: iomf.Keyword("login")}

	baseline := f.auditor.RunQueryWithOptions(f.fn, f.sp, f.registry, q, Options{UseSegmentScan: false})
	optimized := f.auditor.RunQueryWithOptions(f.fn, f.sp, f.registry, q, Options{UseSegmentScan: true})

	if baseline.Accept != optimized.Accept {
		t.Fatalf("accept mismatch: baseline=%v optimized=%v", baseline.Accept, optimized.Accept)
	}
	if len(baseline.Entries) != len(optimized.Entries) {
		t.Fatalf("entry count mismatch: baseline=%d optimized=%d", len(baseline.Entries), len(optimized.Entries))
	}
	if len(baseline.Entries) != 2 {
		t.Fatalf("expected 2 matching entries (blocks 2,3), got %d", len(baseline.Entries))
	}
}

// TestOptimizedVisitsFewerSegments: the optimized path should actually
// visit fewer segments than the baseline for a narrow query range —
// confirms the optimization does something, not just that it's correct.
func TestOptimizedVisitsFewerSegments(t *testing.T) {
	f := buildMultiSegmentFixture(t)
	qon := query.QOn{LG: 1, HasRange: true, TS: 1900, TE: 2200}

	baselineS1, err := f.fn.Stage1(qon)
	if err != nil {
		t.Fatal(err)
	}
	optimizedS1, _, err := f.fn.AnswerQOnOptimized(qon)
	if err != nil {
		t.Fatal(err)
	}
	if len(baselineS1.Segments) != 3 {
		t.Fatalf("expected baseline to visit all 3 segments, got %d", len(baselineS1.Segments))
	}
	if len(optimizedS1.Segments) >= len(baselineS1.Segments) {
		t.Fatalf("expected optimized to visit fewer segments than baseline (%d), got %d", len(baselineS1.Segments), len(optimizedS1.Segments))
	}
}

// TestOptimizedRejectsSkippedIntersectingSegment: T-048 — a full node that
// silently skips an intersecting segment (simulated by manually building a
// Stage1Result missing that segment) must be rejected by the coverage
// -equality check.
func TestOptimizedRejectsSkippedIntersectingSegment(t *testing.T) {
	f := buildMultiSegmentFixture(t)
	qon := query.QOn{LG: 1, HasRange: true, TS: 1900, TE: 2200}

	full, _, err := f.fn.AnswerQOnOptimized(qon)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Segments) == 0 {
		t.Fatal("expected at least one intersecting segment")
	}
	tampered := full
	tampered.Segments = full.Segments[1:] // drop the first (should-be-included) segment
	if _, ok := f.auditor.verifyStage1Optimized(qon, tampered); ok {
		t.Fatal("dropping an intersecting segment should be rejected by the coverage check")
	}

	// Also confirm a full node that claims an EXTRA (non-intersecting)
	// segment is rejected (over-claiming coverage is also a mismatch).
	baselineAll, err := f.fn.Stage1(qon)
	if err != nil {
		t.Fatal(err)
	}
	if len(baselineAll.Segments) <= len(full.Segments) {
		t.Skip("no extra segment available to test over-claiming")
	}
	over := full
	over.Segments = append(append([]query.SegmentBlockResult{}, full.Segments...), baselineAll.Segments[len(baselineAll.Segments)-1])
	if _, ok := f.auditor.verifyStage1Optimized(qon, over); ok {
		t.Fatal("claiming an extra non-intersecting segment should be rejected by the coverage check")
	}
}
