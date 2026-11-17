package verify

import (
	"testing"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/omt"
	"blas/optimize"
	"blas/query"
)

type fixture struct {
	chain    *chain.Chain
	fn       *query.FullNode
	sp       *query.SP
	auditor  *Auditor
	registry map[uint64][]string
}

// buildFixture builds a 3-block chain: block 0 has lid=1 (keywords
// login/error), block 1 has lid=2 (login), block 2 has lid=1 again
// (login/error). One segment (segmentSize=1024, i.e. effectively
// unpartitioned).
func buildFixture(t *testing.T) fixture {
	t.Helper()
	c, err := chain.NewChain(2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}
	gens := map[uint64]*alis.LogGenerator{}

	type entrySpec struct {
		ts int64
		ew []string
	}
	build := func(lid, fid uint64, entries []entrySpec) alis.FileMetadata {
		gen, ok := gens[lid]
		if !ok {
			gen = alis.NewLogGenerator(lid)
			gens[lid] = gen
		}
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
		sp.Store(query.FileRef{LID: lid, FID: fid}, file)
		registry[lid] = gen.KeywordUniverse()
		return meta
	}

	m0 := build(1, 1, []entrySpec{{1000, []string{"login"}}, {1010, []string{"error"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m0}); err != nil {
		t.Fatal(err)
	}
	m1 := build(2, 1, []entrySpec{{2000, []string{"login"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m1}); err != nil {
		t.Fatal(err)
	}
	m2 := build(1, 2, []entrySpec{{3000, []string{"login", "error"}}, {3010, []string{"login"}}})
	if _, err := c.AddBlock([]alis.FileMetadata{m2}); err != nil {
		t.Fatal(err)
	}

	fn := query.NewFullNode(c)
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, b := range c.Blocks() {
		headers[i] = b.Header
	}
	auditor := NewAuditor(headers, 1024)

	return fixture{chain: c, fn: fn, sp: sp, auditor: auditor, registry: registry}
}

// TestHonestQueryAccepts: T-045-style — a fully honest run accepts and
// returns exactly the ground-truth entry set.
func TestHonestQueryAccepts(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	res := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if !res.Accept {
		t.Fatal("expected accept")
	}
	// lid=1's files: file1 (esn0 login, esn1 error), file2 (esn0
	// login+error, esn1 login). EK=login matches: file1/esn0, file2/esn0,
	// file2/esn1 = 3 entries.
	if len(res.Entries) != 3 {
		t.Fatalf("expected 3 matching entries, got %d: %+v", len(res.Entries), res.Entries)
	}
}

func TestPreparedResponseVerificationIsComplete(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	response, ok := f.auditor.PrepareResponse(f.fn, f.sp, f.registry, q, Options{})
	if !ok {
		t.Fatal("failed to prepare honest response")
	}
	if result := f.auditor.VerifyResponse(f.registry, q, response, Options{}); !result.Accept || len(result.Entries) != 3 {
		t.Fatalf("prepared response: accept=%v entries=%d", result.Accept, len(result.Entries))
	}
	if len(response.Files) < 2 {
		t.Fatalf("expected at least two file responses, got %d", len(response.Files))
	}
	omitted := response
	omitted.Files = append([]query.FileResult(nil), response.Files[:len(response.Files)-1]...)
	if f.auditor.VerifyResponse(f.registry, q, omitted, Options{}).Accept {
		t.Fatal("omitting a required file response was accepted")
	}
	duplicated := response
	duplicated.Files = append(append([]query.FileResult(nil), response.Files...), response.Files[0])
	if f.auditor.VerifyResponse(f.registry, q, duplicated, Options{}).Accept {
		t.Fatal("duplicating a file response was accepted")
	}
}

// TestQueryWithRangeAndFK exercises FK + time range together.
func TestQueryWithRangeAndFK(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 1, HasRange: true, TS: 2900, TE: 3500, FK: nil, EK: iomf.Keyword("error")}
	res := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if !res.Accept {
		t.Fatal("expected accept")
	}
	if len(res.Entries) != 1 {
		t.Fatalf("expected 1 entry (file2/esn0, ts=3000, has error), got %d: %+v", len(res.Entries), res.Entries)
	}
}

// TestNoTimeRangeMatchesEverything: G2 — a query with no time range keeps
// the LG/keyword predicates but skips the time predicate.
func TestNoTimeRangeMatchesEverything(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 2, EK: iomf.Keyword("login")}
	res := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if !res.Accept || len(res.Entries) != 1 {
		t.Fatalf("expected accept with 1 entry, got accept=%v entries=%d", res.Accept, len(res.Entries))
	}
}

// TestEmptyResultAccepted: G3/T-079 — a query matching nothing still
// accepts, with zero entries.
func TestEmptyResultAccepted(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 1, EK: iomf.Keyword("nonexistent-keyword")}
	res := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if !res.Accept {
		t.Fatal("expected accept even though nothing matches")
	}
	if len(res.Entries) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(res.Entries))
	}
}

