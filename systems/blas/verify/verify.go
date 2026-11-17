// Package verify wires the per-level verification built in `iomf`/`alis`
// into the full three-stage accept/reject pipeline the auditor runs (§9,
// V-E, Fig. 4), including:
//
//   - G5 (DECISIONS.md §G): a full node's query response is untrusted —
//     every block-level and transaction-level VO is independently
//     re-derived and checked against header state the auditor already
//     holds, exactly like the SP's entry-level VO (R-SEC-001).
//   - G1: Stage 1/2 evidence is verified BEFORE the auditor trusts bm_EW to
//     decide which fids to request from the SP; the auditor sends the
//     selected fids plus the already-known lg and Q_off (text over the
//     Fig. 4 figure label).
//   - G2: a query with no time range skips the time predicate and keeps
//     the LG/keyword sub-queries — this falls out for free, since every
//     Process*/Verify* function already treats HasRange=false as "match
//     everything" at that predicate.
//   - G3: an authenticated empty answer is accepted as complete — also
//     falls out for free, since every Verify* function recomputes the
//     expected result independently rather than trusting a claimed result
//     set.
package verify

import (
	"sort"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/optimize"
	"blas/query"
)

// Auditor is the log auditor: a blockchain light node that stores only
// block header information (R-FUNC-015). SegmentSize must be the same
// public protocol parameter S the chain was built with (R-GO-027: not a
// hard-coded constant; the caller supplies it, exactly as it supplies the
// chain's own construction parameter).
type Auditor struct {
	Headers       []chain.BlockHeader
	SegmentSize   uint64
	storedHeaders int
	headerReader  func(uint64) (chain.BlockHeader, error)
	headerError   error
}

// NewAuditor copies the given headers (a light node syncs header data
// only — R-FUNC-015, R-VER-013) and records S.
func NewAuditor(headers []chain.BlockHeader, segmentSize uint64) *Auditor {
	h := append([]chain.BlockHeader{}, headers...)
	return &Auditor{Headers: h, SegmentSize: segmentSize}
}

// verifyStage1 re-derives the global candidate BlockHeight set from the
// full node's per-segment VOs, checking full segment coverage (no segment
// silently skipped, T-048) and every segment's evidence against the
// auditor's own header state (R-VER-008).
func (a *Auditor) verifyStage1(qon query.QOn, s1 query.Stage1Result) ([]uint64, bool) {
	numSegments := optimize.SegmentCount(a.headerCount(), a.SegmentSize)
	all := make([]uint64, numSegments)
	for i := range all {
		all[i] = uint64(i)
	}
	return a.verifyStage1Segments(qon, s1, all)
}

// verifyStage1Optimized implements the R-OPT-008/F3 coverage-consistency
// check: the auditor performs its own backward scan and requires EQUALITY
// with the segment indices the full node's answer actually covers, before
// verifying each of those segments exactly as verifyStage1 does (T-048: a
// full node that silently skips an intersecting segment is rejected here,
// since the equality check fails).
func (a *Auditor) verifyStage1Optimized(qon query.QOn, s1 query.Stage1Result) ([]uint64, bool) {
	if a.headerReader != nil {
		return a.verifyStage1Segments(qon, s1, a.storedIntersecting(qon))
	}
	expected := optimize.IntersectingSegments(a.headerTimes(), a.SegmentSize, qon.HasRange, qon.TS, qon.TE)
	return a.verifyStage1Segments(qon, s1, expected)
}

// headerTimes projects the Opt. 1 header fields optimize.IntersectingSegments
// needs (see optimize/partition.go's doc comment on why `optimize` cannot
// import `blas/chain` directly).
func (a *Auditor) headerTimes() []optimize.HeaderTimes {
	out := make([]optimize.HeaderTimes, a.headerCount())
	for i, h := range a.Headers {
		out[i] = optimize.HeaderTimes{BlockTsMin: h.BlockTsMin, BlockTsMax: h.BlockTsMax, PastTsMax: h.PastTsMax}
	}
	return out
}

