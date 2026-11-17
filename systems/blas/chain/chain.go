package chain

import (
	"fmt"
	"sort"

	"blas/alis"
	"blas/bhash"
	"blas/bitmap"
	"blas/iomf"
	"blas/optimize"
)

// Block is one committed, immutable block: header plus the transaction
// list and its transaction-level index — exactly one iomf.Store
// (R-ALIS-010..014 reuse the M5 machinery, as established in M6).
type Block struct {
	Header       BlockHeader
	Transactions []alis.Transaction
	TxIndex      *iomf.Store
}

// Segment is one Optimization-1 segment (VI): the block-level index is
// built independently over exactly the blocks in this segment (F1 in
// DECISIONS.md — the complete block-level index is partitioned, not just
// the RIMBTs). Base is the first global BlockHeight in the segment; a
// block's local (segment-relative) height is BlockHeight-Base (D6).
type Segment struct {
	Base      uint64
	Summaries []alis.BlockSummary // index i == local height i
	Store     *alis.BlockLevelStore

	// Optimization-2 fields (VI, R-OPT-012), populated only when the chain
	// was built with UseAggregation (GAP-32 closure — see AddBlock and
	// TRACKER.md for the concrete mechanism). LGAggTrees/LGAggIndex are
	// per-lid; AggLGIIMT is the segment's aggregated block-level LG IIMT
	// (extended leaves, D-4), whose root is AggBlockLGRoot in the anchor
	// header of this segment as of each block.
	LGAggTrees map[uint64]*optimize.AggregatedTree
	LGAggIndex map[uint64]map[alis.TxAggKey]int
	AggLGIIMT  *optimize.AggregatedTree
}

// Chain is the deterministic, single-process blockchain emulator (E1): a
// full node's local state after acting as block generator for every block
// appended so far. No consensus, no networking (R-FUNC-019 / GAP-23 →
// DECISIONS.md E1).
//
// SegmentSize models Optimization 1 (VI): "disabled" partitioning is the
// degenerate case SegmentSize >= total block count, so the whole chain is
// one segment — not a structurally different code path (consistent with F6
// applying the same idea to Optimization 2).
type Chain struct {
	fanout      int
	segmentSize uint64

	// useAggregation enables Optimization 2 (VI): building the per-segment
	// aggregated generator MHTs and the extended block-level LG IIMT
	// alongside the plain block-level index (GAP-32 closure). Off by
	// default (NewChain); enabled via NewChainWithOptions.
	useAggregation bool

	blocks   []*Block
	segments []*Segment // completed segments, in order
	current  *Segment   // the currently open (possibly partial) segment

	havePastTsMax    bool
	pastTsMaxRunning int64

	// keywordRegistry is the on-chain "keyword set file" (R-FUNC-006):
	// each log generator's maintained entry-keyword universe, published
	// as plain metadata (E4 in DECISIONS.md: "on-chain metadata/registry
	// ... do not add another authenticated data structure for it" — so
	// this is intentionally NOT Merkle-committed, just stored directly on
	// the Chain object rather than tracked in a separate, manually-passed
	// map by every caller).
	keywordRegistry map[uint64][]string
}

// RegisterKeywordUniverse publishes lid's current entry-keyword universe
// (alis.LogGenerator.KeywordUniverse()) to the on-chain keyword-set-file
// registry. A log generator calls this whenever its universe changes
// (typically after closing a file); the auditor reads it back via
// KeywordRegistry (R-FUNC-006, E4).
func (c *Chain) RegisterKeywordUniverse(lid uint64, universe []string) {
	if c.keywordRegistry == nil {
		c.keywordRegistry = map[uint64][]string{}
	}
	c.keywordRegistry[lid] = append([]string{}, universe...)
}

// KeywordRegistry returns a copy of the on-chain keyword-set-file registry
// (lid -> that generator's entry-keyword universe, in kid order).
func (c *Chain) KeywordRegistry() map[uint64][]string {
	out := make(map[uint64][]string, len(c.keywordRegistry))
	for lid, universe := range c.keywordRegistry {
		out[lid] = append([]string{}, universe...)
	}
	return out
}

// NewChain starts an empty chain with the given tree fanout F and
// Optimization-1 segment size S (VIII-B: F=4, S=1024 in the paper-style
// evaluation; both are construction parameters here, never hard-coded,
// R-GO-018/R-GO-027).
func NewChain(fanout int, segmentSize uint64) (*Chain, error) {
	return NewChainWithOptions(fanout, segmentSize, false)
}