// TestTamperedHeaderRejected: T-061-style — altering a header the auditor
// holds causes rejection.
func TestTamperedHeaderRejected(t *testing.T) {
	f := buildFixture(t)
	// Transaction-level roots are checked per-block (not per-segment-anchor
	// like the block-level roots), so corrupting block 0's own TxOMTRoot
	// is guaranteed to be on the verification path for a query naming lg=1
	// (which has a transaction in block 0).
	f.auditor.Headers[0].RH.TxOMTRoot[0] ^= 0xFF
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	res := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if res.Accept {
		t.Fatal("expected reject when a committed header root is corrupted")
	}
}

// TestMissingSPFileRejected: G1 — a missing required SP response is a
// reject.
func TestMissingSPFileRejected(t *testing.T) {
	f := buildFixture(t)
	// Remove SP's copy of file (1,1) but leave it referenced by the chain.
	emptySP := query.NewSP()
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	res := f.auditor.RunQuery(f.fn, emptySP, f.registry, q)
	if res.Accept {
		t.Fatal("expected reject when the SP cannot supply a required file")
	}
}

// TestEndToEndSoundness: T-041-style — SP-side tampering with returned
// entries must be rejected in each case.
func TestEndToEndSoundness(t *testing.T) {
	f := buildFixture(t)
	// file (lid=1, fid=2) has esn0={login,error} and esn1={login} only.
	// EK=error matches esn0 alone.
	qon, qoff := query.Split(query.Q{LG: 1, EK: iomf.Keyword("error")})

	s1, s2, err := f.fn.AnswerQOn(qon)
	if err != nil {
		t.Fatal(err)
	}
	candidates, ok := f.auditor.verifyStage1(qon, s1)
	if !ok {
		t.Fatal("expected stage1 to verify")
	}
	txs, ok := f.auditor.verifyStage2(qon, candidates, s2)
	if !ok {
		t.Fatal("expected stage2 to verify")
	}
	var target alis.Transaction
	for _, tx := range txs {
		if tx.FID == 2 {
			target = tx
		}
	}
	if target.FID != 2 {
		t.Fatal("expected transaction for fid=2")
	}
	ref := query.FileRef{LID: target.LID, FID: target.FID}
	results, err := f.sp.AnswerQOff([]query.FileRef{ref}, qoff)
	if err != nil {
		t.Fatal(err)
	}
	honestVO := results[0].VO
	if len(honestVO.ResultProofs) != 1 {
		t.Fatalf("expected exactly 1 matching entry (esn=0), got %d", len(honestVO.ResultProofs))
	}
	eq := alis.EntryQuery{EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE}
	if !alis.VerifyEntryQuery(target.RHIIMT, target.RHOMT, eq, honestVO) {
		t.Fatal("honest entry-level VO should verify")
	}

	// (a)/(b) non-satisfying entry injected: splice in esn=1's own proof
	// (which does NOT have "error") as if it also matched.
	file, ok := f.sp.File(ref)
	if !ok {
		t.Fatal("expected file to exist on SP")
	}
	esn1Proof, ok := file.OMT.Prove(1)
	if !ok {
		t.Fatal("expected esn=1 to exist")
	}
	forged := honestVO
	forged.ResultProofs = append(append([]omt.Proof{}, honestVO.ResultProofs...), esn1Proof)
	if alis.VerifyEntryQuery(target.RHIIMT, target.RHOMT, eq, forged) {
		t.Fatal("injecting a non-satisfying entry should be rejected")
	}

	// (c) forged entry: tamper the object bytes of the genuine result.
	tamperedObj := honestVO
	tamperedObj.ResultProofs = append([]omt.Proof{}, honestVO.ResultProofs...)
	tamperedObj.ResultProofs[0].Object = append([]byte{}, tamperedObj.ResultProofs[0].Object...)
	tamperedObj.ResultProofs[0].Object[0] ^= 0xFF
	if alis.VerifyEntryQuery(target.RHIIMT, target.RHOMT, eq, tamperedObj) {
		t.Fatal("a forged/tampered entry should be rejected")
	}
}

