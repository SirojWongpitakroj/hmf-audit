// Optimization 2: on-chain index aggregation (VI, Fig. 5). This file
// implements the two per-segment aggregated MHTs (R-OPT-011/012) and the
// extended block-level IIMT leaf formula they deposit into (D-4 deviation).
//
// GAP-32 (explicitly left unresolved even after DECISIONS.md's rulings —
// see F6/R-OPT-013 in DECISIONS.md, and TRACKER.md's "Open gaps" section):
// which Stage-2 proofs an aggregated root is meant to REPLACE, and exactly
// how the verifier checks a query answer against an aggregated root instead
// of the independent per-block proofs it supersedes, is not specified by
// the paper and is not settled by any ruling. This file implements the
// aggregated structures themselves (construction, root, and a
// self-contained membership proof that a given block's bitmap/transaction
// is included in the aggregated root) and tests them in isolation
// (T-050/T-051-style). It does NOT wire aggregation into `query`/`verify`'s
// Stage 1/2 pipeline, because doing so would require inventing the missing
// GAP-32 mechanism rather than following a decided rule.
package optimize

import (
	"sort"

	"blas/bhash"
	"blas/bitmap"
)

// mtNode mirrors the small fixed-fanout Merkle level structure used
// elsewhere (omt/iimt/rimbt): level 0 is leaf hashes, each subsequent level
// groups the previous one into chunks of at most F, with one incomplete
// rightmost group permitted (A7).
type mtLevels [][]bhash.Digest

func buildLevels(fanout int, leafHashes []bhash.Digest) mtLevels {
	if len(leafHashes) == 0 {
		return nil
	}
	levels := mtLevels{leafHashes}
	cur := leafHashes
	for len(cur) > 1 {
		numGroups := (len(cur) + fanout - 1) / fanout
		next := make([]bhash.Digest, numGroups)
		for g := 0; g < numGroups; g++ {
			start := g * fanout
			end := start + fanout
			if end > len(cur) {
				end = len(cur)
			}
			parts := make([][]byte, 0, end-start)
			for i := start; i < end; i++ {
				parts = append(parts, bhash.DigestBytes(cur[i]))
			}
			next[g] = bhash.HashConcat(parts...)
		}
		levels = append(levels, next)
		cur = next
	}
	return levels
}

// AggProofStep mirrors omt.ProofStep/iimt.ProofStep: explicit sibling
// hashes plus position (R-GO-019).
type AggProofStep struct {
	ChildIndex    int
	SiblingHashes []bhash.Digest
}

// AggProof is a membership proof for one leaf of an aggregated MHT.
type AggProof struct {
	LeafIndex int
	LeafHash  bhash.Digest
	Steps     []AggProofStep
}

// AggregatedTree is one per-segment aggregated MHT (either a per-keyword
// bitmap aggregation or a per-lid transaction aggregation).
type AggregatedTree struct {
	fanout int
	levels mtLevels
}

// BuildAggregatedTree builds an aggregated MHT with fanout F over the given
// ordered leaf hashes (F5 in DECISIONS.md: keyword-MHT leaves ordered by
// BlockHeight; generator-MHT leaves ordered by (BlockHeight, tsn) — the
// caller is responsible for supplying leafHashes in that order).
func BuildAggregatedTree(fanout int, leafHashes []bhash.Digest) *AggregatedTree {
	return &AggregatedTree{fanout: fanout, levels: buildLevels(fanout, leafHashes)}
}

// Root returns the aggregated MHT's root, or the zero digest if empty.
func (t *AggregatedTree) Root() bhash.Digest {
	if len(t.levels) == 0 {
		return bhash.Digest{}
	}
	return t.levels[len(t.levels)-1][0]
}

// Prove returns a membership proof for the leaf at position idx.
func (t *AggregatedTree) Prove(idx int) (AggProof, bool) {
	if len(t.levels) == 0 || idx < 0 || idx >= len(t.levels[0]) {
		return AggProof{}, false
	}
	p := AggProof{LeafIndex: idx, LeafHash: t.levels[0][idx]}
	pos := idx
	for lvl := 0; lvl < len(t.levels)-1; lvl++ {
		cur := t.levels[lvl]
		groupStart := (pos / t.fanout) * t.fanout
		groupEnd := groupStart + t.fanout
		if groupEnd > len(cur) {
			groupEnd = len(cur)
		}
		step := AggProofStep{ChildIndex: pos - groupStart}
		for i := groupStart; i < groupEnd; i++ {
			if i == pos {
				continue
			}
			step.SiblingHashes = append(step.SiblingHashes, cur[i])
		}
		p.Steps = append(p.Steps, step)
		pos /= t.fanout
	}
	return p, true
}

// VerifyAggProof reconstructs the root from p and compares it with root.
func VerifyAggProof(root bhash.Digest, p AggProof) bool {
	cur := p.LeafHash
	for _, step := range p.Steps {
		total := len(step.SiblingHashes) + 1
		ordered := make([]bhash.Digest, total)
		si := 0
		for i := 0; i < total; i++ {
			if i == step.ChildIndex {
				ordered[i] = cur
			} else {
				ordered[i] = step.SiblingHashes[si]
				si++
			}
		}
		parts := make([][]byte, total)
		for i, h := range ordered {
			parts[i] = bhash.DigestBytes(h)
		}
		cur = bhash.HashConcat(parts...)
	}
	return cur == root
}

