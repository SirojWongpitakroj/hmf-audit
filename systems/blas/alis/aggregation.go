package alis

import "blas/optimize"

// BlockTxs pairs a block's global height with its transactions, for
// building Optimization-2 per-segment generator-aggregation trees
// (VI, R-OPT-012). Callers supply blocks in ascending BlockHeight order;
// each block's own Txs must already be tsn-ordered (as BuildTransactionLevel
// always produces).
type BlockTxs struct {
	Height uint64
	Txs    []Transaction
}

// TxAggKey identifies one transaction within a per-lid generator
// -aggregation tree by (BlockHeight, TSN) — the natural key since a
// transaction's aggregated-MHT leaf position depends on both.
type TxAggKey struct {
	Height uint64
	TSN    uint64
}

// BuildGeneratorAggregationTrees builds, for every lid appearing across
// blocks, the per-lid generator-aggregation MHT (R-OPT-012): one MHT over
// ALL of that lid's transactions across the given blocks, leaves ordered by
// (BlockHeight, tsn) (F5) — which falls out for free from blocks already
// being supplied in ascending height order with each block's own
// transactions in tsn order. Also returns, per lid, an index from
// (BlockHeight, tsn) to that transaction's leaf position, needed to produce
// a membership proof for a specific matched transaction later.
//
// Building each lid's tree only from that lid's own entries (never from
// iterating the intermediate per-lid map in hash-input order) keeps this
// deterministic regardless of Go map iteration (R-GO-006): the per-lid
// entries slices are populated in blocks/Txs order, not map order, and
// each resulting tree depends only on its own entries.
func BuildGeneratorAggregationTrees(fanout int, blocks []BlockTxs) (map[uint64]*optimize.AggregatedTree, map[uint64]map[TxAggKey]int) {
	type entry struct {
		height uint64
		tx     Transaction
	}
	byLID := map[uint64][]entry{}
	for _, b := range blocks {
		for _, tx := range b.Txs {
			byLID[tx.LID] = append(byLID[tx.LID], entry{height: b.Height, tx: tx})
		}
	}

	trees := map[uint64]*optimize.AggregatedTree{}
	indexes := map[uint64]map[TxAggKey]int{}
	for lid, entries := range byLID {
		input := optimize.GeneratorAggregationInput{LID: lid}
		idx := map[TxAggKey]int{}
		for i, e := range entries {
			input.BlockHeight = append(input.BlockHeight, e.height)
			input.TSN = append(input.TSN, e.tx.TSN)
			input.TxHash = append(input.TxHash, HashTransaction(e.tx))
			idx[TxAggKey{Height: e.height, TSN: e.tx.TSN}] = i
		}
		trees[lid] = optimize.BuildGeneratorAggregation(fanout, input)
		indexes[lid] = idx
	}
	return trees, indexes
}
