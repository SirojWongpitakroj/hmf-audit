package eval

import (
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/query"
	"blas/verify"
)

// TestAggregationReducesHarnessVOSize is a harness-level regression for a
// real bug caught during independent verification: eval.RunOnceWithOptions
// computed VOSizeBytes from the plain (non-aggregated) Stage1/Stage2 answer
// regardless of Options.UseAggregation, so the ablation harness reported
// byte-identical VO sizes for the "All" (aggregation on) and "No-aggre"
// variants — silently hiding Optimization 2's entire effect from any
// benchmark run.
//
// The fixture mirrors verify.buildMultiBlockAggregationFixture (one
// generator's transactions spread across several blocks, each block padded
// with filler transactions from OTHER generators so its own
// transaction-level OMT has real height — with only one transaction per
// block, the per-block OMT proof is already trivial and the comparison
// would be meaningless in either direction, per that helper's own doc
// comment), but drives it through the eval harness's own
// RunOnceWithOptions/VOSize accounting instead of calling verify directly,
// so a regression here specifically catches the harness's wiring rather
// than the already-tested underlying mechanism.
//
// fillerPerBlock is deliberately large relative to numBlocks: aggregation's
// net saving is (numBlocks matched-tx proofs shortened from per-block-OMT
// depth to aggregation-tree depth) MINUS (one binding proof, paid once per
// segment, whose own depth grows with the total number of distinct lids
// the block-level LG IIMT has ever seen). That saving only shows up in the
// TOTAL VO size once each block is large enough that the per-block-OMT
// depth it avoids outweighs the binding proof's one-time cost — a small
// fixture (e.g. ~15 filler txs/block at 6 blocks, enough to demonstrate the
// underlying per-tx hash-count reduction in verify's own unit test) can
// still net LARGER thanks to that fixed binding cost, which is exactly why
// this harness-level regression uses a bigger block size than that test.
func TestAggregationReducesHarnessVOSize(t *testing.T) {
	const numBlocks = 8
	const fillerPerBlock = 127

	c, err := chain.NewChainWithOptions(2, uint64(numBlocks+1), true) // one big segment
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}
	gen := alis.NewLogGenerator(1)

	for i := 0; i < numBlocks; i++ {
		b, err := alis.NewEntryFileBuilder(gen, uint64(i), 2)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := b.AddEntry(int64(1000+i*10), []string{"login"}, nil); err != nil {
			t.Fatal(err)
		}
		file, meta, err := b.Close()
		if err != nil {
			t.Fatal(err)
		}
		sp.Store(query.FileRef{LID: 1, FID: uint64(i)}, file)
		registry[1] = gen.KeywordUniverse()

		metas := []alis.FileMetadata{meta}
		for j := 0; j < fillerPerBlock; j++ {
			fillerLID := uint64(1000 + i*fillerPerBlock + j) // distinct filler generator per file
			fillerGen := alis.NewLogGenerator(fillerLID)
			fb, err := alis.NewEntryFileBuilder(fillerGen, 0, 2)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fb.AddEntry(int64(1000+i*10), []string{"filler"}, nil); err != nil {
				t.Fatal(err)
			}
			ffile, fmeta, err := fb.Close()
			if err != nil {
				t.Fatal(err)
			}
			sp.Store(query.FileRef{LID: fillerLID, FID: 0}, ffile)
			metas = append(metas, fmeta)
		}
		if _, err := c.AddBlock(metas); err != nil {
			t.Fatal(err)
		}
	}

	fn := query.NewFullNode(c)
	auditor := verify.NewAuditor(fn.Headers(), c.SegmentSize())
	q := query.Q{LG: 1} // lid 1's entire run: no FK/EK/range predicate

	plain := RunOnceWithOptions(fn, sp, auditor, registry, q, QAnd, verify.Options{UseAggregation: false})
	agg := RunOnceWithOptions(fn, sp, auditor, registry, q, QAnd, verify.Options{UseAggregation: true})

	if !plain.Accepted || !agg.Accepted {
		t.Fatalf("expected both to accept: plain=%v agg=%v", plain.Accepted, agg.Accepted)
	}
	if plain.ResultEntries != agg.ResultEntries || plain.ResultEntries != numBlocks {
		t.Fatalf("expected identical %d-entry result sets (T-053: query-time switches never change results), got plain=%d agg=%d", numBlocks, plain.ResultEntries, agg.ResultEntries)
	}
	if agg.VOSizeBytes >= plain.VOSizeBytes {
		t.Fatalf("expected aggregation to strictly reduce VO size over a %d-block hit set, got plain=%d agg=%d bytes (equal/larger means the harness is not measuring the aggregated path)", numBlocks, plain.VOSizeBytes, agg.VOSizeBytes)
	}
	t.Logf("plain=%d agg=%d VO bytes over %d blocks (%d filler txs/block)", plain.VOSizeBytes, agg.VOSizeBytes, numBlocks, fillerPerBlock)
}