// TestEndToEndCompleteness: T-042-style.
func TestEndToEndCompleteness(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	qon, qoff := query.Split(q)

	// (a) honest.
	honest := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if !honest.Accept || len(honest.Entries) != 3 {
		t.Fatalf("expected honest accept with 3 entries, got accept=%v n=%d", honest.Accept, len(honest.Entries))
	}

	s1, s2, err := f.fn.AnswerQOn(qon)
	if err != nil {
		t.Fatal(err)
	}
	candidates, ok := f.auditor.verifyStage1(qon, s1)
	if !ok || len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %v ok=%v", candidates, ok)
	}

	// (c) full node omits one matching transaction within a block's VO.
	tamperedS2 := s2
	tamperedS2.Blocks = append([]query.BlockTxResult{}, s2.Blocks...)
	if len(tamperedS2.Blocks[0].VO.ResultProofs) == 0 {
		t.Fatal("expected block 0 to have a matching transaction")
	}
	tamperedS2.Blocks[0].VO.ResultProofs = tamperedS2.Blocks[0].VO.ResultProofs[:0]
	if _, ok := f.auditor.verifyStage2(qon, candidates, tamperedS2); ok {
		t.Fatal("omitting a matching transaction should be rejected")
	}

	// (d) full node omits an entire candidate block's answer.
	tamperedS2b := s2
	tamperedS2b.Blocks = s2.Blocks[:1]
	if _, ok := f.auditor.verifyStage2(qon, candidates, tamperedS2b); ok {
		t.Fatal("omitting a whole candidate block's answer should be rejected")
	}

	// (b) SP omits one matching entry from a file's VO.
	txs, ok := f.auditor.verifyStage2(qon, candidates, s2)
	if !ok {
		t.Fatal("expected stage2 to verify")
	}
	selected := filterByBmEW(txs, qoff.EK, f.registry)
	ref := query.FileRef{LID: selected[0].LID, FID: selected[0].FID}
	results, err := f.sp.AnswerQOff([]query.FileRef{ref}, qoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(results[0].VO.ResultProofs) == 0 {
		t.Fatal("expected at least one matching entry")
	}
	omitted := results[0].VO
	omitted.ResultProofs = omitted.ResultProofs[:0]
	eq := alis.EntryQuery{EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE}
	if alis.VerifyEntryQuery(selected[0].RHIIMT, selected[0].RHOMT, eq, omitted) {
		t.Fatal("SP omitting a matching entry should be rejected")
	}
}

// TestBmEWFilterConservativeNOT: T-043 — the auditor's bm_EW file filter
// uses the R-1 conservative expansion (NOT -> TRUE) since bm_EW is a
// coarse per-file summary, not the entry-level predicate itself.
func TestBmEWFilterConservativeNOT(t *testing.T) {
	f := buildFixture(t)
	q := query.Q{LG: 1, EK: iomf.Not(iomf.Keyword("nonexistent-keyword"))}
	qon, qoff := query.Split(q)

	s1, s2, err := f.fn.AnswerQOn(qon)
	if err != nil {
		t.Fatal(err)
	}
	candidates, ok := f.auditor.verifyStage1(qon, s1)
	if !ok {
		t.Fatal("expected stage1 to verify")
	}
	txs, ok := f.auditor.verifyStage2(qon, candidates, s2)
	if !ok {
		t.Fatal("expected stage2 to verify")
	}
	// Direct evalConservative check: a bare NOT of ANY keyword must
	// evaluate to true (conservative expansion), regardless of whether the
	// keyword actually occurs — this is what keeps completeness intact
	// for coarse per-file filtering (D-2/R-1).
	if !evalConservative(qoff.EK, func(string) bool { return false }) {
		t.Fatal("a bare NOT must conservatively evaluate to true")
	}

	// Consequently, filterByBmEW must not drop lid=1's files just because
	// they lack "nonexistent-keyword" — the exact NOT belongs at Stage 3
	// only.
	selected := filterByBmEW(txs, qoff.EK, f.registry)
	if len(selected) != len(txs) {
		t.Fatalf("conservative NOT should select every candidate file, got %d of %d", len(selected), len(txs))
	}

	// The full pipeline still ends up correct: Stage 3's own exact NOT (on
	// the real entry-keyword bitmaps) filters correctly, so every returned
	// entry actually satisfies EK.
	res := f.auditor.RunQuery(f.fn, f.sp, f.registry, q)
	if !res.Accept {
		t.Fatal("expected accept")
	}
	for _, e := range res.Entries {
		for _, w := range e.EW {
			if w == "nonexistent-keyword" {
				t.Fatal("returned entry should not contain the negated keyword")
			}
		}
	}
}

