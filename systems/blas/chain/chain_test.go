package chain

import (
	"testing"

	"blas/alis"
	"blas/bhash"
)

func mkMeta(lid, fid uint64, tsMin, tsMax int64, fw []string) alis.FileMetadata {
	return alis.FileMetadata{LID: lid, FID: fid, TsMin: tsMin, TsMax: tsMax, FW: fw}
}

// TestGenesisBlockPrevHashZero: a chain's first block has an all-zero
// PrevBlockHash (no predecessor).
func TestGenesisBlockPrevHashZero(t *testing.T) {
	c, err := NewChain(2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	b0, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 1, 100, 200, []string{"a"})})
	if err != nil {
		t.Fatal(err)
	}
	if b0.Header.PrevBlockHash != (bhash.Digest{}) {
		t.Fatalf("genesis PrevBlockHash should be zero, got %x", b0.Header.PrevBlockHash)
	}
	if b0.Header.BlockHeight != 0 {
		t.Fatalf("genesis BlockHeight should be 0, got %d", b0.Header.BlockHeight)
	}
}

// TestPrevBlockHashChaining: T-061-adjacent — each block's PrevBlockHash
// equals H(previous block's header), and altering a committed header
// changes what the next block would need to chain to.
func TestPrevBlockHashChaining(t *testing.T) {
	c, err := NewChain(2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	b0, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 1, 100, 200, []string{"a"})})
	if err != nil {
		t.Fatal(err)
	}
	b1, err := c.AddBlock([]alis.FileMetadata{mkMeta(2, 1, 300, 400, []string{"b"})})
	if err != nil {
		t.Fatal(err)
	}
	want := HashBlockHeader(b0.Header)
	if b1.Header.PrevBlockHash != want {
		t.Fatalf("b1.PrevBlockHash = %x, want H(b0.Header) = %x", b1.Header.PrevBlockHash, want)
	}

	tampered := b0.Header
	tampered.FileTsMax = 99999
	if HashBlockHeader(tampered) == want {
		t.Fatal("tampering a committed header should change its hash")
	}
}

// TestHeaderFieldsComputedFromTransactions: R-ALIS-023 — FileTsMin,
// FileTsMax, TxNum computed from the block's own transactions.
func TestHeaderFieldsComputedFromTransactions(t *testing.T) {
	c, err := NewChain(2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	metas := []alis.FileMetadata{
		mkMeta(1, 1, 100, 500, []string{"a"}),
		mkMeta(2, 1, 50, 300, []string{"b"}),
	}
	b, err := c.AddBlock(metas)
	if err != nil {
		t.Fatal(err)
	}
	if b.Header.FileTsMin != 50 || b.Header.FileTsMax != 500 {
		t.Fatalf("FileTsMin/Max = %d/%d, want 50/500", b.Header.FileTsMin, b.Header.FileTsMax)
	}
	if b.Header.TxNum != 2 {
		t.Fatalf("TxNum = %d, want 2", b.Header.TxNum)
	}
}

// TestPastTsMaxAndSegmentRunningValues: PastTsMax is the running max across
// all blocks; BlockTsMax/BlockTsMin are the running max/min within the
// CURRENT segment (F2 in DECISIONS.md).
func TestPastTsMaxAndSegmentRunningValues(t *testing.T) {
	c, err := NewChain(2, 2) // segment size 2, to exercise segment boundaries with few blocks
	if err != nil {
		t.Fatal(err)
	}
	b0, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 1, 100, 200, []string{"x"})})
	if err != nil {
		t.Fatal(err)
	}
	b1, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 2, 50, 600, []string{"x"})}) // still segment 0 (heights 0,1)
	if err != nil {
		t.Fatal(err)
	}
	b2, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 3, 700, 800, []string{"x"})}) // new segment (height 2)
	if err != nil {
		t.Fatal(err)
	}

	if b0.Header.PastTsMax != 200 || b0.Header.BlockTsMax != 200 || b0.Header.BlockTsMin != 100 {
		t.Fatalf("b0 running values wrong: %+v", b0.Header)
	}
	if b1.Header.PastTsMax != 600 || b1.Header.BlockTsMax != 600 || b1.Header.BlockTsMin != 50 {
		t.Fatalf("b1 running values wrong (segment-so-far max/min over b0,b1): %+v", b1.Header)
	}
	// b2 starts a new segment: BlockTsMax/Min reset to b2's own values,
	// but PastTsMax keeps the whole-chain running maximum.
	if b2.Header.PastTsMax != 800 {
		t.Fatalf("b2.PastTsMax = %d, want 800 (whole-chain running max)", b2.Header.PastTsMax)
	}
	if b2.Header.BlockTsMax != 800 || b2.Header.BlockTsMin != 700 {
		t.Fatalf("b2 segment-local running values wrong: %+v", b2.Header)
	}
}