// NewChainWithOptions is NewChain plus Optimization 2's on/off switch
// (useAggregation, VI/R-OPT-015: independently switchable). Every existing
// caller of NewChain is unaffected (aggregation off), so this is additive,
// not a breaking change.
func NewChainWithOptions(fanout int, segmentSize uint64, useAggregation bool) (*Chain, error) {
	if fanout < 2 {
		return nil, fmt.Errorf("chain: fanout must be >= 2, got %d", fanout)
	}
	if segmentSize == 0 {
		return nil, fmt.Errorf("chain: segmentSize must be >= 1, got %d", segmentSize)
	}
	return &Chain{fanout: fanout, segmentSize: segmentSize, useAggregation: useAggregation}, nil
}

// Fanout returns F.
func (c *Chain) Fanout() int { return c.fanout }

// SegmentSize returns S.
func (c *Chain) SegmentSize() uint64 { return c.segmentSize }

// Blocks returns the committed blocks, in ascending BlockHeight order. The
// returned slice is the chain's own backing slice; callers must not mutate
// it.
func (c *Chain) Blocks() []*Block { return c.blocks }

// Segments returns every segment, completed or currently open, in order.
func (c *Chain) Segments() []*Segment {
	out := append([]*Segment{}, c.segments...)
	if c.current != nil {
		out = append(out, c.current)
	}
	return out
}

// AddBlock acts as the block generator (R-FUNC-008) for one new block: it
// sorts the given file metadata by (lid, fid), assigns tsn from 0, builds
// the transaction-level index, computes the header fields (R-ALIS-023,
// R-FUNC-009), maintains the current segment's block-level index (F1/F2),
// and chains PrevBlockHash to the previous block (V-B).
func (c *Chain) AddBlock(metas []alis.FileMetadata) (*Block, error) {
	if len(metas) == 0 {
		return nil, fmt.Errorf("chain: cannot create a block with zero transactions")
	}

	height := uint64(len(c.blocks))

	txBuilder, txStore, err := alis.BuildTransactionLevel(c.fanout, metas)
	if err != nil {
		return nil, err
	}
	txs := txBuilder.Transactions()

	fileTsMin, fileTsMax := txs[0].TsMin, txs[0].TsMax
	lidSeen := map[uint64]bool{}
	var lids []uint64
	fwSeen := map[string]bool{}
	var fileKeywords []string
	for _, tx := range txs {
		if tx.TsMin < fileTsMin {
			fileTsMin = tx.TsMin
		}
		if tx.TsMax > fileTsMax {
			fileTsMax = tx.TsMax
		}
		if !lidSeen[tx.LID] {
			lidSeen[tx.LID] = true
			lids = append(lids, tx.LID)
		}
		for _, w := range tx.FW {
			if !fwSeen[w] {
				fwSeen[w] = true
				fileKeywords = append(fileKeywords, w)
			}
		}
	}
	sort.Slice(lids, func(i, j int) bool { return lids[i] < lids[j] })
	sort.Strings(fileKeywords)

	segmentIndex := height / c.segmentSize
	localHeight := height - segmentIndex*c.segmentSize
	if c.current == nil || localHeight == 0 {
		if c.current != nil {
			c.segments = append(c.segments, c.current)
		}
		c.current = &Segment{Base: height}
	}
	c.current.Summaries = append(c.current.Summaries, alis.BlockSummary{
		BlockHeight: localHeight, LIDs: lids, FileKeywords: fileKeywords,
		FileTsMin: fileTsMin, FileTsMax: fileTsMax,
	})
	blockLevelStore, err := alis.BuildBlockLevel(c.fanout, c.current.Summaries)
	if err != nil {
		return nil, err
	}
	c.current.Store = blockLevelStore

	if !c.havePastTsMax || fileTsMax > c.pastTsMaxRunning {
		c.pastTsMaxRunning = fileTsMax
		c.havePastTsMax = true
	}
	segTsMax := c.current.Summaries[0].FileTsMax
	segTsMin := c.current.Summaries[0].FileTsMin
	for _, s := range c.current.Summaries {
		if s.FileTsMax > segTsMax {
			segTsMax = s.FileTsMax
		}
		if s.FileTsMin < segTsMin {
			segTsMin = s.FileTsMin
		}
	}

	txOMTRoot, err := txStore.OMT.Root()
	if err != nil {
		return nil, err
	}
	txFKRoot, err := txStore.IIMT.Root()
	if err != nil {
		return nil, err
	}
	txTsMinRoot, err := txStore.RIMBTs["ts_min"].Root()
	if err != nil {
		return nil, err
	}
	txTsMaxRoot, err := txStore.RIMBTs["ts_max"].Root()
	if err != nil {
		return nil, err
	}
	blkLGRoot, err := blockLevelStore.LG.Root()
	if err != nil {
		return nil, err
	}
	blkFKRoot, err := blockLevelStore.FK.Root()
	if err != nil {
		return nil, err
	}
	blkTsMinRoot, err := blockLevelStore.FileTsMin.Root()
	if err != nil {
		return nil, err
	}
	blkTsMaxRoot, err := blockLevelStore.FileTsMax.Root()
	if err != nil {
		return nil, err
	}

	var aggBlockLGRoot bhash.Digest
	if c.useAggregation {
		aggBlockLGRoot, err = c.buildAggregation(segmentIndex, height, txs, blockLevelStore)
		if err != nil {
			return nil, err
		}
	}

	var prevHash bhash.Digest
	if len(c.blocks) > 0 {
		prevHash = HashBlockHeader(c.blocks[len(c.blocks)-1].Header)
	}

	header := BlockHeader{
		BlockHeight:   height,
		PrevBlockHash: prevHash,
		FileTsMin:     fileTsMin,
		FileTsMax:     fileTsMax,
		TxNum:         uint64(len(txs)),
		RH: RootHashSet{
			TxOMTRoot: txOMTRoot, TxFKRoot: txFKRoot, TxTsMinRoot: txTsMinRoot, TxTsMaxRoot: txTsMaxRoot,
			BlockLGRoot: blkLGRoot, BlockFKRoot: blkFKRoot, BlockFileTsMinRoot: blkTsMinRoot, BlockFileTsMaxRoot: blkTsMaxRoot,
		},
		PastTsMax:      c.pastTsMaxRunning,
		BlockTsMax:     segTsMax,
		BlockTsMin:     segTsMin,
		AggBlockLGRoot: aggBlockLGRoot,
	}

	block := &Block{Header: header, Transactions: txs, TxIndex: txStore}
	c.blocks = append(c.blocks, block)
	return block, nil
}