// buildMultiBlockAggregationFixture builds a chain with Optimization 2
// enabled, where lid=1 has one transaction in each of several blocks
// (a "multi-block hit set", T-052's own phrase) within one segment.
func buildMultiBlockAggregationFixture(t *testing.T, numBlocks int) fixture {
	t.Helper()
	c, err := chain.NewChainWithOptions(2, uint64(numBlocks+1), true) // one big segment
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	registry := map[uint64][]string{}
	gen := alis.NewLogGenerator(1)

	// Each block also carries several "filler" files from OTHER
	// generators, so each block's own transaction-level OMT has real
	// height (otherwise, with exactly one transaction per block, every
	// per-block OMT is a trivial single-leaf tree with a zero-length
	// proof, which would make the comparison meaningless in either
	// direction). This mirrors BLAS's real setting: many generators
	// contribute to each block, so one lid's own transactions are a small
	// fraction of a block's total.
	const fillerPerBlock = 15
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
		ref := query.FileRef{LID: 1, FID: uint64(i)}
		sp.Store(ref, file)
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
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, b := range c.Blocks() {
		headers[i] = b.Header
	}
	auditor := NewAuditor(headers, uint64(numBlocks+1))
	return fixture{chain: c, fn: fn, sp: sp, auditor: auditor, registry: registry}
}

// omtProofHashCount / aggProofHashCount approximate "hash values in the
// VO" (R-OPT-013's own metric) for a size comparison — this is a test
// -local measurement convention, not a claim about the wire format
// (GAP-38 remains unresolved).
func omtProofHashCount(p omt.Proof) int {
	n := 0
	for _, s := range p.Steps {
		n += len(s.SiblingHashes)
	}
	return n
}

func aggProofHashCount(p optimize.AggProof) int {
	n := 0
	for _, s := range p.Steps {
		n += len(s.SiblingHashes)
	}
	return n
}

// TestAggregationReducesVOSizeSameResults: T-052 — same query, same
// results/accept-reject, strictly fewer hash values with aggregation on,
// over a multi-block hit set.
func TestAggregationReducesVOSizeSameResults(t *testing.T) {
	f := buildMultiBlockAggregationFixture(t, 6)
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}

	plain := f.auditor.RunQueryWithOptions(f.fn, f.sp, f.registry, q, Options{UseAggregation: false})
	agg := f.auditor.RunQueryWithOptions(f.fn, f.sp, f.registry, q, Options{UseAggregation: true})

	if !plain.Accept || !agg.Accept {
		t.Fatalf("expected both to accept: plain=%v agg=%v", plain.Accept, agg.Accept)
	}
	if len(plain.Entries) != len(agg.Entries) || len(plain.Entries) != 6 {
		t.Fatalf("expected identical 6-entry result sets, got plain=%d agg=%d", len(plain.Entries), len(agg.Entries))
	}

	qon, _ := query.Split(q)
	plainS1, err := f.fn.Stage1(qon)
	if err != nil {
		t.Fatal(err)
	}
	plainS2, err := f.fn.Stage2(qon, plainS1.CandidateHeights)
	if err != nil {
		t.Fatal(err)
	}
	aggS2, _, err := f.fn.Stage2Aggregated(qon, plainS1)
	if err != nil {
		t.Fatal(err)
	}

	plainHashes := 0
	for _, br := range plainS2.Blocks {
		for _, p := range br.VO.LGProofs {
			plainHashes += omtProofHashCount(p)
		}
		for _, p := range br.VO.ResultProofs {
			plainHashes += omtProofHashCount(p)
		}
	}
	aggHashes := 0
	for _, br := range aggS2.Blocks {
		for _, p := range br.VO.LGProofs {
			aggHashes += omtProofHashCount(p) // LG neighbour/run evidence unchanged, see TRACKER.md scope note
		}
		for _, e := range br.VO.AggregatedResultTxs {
			aggHashes += aggProofHashCount(e.Proof)
		}
	}
	if aggHashes >= plainHashes {
		t.Fatalf("expected aggregation to reduce the matched-transaction hash count: plain=%d agg=%d", plainHashes, aggHashes)
	}
	t.Logf("plain=%d agg=%d hash values (result-transaction proofs only, 6 blocks)", plainHashes, aggHashes)
}