// verifyStage1Segments verifies s1 against exactly the given expected
// segment indices (ascending), rejecting on any mismatch in coverage or on
// any segment's own evidence.
func (a *Auditor) verifyStage1Segments(qon query.QOn, s1 query.Stage1Result, expectedIndices []uint64) ([]uint64, bool) {
	if len(s1.Segments) != len(expectedIndices) {
		return nil, false
	}
	var candidates []uint64
	for i, seg := range s1.Segments {
		segIndex := expectedIndices[i]
		expectedBase := segIndex * a.SegmentSize
		if seg.SegmentBase != expectedBase {
			return nil, false
		}
		expectedAnchor := optimize.AnchorHeight(segIndex, a.SegmentSize, a.headerCount())
		if seg.AnchorHeight != expectedAnchor {
			return nil, false
		}
		n := expectedAnchor - expectedBase + 1
		header := a.headerAt(expectedAnchor)
		rh := alis.BlockLevelRoots{
			LGRoot: header.RH.BlockLGRoot, FKRoot: header.RH.BlockFKRoot,
			FileTsMinRoot: header.RH.BlockFileTsMinRoot, FileTsMaxRoot: header.RH.BlockFileTsMaxRoot,
		}
		blq := alis.BlockLevelQuery{LG: qon.LG, FK: qon.FK, HasRange: qon.HasRange, TS: qon.TS, TE: qon.TE}
		if !alis.VerifyBlockLevelQuery(rh, n, blq, seg.VO) {
			return nil, false
		}
		for _, h := range seg.VO.CandidateHeights {
			candidates = append(candidates, expectedBase+h)
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i] < candidates[j] })
	return candidates, true
}

// verifyStage2 checks that the full node answered for exactly the verified
// candidate set (no omission, T-042(d)) and that every block's
// transaction-level evidence checks out (R-VER-009), returning the
// authenticated matching transactions.
func (a *Auditor) verifyStage2(qon query.QOn, candidates []uint64, s2 query.Stage2Result) ([]alis.Transaction, bool) {
	if len(s2.Blocks) != len(candidates) {
		return nil, false
	}
	var out []alis.Transaction
	for i, br := range s2.Blocks {
		if br.Height != candidates[i] {
			return nil, false
		}
		header := a.headerAt(br.Height)
		rh := alis.TxLevelRoots{
			OMTRoot: header.RH.TxOMTRoot, FKRoot: header.RH.TxFKRoot,
			TsMinRoot: header.RH.TxTsMinRoot, TsMaxRoot: header.RH.TxTsMaxRoot,
		}
		txq := alis.TxQuery{LG: qon.LG, FK: qon.FK, HasRange: qon.HasRange, TS: qon.TS, TE: qon.TE}
		if !alis.VerifyTxLevelQuery(rh, header.TxNum, txq, br.VO) {
			return nil, false
		}
		for _, p := range br.VO.ResultProofs {
			tx, err := alis.DecodeTransaction(p.Object)
			if err != nil || tx.TSN != p.OID {
				return nil, false
			}
			out = append(out, tx)
		}
	}
	return out, true
}

// verifyStage2Aggregated is verifyStage2's Optimization-2 counterpart
// (GAP-32 closure): for each candidate block, it first checks that block's
// segment has a valid LG aggregation binding — a proof that the
// block-level LG IIMT's extended leaf H(w‖H(bm)‖RH_i) for lg is genuinely
// committed in that segment's AggBlockLGRoot (read from the segment's
// anchor header, which the auditor already trusts) — then verifies the
// block's FK/range/matched-transaction evidence via
// alis.VerifyTxLevelQueryAggregated against that segment's aggregated root,
// instead of the block's own transaction-level OMT root.
//
// The leaf-hash equality check below is what ties binding.Bitmap to the
// SAME bitmap Stage 1 already verified for this segment: the extended leaf
// hash is a function of (lid key, bitmap, aggregated root), so a binding
// using any other bitmap value could not reproduce the leaf hash actually
// committed inside AggBlockLGRoot at construction time — no separate
// cross-check against Stage 1's own bitmap is needed.
func (a *Auditor) verifyStage2Aggregated(qon query.QOn, candidates []uint64, s2 query.Stage2Result, bindings map[uint64]query.AggregatedLGBinding) ([]alis.Transaction, bool) {
	if len(s2.Blocks) != len(candidates) {
		return nil, false
	}
	verifiedSegments := map[uint64]bool{}
	var out []alis.Transaction
	for i, br := range s2.Blocks {
		if br.Height != candidates[i] {
			return nil, false
		}
		segIndex := br.Height / a.SegmentSize
		binding, ok := bindings[segIndex]
		if !ok || binding.LID != qon.LG {
			return nil, false
		}
		if !verifiedSegments[segIndex] {
			anchor := optimize.AnchorHeight(segIndex, a.SegmentSize, a.headerCount())
			anchorHeader := a.headerAt(anchor)
			expectedLeaf := optimize.ExtendedIIMTLeafHash(alis.LIDKey(qon.LG), binding.Bitmap, binding.AggRoot)
			if expectedLeaf != binding.LeafProof.LeafHash {
				return nil, false
			}
			if !optimize.VerifyAggProof(anchorHeader.AggBlockLGRoot, binding.LeafProof) {
				return nil, false
			}
			verifiedSegments[segIndex] = true
		}

		header := a.headerAt(br.Height)
		rh := alis.TxLevelRoots{
			OMTRoot: header.RH.TxOMTRoot, FKRoot: header.RH.TxFKRoot,
			TsMinRoot: header.RH.TxTsMinRoot, TsMaxRoot: header.RH.TxTsMaxRoot,
		}
		txq := alis.TxQuery{LG: qon.LG, FK: qon.FK, HasRange: qon.HasRange, TS: qon.TS, TE: qon.TE}
		if !alis.VerifyTxLevelQueryAggregated(rh, header.TxNum, txq, br.VO, binding.AggRoot, br.Height) {
			return nil, false
		}
		for _, e := range br.VO.AggregatedResultTxs {
			out = append(out, e.Transaction)
		}
	}
	return out, true
}