// TestSegmentBoundaries: with segmentSize=2, blocks 0-1 are segment 0 and
// block 2 starts segment 1; SegmentForHeight reports the correct local
// height.
func TestSegmentBoundaries(t *testing.T) {
	c, err := NewChain(2, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, uint64(i), int64(i*10), int64(i*10+5), []string{"x"})}); err != nil {
			t.Fatal(err)
		}
	}
	segs := c.Segments()
	if len(segs) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segs))
	}
	if len(segs[0].Summaries) != 2 || len(segs[1].Summaries) != 1 {
		t.Fatalf("expected segment sizes [2,1], got [%d,%d]", len(segs[0].Summaries), len(segs[1].Summaries))
	}
	seg, local, err := c.SegmentForHeight(2)
	if err != nil {
		t.Fatal(err)
	}
	if seg.Base != 2 || local != 0 {
		t.Fatalf("SegmentForHeight(2) = base %d local %d, want base=2 local=0", seg.Base, local)
	}
}

func TestEmptyBlockRejected(t *testing.T) {
	c, err := NewChain(2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBlock(nil); err == nil {
		t.Fatal("expected error for an empty block")
	}
}

// TestShuffledMetadataIdenticalRoots: same set of files, different input
// order, must yield an identical transaction-level OMT root (R-GO-007,
// mirrors T-038 through the chain layer).
func TestShuffledMetadataIdenticalRoots(t *testing.T) {
	metas := []alis.FileMetadata{
		mkMeta(1, 1, 100, 200, []string{"a"}),
		mkMeta(1, 2, 200, 300, []string{"b"}),
		mkMeta(2, 1, 50, 60, []string{"c"}),
	}
	shuffled := []alis.FileMetadata{metas[2], metas[0], metas[1]}

	c1, _ := NewChain(2, 1024)
	b1, err := c1.AddBlock(metas)
	if err != nil {
		t.Fatal(err)
	}
	c2, _ := NewChain(2, 1024)
	b2, err := c2.AddBlock(shuffled)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Header.RH.TxOMTRoot != b2.Header.RH.TxOMTRoot {
		t.Fatal("shuffled file-metadata input should yield an identical transaction-level OMT root")
	}
}

// TestAggregationBuildsWhenEnabled: Optimization 2 (GAP-32 closure) —
// when useAggregation is set, each block's header carries a non-zero
// AggBlockLGRoot, and the segment's per-lid generator-aggregation trees
// and aggregated LG IIMT are populated; with aggregation disabled (the
// default NewChain), AggBlockLGRoot stays the zero digest.
func TestAggregationBuildsWhenEnabled(t *testing.T) {
	c, err := NewChainWithOptions(2, 1024, true)
	if err != nil {
		t.Fatal(err)
	}
	b0, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 1, 100, 200, []string{"x"})})
	if err != nil {
		t.Fatal(err)
	}
	if b0.Header.AggBlockLGRoot == (bhash.Digest{}) {
		t.Fatal("expected a non-zero AggBlockLGRoot when aggregation is enabled")
	}
	segs := c.Segments()
	if len(segs) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(segs))
	}
	if segs[0].AggLGIIMT == nil {
		t.Fatal("expected the segment's aggregated LG IIMT to be built")
	}
	if len(segs[0].LGAggTrees) != 1 {
		t.Fatalf("expected exactly 1 lid's generator-aggregation tree, got %d", len(segs[0].LGAggTrees))
	}
	tree, ok := segs[0].LGAggTrees[1]
	if !ok {
		t.Fatal("expected a generator-aggregation tree for lid=1")
	}
	if tree.Root() != segs[0].AggLGIIMT.Root() && tree.Root() == (bhash.Digest{}) {
		t.Fatal("generator-aggregation tree should have a non-zero root")
	}

	c2, err := NewChain(2, 1024) // aggregation disabled (default)
	if err != nil {
		t.Fatal(err)
	}
	b0NoAgg, err := c2.AddBlock([]alis.FileMetadata{mkMeta(1, 1, 100, 200, []string{"x"})})
	if err != nil {
		t.Fatal(err)
	}
	if b0NoAgg.Header.AggBlockLGRoot != (bhash.Digest{}) {
		t.Fatal("expected a zero AggBlockLGRoot when aggregation is disabled")
	}
}

// TestAggregationGrowsAcrossBlocksInSegment: a lid with transactions in two
// blocks of the same segment ends up with a generator-aggregation tree
// covering both, and the tree's root changes as the second block is added.
func TestAggregationGrowsAcrossBlocksInSegment(t *testing.T) {
	c, err := NewChainWithOptions(2, 1024, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 1, 100, 200, []string{"x"})}); err != nil {
		t.Fatal(err)
	}
	segs := c.Segments()
	rootAfterFirst := segs[0].LGAggTrees[1].Root()

	if _, err := c.AddBlock([]alis.FileMetadata{mkMeta(1, 2, 300, 400, []string{"x"})}); err != nil {
		t.Fatal(err)
	}
	segs = c.Segments()
	rootAfterSecond := segs[0].LGAggTrees[1].Root()

	if rootAfterFirst == rootAfterSecond {
		t.Fatal("expected the generator-aggregation root to change after a second block adds more of lid 1's transactions")
	}
	idx := segs[0].LGAggIndex[1]
	if len(idx) != 2 {
		t.Fatalf("expected 2 indexed transactions for lid=1, got %d", len(idx))
	}
	if _, ok := idx[alis.TxAggKey{Height: 0, TSN: 0}]; !ok {
		t.Fatal("expected an index entry for (height=0, tsn=0)")
	}
	if _, ok := idx[alis.TxAggKey{Height: 1, TSN: 0}]; !ok {
		t.Fatal("expected an index entry for (height=1, tsn=0)")
	}
}
