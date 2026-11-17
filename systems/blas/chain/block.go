// Package chain implements a deterministic, single-process blockchain
// emulator (E1 in DECISIONS.md): no consensus protocol, no networking, no
// real blockchain platform. It models exactly the on-chain state BLAS
// requires: blocks with headers, transaction-level and block-level indexes,
// and the block-generator role (a full node acting as block generator,
// §1.3/R-FUNC-008).
package chain

import (
	"blas/bhash"
)

// RootHashSet is RH: an ordered set of eight roots — four transaction-level
// roots then four block-level roots (E2 in DECISIONS.md), reflecting state
// after inclusion of the current block. The within-level order follows the
// order each tree is introduced for that level in the SRS (R-ALIS-010 for
// the transaction level: OMT, FK IIMT, ts_min RIMBT, ts_max RIMBT;
// R-ALIS-018..021 for the block level: LG IIMT, FK IIMT, FileTsMin RIMBT,
// FileTsMax RIMBT). The paper fixes the count (E2) but not this ordering;
// this is one fixed, documented, deterministic choice, not a re-openable
// default.
type RootHashSet struct {
	TxOMTRoot          bhash.Digest
	TxFKRoot           bhash.Digest
	TxTsMinRoot        bhash.Digest
	TxTsMaxRoot        bhash.Digest
	BlockLGRoot        bhash.Digest
	BlockFKRoot        bhash.Digest
	BlockFileTsMinRoot bhash.Digest
	BlockFileTsMaxRoot bhash.Digest
}

// BlockHeader is the V-B/Fig. 3 block header, always carrying the Opt. 1
// fields (VI): PastTsMax, BlockTsMax, BlockTsMin. Partitioning "disabled" is
// modeled as an effectively unbounded segment size (F6-style: a single
// degenerate parameter setting rather than a second header shape), so these
// three fields are always well-defined and the header encoding never
// changes shape between the partitioned and unpartitioned cases — only the
// values differ (T-053: identical results/accept-reject, differing only in
// what the values are used for at query time).
type BlockHeader struct {
	BlockHeight   uint64
	PrevBlockHash bhash.Digest
	FileTsMin     int64
	FileTsMax     int64
	TxNum         uint64
	RH            RootHashSet

	// Opt. 1 fields (VI):
	PastTsMax  int64 // max FileTsMax of all blocks up to and including this one
	BlockTsMax int64 // max FileTsMax of the blocks in the current segment, up to and including this one
	BlockTsMin int64 // min FileTsMin of the blocks in the current segment, up to and including this one

	// Opt. 2 field (VI, D-4 deviation; GAP-32 closure — see chain.go's
	// aggregation-building code and TRACKER.md for the concrete, documented
	// mechanism chosen). This is the root of the SEGMENT's aggregated block
	// -level LG IIMT (extended leaves H(w‖H(bm)‖RH_i), R-OPT-012) as of this
	// block. Always present in the header shape (zero digest when
	// Optimization 2 is disabled) for the same "uniform shape, degenerate
	// value" reason as the Opt. 1 fields above.
	AggBlockLGRoot bhash.Digest
}

// EncodeBlockHeader serializes every declared field, in the order given
// above, for H(block) (A5 in DECISIONS.md, extended to the block header —
// resolves GAP-24's block-hash-construction gap the same way GAP-4 was
// resolved for transactions/objects: hash every field, declared order,
// fixed-width integers, raw digests).
func EncodeBlockHeader(h BlockHeader) []byte {
	out := bhash.Uint64BE(h.BlockHeight)
	out = append(out, bhash.DigestBytes(h.PrevBlockHash)...)
	out = append(out, bhash.Int64BE(h.FileTsMin)...)
	out = append(out, bhash.Int64BE(h.FileTsMax)...)
	out = append(out, bhash.Uint64BE(h.TxNum)...)
	out = append(out, bhash.DigestBytes(h.RH.TxOMTRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.TxFKRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.TxTsMinRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.TxTsMaxRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.BlockLGRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.BlockFKRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.BlockFileTsMinRoot)...)
	out = append(out, bhash.DigestBytes(h.RH.BlockFileTsMaxRoot)...)
	out = append(out, bhash.Int64BE(h.PastTsMax)...)
	out = append(out, bhash.Int64BE(h.BlockTsMax)...)
	out = append(out, bhash.Int64BE(h.BlockTsMin)...)
	out = append(out, bhash.DigestBytes(h.AggBlockLGRoot)...)
	return out
}

// HashBlockHeader returns H(block) — used as the NEXT block's
// PrevBlockHash (V-B: "PrevBlockHash records the hash value of the previous
// block"). Hashing the header alone (rather than header+body) is the
// natural reading: the header's RH already commits to the entire body
// (every transaction, via the transaction-level OMT root), so hashing the
// header transitively commits to the body — the standard header-commits-
// to-body pattern, and the only reading consistent with "root hash set of
// on-chain index ... stored in the block header" already representing the
// body's content.
func HashBlockHeader(h BlockHeader) bhash.Digest {
	return bhash.H(EncodeBlockHeader(h))
}