// evalConservative implements the R-1 residual (DECISIONS.md §C/D-2): the
// standard monotone over-approximation — every negative literal becomes
// TRUE, then the remaining monotone expression is evaluated normally.
func evalConservative(e *iomf.Expr, has func(string) bool) bool {
	switch e.Kind {
	case iomf.ExprKeyword:
		return has(e.Keyword)
	case iomf.ExprNot:
		return true
	case iomf.ExprAnd:
		return evalConservative(e.Left, has) && evalConservative(e.Right, has)
	case iomf.ExprOr:
		return evalConservative(e.Left, has) || evalConservative(e.Right, has)
	default:
		return false
	}
}

// filterByBmEW implements R-QUERY-019/R-VER-014: the auditor's own bm_EW
// file filter, applied to Stage-2-verified transactions AFTER Stage 1/2
// verification (G1), using the conservative NOT expansion (R-1) since
// bm_EW is a coarse per-file summary, not the entry-level predicate itself
// (exact NOT is applied only at Stage 3, inside alis.VerifyEntryQuery's own
// EK evaluation on the real entry-keyword IIMT bitmaps).
//
// keywordUniverse supplies, per lid, that generator's entry-keyword
// universe (E4 in DECISIONS.md: on-chain keyword-set-file metadata/
// registry, not an additional authenticated data structure — so this is
// read directly, not proved). Because the universe is append-only, a
// file's bm_EW (built with N = universe size as of that file's close) is
// correctly interpreted using the first N entries of the CURRENT universe.
func filterByBmEW(txs []alis.Transaction, ek *iomf.Expr, keywordUniverse map[uint64][]string) []alis.Transaction {
	if ek == nil {
		return txs
	}
	var out []alis.Transaction
	for _, tx := range txs {
		universe := keywordUniverse[tx.LID]
		n := tx.BmEW.Len()
		has := func(w string) bool {
			for kid := uint64(0); kid < n && int(kid) < len(universe); kid++ {
				if universe[kid] == w {
					return tx.BmEW.Get(kid)
				}
			}
			return false
		}
		if evalConservative(ek, has) {
			out = append(out, tx)
		}
	}
	return out
}

// Result is the outcome of a full three-stage audited query.
type Result struct {
	Entries []alis.LogEntry
	Accept  bool
}

// Options selects which of the two BLAS optimizations (VI) are exercised
// for one query (R-OPT-015: independently switchable). The zero value runs
// the unoptimized "All sub-queries visit every segment" baseline; neither
// option changes the final result or accept/reject (T-053) — only which
// evidence path is taken and, in a real system, VO size/CPU time.
type Options struct {
	UseSegmentScan bool // Optimization 1 (§10.1): backward scan restricted to intersecting segments
	UseAggregation bool // Optimization 2 (§10.2): reserved for M11
}

