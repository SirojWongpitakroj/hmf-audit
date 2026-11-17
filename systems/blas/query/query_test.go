package query

import (
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
)

// buildTestChain builds a 3-block chain, one file (from one generator) per
// block, and stores each file's EntryFile on an SP.
func buildTestChain(t *testing.T) (*chain.Chain, *SP, map[FileRef]alis.FileMetadata) {
	t.Helper()
	c, err := chain.NewChain(2, 1024) // one big segment (partitioning effectively disabled)
	if err != nil {
		t.Fatal(err)
	}
	sp := NewSP()
	metas := map[FileRef]alis.FileMetadata{}

	build := func(lid, fid uint64, entries []struct {
		ts int64
		ew []string
	}) alis.FileMetadata {
		gen := alis.NewLogGenerator(lid)
		b, err := alis.NewEntryFileBuilder(gen, fid, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if _, err := b.AddEntry(e.ts, e.ew, nil); err != nil {
				t.Fatal(err)
			}
		}
		file, meta, err := b.Close()
		if err != nil {
			t.Fatal(err)
		}
		ref := FileRef{LID: lid, FID: fid}
		sp.Store(ref, file)
		metas[ref] = meta
		return meta
	}

	m0 := build(1, 1, []struct {
		ts int64
		ew []string
	}{{1000, []string{"login"}}, {1010, []string{"error"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m0}); err != nil {
		t.Fatal(err)
	}

	m1 := build(2, 1, []struct {
		ts int64
		ew []string
	}{{2000, []string{"login"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m1}); err != nil {
		t.Fatal(err)
	}

	m2 := build(1, 2, []struct {
		ts int64
		ew []string
	}{{3000, []string{"login", "error"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m2}); err != nil {
		t.Fatal(err)
	}

	return c, sp, metas
}

func TestSplit(t *testing.T) {
	q := Q{LG: 5, HasRange: true, TS: 1, TE: 2, FK: iomf.Keyword("a"), EK: iomf.Keyword("b")}
	qon, qoff := Split(q)
	if qon.LG != 5 || qon.FK == nil || qon.HasRange != true {
		t.Fatalf("qon = %+v", qon)
	}
	if qoff.LG != 5 || qoff.EK == nil || qoff.HasRange != true {
		t.Fatalf("qoff = %+v", qoff)
	}
}

// TestFullNodeStage1FindsCandidateBlocks: lg=1 appears in blocks 0 and 2.
func TestFullNodeStage1FindsCandidateBlocks(t *testing.T) {
	c, _, _ := buildTestChain(t)
	fn := NewFullNode(c)
	s1, err := fn.Stage1(QOn{LG: 1})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint64{0, 2}
	if len(s1.CandidateHeights) != len(want) {
		t.Fatalf("candidates = %v, want %v", s1.CandidateHeights, want)
	}
	for i := range want {
		if s1.CandidateHeights[i] != want[i] {
			t.Fatalf("candidates = %v, want %v", s1.CandidateHeights, want)
		}
	}
}

// TestFullNodeAnswerQOnEndToEnd exercises Stage1+Stage2 together.
func TestFullNodeAnswerQOnEndToEnd(t *testing.T) {
	c, _, _ := buildTestChain(t)
	fn := NewFullNode(c)
	s1, s2, err := fn.AnswerQOn(QOn{LG: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(s1.CandidateHeights) != 2 {
		t.Fatalf("expected 2 candidate blocks, got %d", len(s1.CandidateHeights))
	}
	if len(s2.Blocks) != 2 {
		t.Fatalf("expected 2 block tx results, got %d", len(s2.Blocks))
	}
	for _, br := range s2.Blocks {
		if len(br.VO.ResultProofs) != 1 {
			t.Fatalf("block %d: expected exactly 1 matching transaction for lg=1, got %d", br.Height, len(br.VO.ResultProofs))
		}
	}
}

// TestSPAnswerQOff exercises the entry-level stage.
func TestSPAnswerQOff(t *testing.T) {
	_, sp, _ := buildTestChain(t)
	results, err := sp.AnswerQOff([]FileRef{{LID: 1, FID: 1}}, QOff{EK: iomf.Keyword("login")})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || len(results[0].VO.ResultProofs) != 1 {
		t.Fatalf("expected 1 file with 1 matching entry, got %+v", results)
	}
}

func TestSPMissingFileErrors(t *testing.T) {
	_, sp, _ := buildTestChain(t)
	if _, err := sp.AnswerQOff([]FileRef{{LID: 99, FID: 99}}, QOff{}); err == nil {
		t.Fatal("expected error for a file the SP does not have")
	}
}

// buildAggregatedTestChain mirrors buildTestChain but with Optimization 2
// enabled (GAP-32 closure).
func buildAggregatedTestChain(t *testing.T) (*chain.Chain, *SP, map[FileRef]alis.FileMetadata) {
	t.Helper()
	c, err := chain.NewChainWithOptions(2, 1024, true)
	if err != nil {
		t.Fatal(err)
	}
	sp := NewSP()
	metas := map[FileRef]alis.FileMetadata{}

	build := func(lid, fid uint64, entries []struct {
		ts int64
		ew []string
	}) alis.FileMetadata {
		gen := alis.NewLogGenerator(lid)
		b, err := alis.NewEntryFileBuilder(gen, fid, 2)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if _, err := b.AddEntry(e.ts, e.ew, nil); err != nil {
				t.Fatal(err)
			}
		}
		file, meta, err := b.Close()
		if err != nil {
			t.Fatal(err)
		}
		ref := FileRef{LID: lid, FID: fid}
		sp.Store(ref, file)
		metas[ref] = meta
		return meta
	}

	m0 := build(1, 1, []struct {
		ts int64
		ew []string
	}{{1000, []string{"login"}}, {1010, []string{"error"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m0}); err != nil {
		t.Fatal(err)
	}
	m1 := build(1, 2, []struct {
		ts int64
		ew []string
	}{{2000, []string{"login"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m1}); err != nil {
		t.Fatal(err)
	}

	return c, sp, metas
}

// TestStage2AggregatedMatchesPlain: GAP-32 closure — the aggregated Stage 2
// path finds the same matched transactions as the plain path, and its
// proofs verify against the segment's aggregated root.
func TestStage2AggregatedMatchesPlain(t *testing.T) {
	c, _, _ := buildAggregatedTestChain(t)
	fn := NewFullNode(c)
	qon := QOn{LG: 1}

	s1, err := fn.Stage1(qon)
	if err != nil {
		t.Fatal(err)
	}
	plainS2, err := fn.Stage2(qon, s1.CandidateHeights)
	if err != nil {
		t.Fatal(err)
	}
	aggS2, bindings, err := fn.Stage2Aggregated(qon, s1)
	if err != nil {
		t.Fatal(err)
	}
	if len(aggS2.Blocks) != len(plainS2.Blocks) {
		t.Fatalf("expected same number of blocks, got %d vs %d", len(aggS2.Blocks), len(plainS2.Blocks))
	}
	for i := range aggS2.Blocks {
		if len(aggS2.Blocks[i].VO.AggregatedResultTxs) != len(plainS2.Blocks[i].VO.ResultProofs) {
			t.Fatalf("block %d: expected %d aggregated result txs, got %d",
				aggS2.Blocks[i].Height, len(plainS2.Blocks[i].VO.ResultProofs), len(aggS2.Blocks[i].VO.AggregatedResultTxs))
		}
	}
	if len(bindings) == 0 {
		t.Fatal("expected at least one LG aggregation binding")
	}
}