// buildAggregation implements Optimization 2's on-chain aggregation build
// (VI, R-OPT-012; GAP-32 closure — see TRACKER.md for the scope and
// reasoning behind this concrete mechanism). It rebuilds, for the segment
// containing `height`, every lid's generator-aggregation MHT over all of
// that lid's transactions committed so far in the segment (including the
// block currently being added), then the segment's aggregated block-level
// LG IIMT (extended leaves, D-4) sharing the SAME lid universe and kid
// order as the plain block-level LG IIMT (alis.LIDUniverse,
// alis.BuildBlockLevel's own rule) so a leaf's position is identical in
// both trees. Returns the aggregated tree's root for the caller to store in
// the new block's header.
func (c *Chain) buildAggregation(segmentIndex, height uint64, txs []alis.Transaction, blockLevelStore *alis.BlockLevelStore) (bhash.Digest, error) {
	segBase := segmentIndex * c.segmentSize
	var segBlocks []alis.BlockTxs
	for i, b := range c.blocks[segBase:] {
		segBlocks = append(segBlocks, alis.BlockTxs{Height: segBase + uint64(i), Txs: b.Transactions})
	}
	segBlocks = append(segBlocks, alis.BlockTxs{Height: height, Txs: txs})

	lgTrees, lgIndexes := alis.BuildGeneratorAggregationTrees(c.fanout, segBlocks)
	c.current.LGAggTrees = lgTrees
	c.current.LGAggIndex = lgIndexes

	lidUniverse := alis.LIDUniverse(c.current.Summaries)
	lidKeysInOrder := make([]string, len(lidUniverse))
	bitmaps := map[string]bitmap.Bitmap{}
	aggRoots := map[string]bhash.Digest{}
	for i, l := range lidUniverse {
		key := alis.LIDKey(l)
		lidKeysInOrder[i] = key
		_, proof, ok := blockLevelStore.LG.Query(key)
		if !ok {
			return bhash.Digest{}, fmt.Errorf("chain: lid %d missing from block-level LG IIMT during aggregation", l)
		}
		bitmaps[key] = proof.Bitmap
		tree, ok := lgTrees[l]
		if !ok {
			return bhash.Digest{}, fmt.Errorf("chain: lid %d missing generator-aggregation tree", l)
		}
		aggRoots[key] = tree.Root()
	}
	c.current.AggLGIIMT = optimize.ExtendBlockLevelIIMTLeaves(c.fanout, lidKeysInOrder, bitmaps, aggRoots)
	return c.current.AggLGIIMT.Root(), nil
}

// SegmentForHeight returns the segment containing the given global
// BlockHeight, along with the segment-local height (D6).
func (c *Chain) SegmentForHeight(height uint64) (*Segment, uint64, error) {
	segIndex := height / c.segmentSize
	all := c.Segments()
	if segIndex >= uint64(len(all)) {
		return nil, 0, fmt.Errorf("chain: no segment for height %d", height)
	}
	seg := all[segIndex]
	return seg, height - seg.Base, nil
}