// KeywordBitmapLeafHash is the leaf hash for the per-keyword aggregation
// MHT (R-OPT-011): H(bm) for the block's bitmap for that keyword. The
// paper gives no formula for this leaf (only the generator-aggregation
// leaf is given, R-CRYPTO-008); H(bm) is adopted here as the natural,
// minimal extension, consistent with the "H(bm)" nested-digest convention
// already used as the inner hash inside every IIMT/RIMBT leaf elsewhere in
// this codebase — not a fresh, unrelated invention.
func KeywordBitmapLeafHash(bm bitmap.Bitmap) bhash.Digest {
	return bhash.H(bm.Bytes())
}

// GeneratorTxLeafHash is the leaf hash for the per-lid aggregation MHT
// (R-CRYPTO-008/F5): H(BlockHeight ‖ tsn ‖ H(tx)).
func GeneratorTxLeafHash(blockHeight, tsn uint64, txHash bhash.Digest) bhash.Digest {
	return bhash.HashConcat(bhash.Uint64BE(blockHeight), bhash.Uint64BE(tsn), bhash.DigestBytes(txHash))
}

// ExtendedIIMTLeafHash is the D-4 deviation: the block-level FK/LG IIMT
// leaf hash when Optimization 2 is active, H(w ‖ H(bm) ‖ RH_i), where RH_i
// is the aggregated MHT's root for that keyword/lid. The paper states the
// root is "deposited into" the leaf but never gives this formula; this is
// the documented interpretation (R-CRYPTO-003 extended with one more
// fixed-width field appended, keeping the framing unambiguous since the
// nested digest is fixed-width).
func ExtendedIIMTLeafHash(w string, bm bitmap.Bitmap, aggRoot bhash.Digest) bhash.Digest {
	inner := bhash.H(bm.Bytes())
	return bhash.HashConcat(bhash.FramedString(w), bhash.DigestBytes(inner), bhash.DigestBytes(aggRoot))
}

// KeywordAggregationInput is one keyword's ordered per-block bitmaps within
// a segment (R-OPT-011): BlockHeights and Bitmaps must be the same length
// and already sorted ascending by BlockHeight.
type KeywordAggregationInput struct {
	Keyword     string
	BlockHeight []uint64
	Bitmap      []bitmap.Bitmap
}

// BuildKeywordAggregation builds the per-keyword aggregated MHT for one
// segment (R-OPT-011): one MHT per keyword over that keyword's bitmaps
// across the segment's blocks, leaves ordered by BlockHeight.
func BuildKeywordAggregation(fanout int, in KeywordAggregationInput) *AggregatedTree {
	leaves := make([]bhash.Digest, len(in.Bitmap))
	for i, bm := range in.Bitmap {
		leaves[i] = KeywordBitmapLeafHash(bm)
	}
	return BuildAggregatedTree(fanout, leaves)
}

// GeneratorAggregationInput is one lid's ordered transactions within a
// segment (R-OPT-012), sorted ascending by (BlockHeight, tsn).
type GeneratorAggregationInput struct {
	LID         uint64
	BlockHeight []uint64
	TSN         []uint64
	TxHash      []bhash.Digest
}

// BuildGeneratorAggregation builds the per-lid aggregated MHT for one
// segment (R-OPT-012).
func BuildGeneratorAggregation(fanout int, in GeneratorAggregationInput) *AggregatedTree {
	leaves := make([]bhash.Digest, len(in.TxHash))
	for i := range in.TxHash {
		leaves[i] = GeneratorTxLeafHash(in.BlockHeight[i], in.TSN[i], in.TxHash[i])
	}
	return BuildAggregatedTree(fanout, leaves)
}

// ExtendBlockLevelIIMTLeaves rebuilds an IIMT with the D-4 extended leaf
// formula, depositing each keyword/lid's aggregated root into its own leaf
// (R-OPT-011/012's "deposit ... into the corresponding leaf"). keywords
// must be supplied in the SAME kid order as the original tree (append-only,
// C1); aggRoots gives each keyword's aggregated MHT root. This produces an
// INDEPENDENT tree (Optimization 2's extended form) rather than mutating
// the plain block-level IIMT in place, since the plain and extended forms
// have different leaf hash formulas and therefore different roots — a
// consumer chooses which one it wants (this is the "All"/"No-aggre"
// switch, R-OPT-015).
func ExtendBlockLevelIIMTLeaves(fanout int, keywordsInOrder []string, bitmaps map[string]bitmap.Bitmap, aggRoots map[string]bhash.Digest) *AggregatedTree {
	leaves := make([]bhash.Digest, len(keywordsInOrder))
	for i, w := range keywordsInOrder {
		leaves[i] = ExtendedIIMTLeafHash(w, bitmaps[w], aggRoots[w])
	}
	return BuildAggregatedTree(fanout, leaves)
}

// SortStringsStable is a small determinism helper (never rely on map
// iteration order, R-GO-006) used when assembling keyword lists for
// aggregation input construction.
func SortStringsStable(ss []string) []string {
	out := append([]string{}, ss...)
	sort.Strings(out)
	return out
}
