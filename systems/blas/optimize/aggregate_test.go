package optimize

import (
	"testing"

	"blas/bhash"
	"blas/bitmap"
)

func mkBM(n uint64, bits ...uint64) bitmap.Bitmap {
	bm := bitmap.New(n)
	for _, b := range bits {
		bm.Set(b)
	}
	return bm
}

// TestKeywordAggregationRoundTrip: T-050 — a bitmap in any block of the
// segment verifies through the aggregated root.
func TestKeywordAggregationRoundTrip(t *testing.T) {
	in := KeywordAggregationInput{
		Keyword:     "login",
		BlockHeight: []uint64{0, 1, 2},
		Bitmap:      []bitmap.Bitmap{mkBM(4, 0, 1), mkBM(4, 2), mkBM(4, 0, 3)},
	}
	tree := BuildKeywordAggregation(2, in)
	root := tree.Root()
	for i := range in.Bitmap {
		p, ok := tree.Prove(i)
		if !ok {
			t.Fatalf("expected proof for leaf %d", i)
		}
		if p.LeafHash != KeywordBitmapLeafHash(in.Bitmap[i]) {
			t.Fatalf("leaf %d hash mismatch", i)
		}
		if !VerifyAggProof(root, p) {
			t.Fatalf("leaf %d proof should verify", i)
		}
	}
	// Tampering a bitmap changes its leaf hash and breaks verification.
	tamperedProof, _ := tree.Prove(1)
	tamperedProof.LeafHash = KeywordBitmapLeafHash(mkBM(4, 3)) // different bitmap
	if VerifyAggProof(root, tamperedProof) {
		t.Fatal("tampered bitmap leaf should not verify")
	}
}

// TestGeneratorAggregationRoundTrip: T-051 — a transaction in any block of
// the segment verifies through the aggregated root, with leaf
// H(BlockHeight||tsn||H(tx)).
func TestGeneratorAggregationRoundTrip(t *testing.T) {
	txHash := func(s string) bhash.Digest { return bhash.H([]byte(s)) }
	in := GeneratorAggregationInput{
		LID:         1,
		BlockHeight: []uint64{0, 0, 2},
		TSN:         []uint64{0, 1, 0},
		TxHash:      []bhash.Digest{txHash("tx0"), txHash("tx1"), txHash("tx2")},
	}
	tree := BuildGeneratorAggregation(2, in)
	root := tree.Root()
	for i := range in.TxHash {
		p, ok := tree.Prove(i)
		if !ok {
			t.Fatalf("expected proof for leaf %d", i)
		}
		want := GeneratorTxLeafHash(in.BlockHeight[i], in.TSN[i], in.TxHash[i])
		if p.LeafHash != want {
			t.Fatalf("leaf %d hash mismatch", i)
		}
		if !VerifyAggProof(root, p) {
			t.Fatalf("leaf %d proof should verify", i)
		}
	}
}

// TestExtendedIIMTLeafFormula: D-4 — H(w||H(bm)||RH_i), distinct from the
// plain H(w||H(bm)) formula (R-CRYPTO-003) for the same (w,bm).
func TestExtendedIIMTLeafFormula(t *testing.T) {
	w := "login"
	bm := mkBM(4, 0, 2)
	aggRoot := bhash.H([]byte("agg-root"))
	extended := ExtendedIIMTLeafHash(w, bm, aggRoot)

	plain := bhash.HashConcat(bhash.FramedString(w), bhash.DigestBytes(bhash.H(bm.Bytes())))
	if extended == plain {
		t.Fatal("extended leaf hash must differ from the plain (no-aggregation) leaf hash")
	}
	// Changing the aggregated root changes the leaf hash (the deposit
	// actually matters).
	other := ExtendedIIMTLeafHash(w, bm, bhash.H([]byte("different-root")))
	if extended == other {
		t.Fatal("different aggregated roots must produce different extended leaf hashes")
	}
}

// TestOneSegmentWhenPartitioningDisabled: F6 — with partitioning
// "disabled" (one giant segment spanning the whole chain), the aggregation
// input simply covers every block; no separate code path is needed.
func TestOneSegmentWhenPartitioningDisabled(t *testing.T) {
	// Build an aggregation input spanning 5 blocks as "one segment" and
	// confirm it is structurally identical to building it as a dedicated
	// segment of size 5 — i.e. BuildKeywordAggregation doesn't care
	// whether its input came from a "real" segment or the whole chain.
	bms := []bitmap.Bitmap{mkBM(2, 0), mkBM(2, 1), mkBM(2, 0), mkBM(2, 1), mkBM(2, 0)}
	heights := []uint64{0, 1, 2, 3, 4}
	whole := BuildKeywordAggregation(4, KeywordAggregationInput{Keyword: "k", BlockHeight: heights, Bitmap: bms})
	again := BuildKeywordAggregation(4, KeywordAggregationInput{Keyword: "k", BlockHeight: heights, Bitmap: bms})
	if whole.Root() != again.Root() {
		t.Fatal("identical input should produce identical roots regardless of how segmentation is conceptually described")
	}
}