// TestAggregationSwitchingIdenticalResults: T-053 (aggregation dimension) —
// switching UseAggregation on/off with UseSegmentScan fixed produces
// identical result sets and accept/reject.
func TestAggregationSwitchingIdenticalResults(t *testing.T) {
	f := buildMultiBlockAggregationFixture(t, 4)
	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	variants := []Options{
		{UseSegmentScan: false, UseAggregation: false},
		{UseSegmentScan: false, UseAggregation: true},
		{UseSegmentScan: true, UseAggregation: false},
		{UseSegmentScan: true, UseAggregation: true},
	}
	var first Result
	for i, opts := range variants {
		res := f.auditor.RunQueryWithOptions(f.fn, f.sp, f.registry, q, opts)
		if i == 0 {
			first = res
			continue
		}
		if res.Accept != first.Accept {
			t.Fatalf("variant %+v: accept=%v, want %v", opts, res.Accept, first.Accept)
		}
		if len(res.Entries) != len(first.Entries) {
			t.Fatalf("variant %+v: %d entries, want %d", opts, len(res.Entries), len(first.Entries))
		}
	}
}

// TestForgedAggregationRootRejected: security regression — a forged
// aggregation root (or a leaf proof that doesn't match the claimed root)
// must be rejected, not silently accepted.
func TestForgedAggregationRootRejected(t *testing.T) {
	f := buildMultiBlockAggregationFixture(t, 3)
	qon := query.QOn{LG: 1}

	s1, err := f.fn.Stage1(qon)
	if err != nil {
		t.Fatal(err)
	}
	s2, bindings, err := f.fn.Stage2Aggregated(qon, s1)
	if err != nil {
		t.Fatal(err)
	}
	candidates, ok := f.auditor.verifyStage1(qon, s1)
	if !ok {
		t.Fatal("expected stage1 to verify")
	}
	if _, ok := f.auditor.verifyStage2Aggregated(qon, candidates, s2, bindings); !ok {
		t.Fatal("expected the honest aggregated stage2 to verify")
	}

	// Forge: replace one segment's claimed AggRoot with a bogus one.
	forgedBindings := map[uint64]query.AggregatedLGBinding{}
	for k, v := range bindings {
		v.AggRoot[0] ^= 0xFF
		forgedBindings[k] = v
	}
	if _, ok := f.auditor.verifyStage2Aggregated(qon, candidates, s2, forgedBindings); ok {
		t.Fatal("a forged aggregation root should be rejected")
	}

	// Forge: tamper one matched transaction's proof leaf hash directly.
	tamperedS2 := s2
	tamperedS2.Blocks = append([]query.BlockTxResult{}, s2.Blocks...)
	if len(tamperedS2.Blocks) > 0 && len(tamperedS2.Blocks[0].VO.AggregatedResultTxs) > 0 {
		tamperedS2.Blocks[0].VO.AggregatedResultTxs = append([]alis.AggregatedTxProof{}, tamperedS2.Blocks[0].VO.AggregatedResultTxs...)
		tamperedS2.Blocks[0].VO.AggregatedResultTxs[0].Proof.LeafHash[0] ^= 0xFF
		if _, ok := f.auditor.verifyStage2Aggregated(qon, candidates, tamperedS2, bindings); ok {
			t.Fatal("a tampered aggregated proof leaf hash should be rejected")
		}
	}
}

// TestRunQueryFromChain: T-066 closure — the on-chain keyword-set-file
// registry (R-FUNC-006/E4) is read directly from chain.Chain, not passed
// as a separately-managed side map, and produces the same result as the
// explicit-registry RunQuery.
func TestRunQueryFromChain(t *testing.T) {
	c, err := chain.NewChain(2, 1024)
	if err != nil {
		t.Fatal(err)
	}
	sp := query.NewSP()
	gen := alis.NewLogGenerator(1)
	b, err := alis.NewEntryFileBuilder(gen, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddEntry(1000, []string{"login"}, nil); err != nil {
		t.Fatal(err)
	}
	file, meta, err := b.Close()
	if err != nil {
		t.Fatal(err)
	}
	ref := query.FileRef{LID: 1, FID: 1}
	sp.Store(ref, file)
	c.RegisterKeywordUniverse(1, gen.KeywordUniverse()) // publish to the on-chain registry
	if _, err := c.AddBlock([]alis.FileMetadata{meta}); err != nil {
		t.Fatal(err)
	}

	fn := query.NewFullNode(c)
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, b := range c.Blocks() {
		headers[i] = b.Header
	}
	auditor := NewAuditor(headers, 1024)

	q := query.Q{LG: 1, EK: iomf.Keyword("login")}
	res := auditor.RunQueryFromChain(fn, sp, q)
	if !res.Accept || len(res.Entries) != 1 {
		t.Fatalf("expected accept with 1 entry via the on-chain registry, got accept=%v n=%d", res.Accept, len(res.Entries))
	}

	registry := fn.KeywordRegistry()
	if len(registry[1]) != 1 || registry[1][0] != "login" {
		t.Fatalf("expected on-chain registry to report lid=1's universe as [login], got %v", registry[1])
	}
}