// RunQuery implements the full Fig. 4 pipeline with default Options (both
// optimizations off).
func (a *Auditor) RunQuery(fn *query.FullNode, sp *query.SP, keywordUniverse map[uint64][]string, q query.Q) Result {
	return a.RunQueryWithOptions(fn, sp, keywordUniverse, q, Options{})
}

// RunQueryFromChain is RunQuery, but reads the bm_EW keyword registry
// directly from the full node's chain (R-FUNC-006/E4's on-chain keyword-set
// -file, chain.Chain.KeywordRegistry) instead of requiring the caller to
// separately track and pass it — the more faithful modeling of "the
// auditor reads on-chain metadata," addressing the T-066 note that an
// explicitly-passed map was a modeling simplification. RunQuery/
// RunQueryWithOptions remain available for callers (e.g. tests) that want
// to supply a specific registry snapshot directly.
func (a *Auditor) RunQueryFromChain(fn *query.FullNode, sp *query.SP, q query.Q) Result {
	return a.RunQueryWithOptions(fn, sp, fn.KeywordRegistry(), q, Options{})
}

// RunQueryFromChainWithOptions is RunQueryFromChain plus Options.
func (a *Auditor) RunQueryFromChainWithOptions(fn *query.FullNode, sp *query.SP, q query.Q, opts Options) Result {
	return a.RunQueryWithOptions(fn, sp, fn.KeywordRegistry(), q, opts)
}

// RunQueryWithOptions implements the full Fig. 4 pipeline: Q_on to the full
// node, Stage 1+2 verification, the auditor's bm_EW filter, Q_off to the SP
// for the selected files, Stage 3 verification, and the final accept/reject
// (R-VER-011: accept iff every verification output is true). Any internal
// failure — a verification mismatch or a protocol-level error such as a
// missing SP file — is folded into a single reject, never a distinct error
// outcome (R-VER-012, R-GO-025).
func (a *Auditor) RunQueryWithOptions(fn *query.FullNode, sp *query.SP, keywordUniverse map[uint64][]string, q query.Q, opts Options) Result {
	qon, qoff := query.Split(q)

	var s1 query.Stage1Result
	var err error
	if opts.UseSegmentScan {
		s1, err = fn.Stage1Optimized(qon)
	} else {
		s1, err = fn.Stage1(qon)
	}
	if err != nil {
		return Result{Accept: false}
	}
	var candidates []uint64
	var ok bool
	if opts.UseSegmentScan {
		candidates, ok = a.verifyStage1Optimized(qon, s1)
	} else {
		candidates, ok = a.verifyStage1(qon, s1)
	}
	if !ok {
		return Result{Accept: false}
	}

	var txs []alis.Transaction
	if opts.UseAggregation {
		s2, bindings, err := fn.Stage2Aggregated(qon, s1)
		if err != nil {
			return Result{Accept: false}
		}
		txs, ok = a.verifyStage2Aggregated(qon, candidates, s2, bindings)
	} else {
		var s2 query.Stage2Result
		s2, err = fn.Stage2(qon, s1.CandidateHeights)
		if err != nil {
			return Result{Accept: false}
		}
		txs, ok = a.verifyStage2(qon, candidates, s2)
	}
	if !ok {
		return Result{Accept: false}
	}

	selected := filterByBmEW(txs, qoff.EK, keywordUniverse)
	byRef := map[query.FileRef]alis.Transaction{}
	var refs []query.FileRef
	for _, tx := range selected {
		ref := query.FileRef{LID: tx.LID, FID: tx.FID}
		byRef[ref] = tx
		refs = append(refs, ref)
	}

	fileResults, err := sp.AnswerQOff(refs, qoff)
	if err != nil {
		return Result{Accept: false} // G1: a missing required SP response is a reject
	}

	var entries []alis.LogEntry
	for _, fr := range fileResults {
		tx, ok := byRef[fr.Ref]
		if !ok {
			return Result{Accept: false}
		}
		eq := alis.EntryQuery{EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE}
		if !alis.VerifyEntryQuery(tx.RHIIMT, tx.RHOMT, eq, fr.VO) {
			return Result{Accept: false}
		}
		for _, p := range fr.VO.ResultProofs {
			e, err := alis.DecodeLogEntry(p.Object)
			if err != nil {
				return Result{Accept: false}
			}
			entries = append(entries, e)
		}
	}

	return Result{Entries: entries, Accept: true}
}
