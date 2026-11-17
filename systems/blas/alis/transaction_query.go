package alis

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/iomf"
	"blas/omt"
	"blas/optimize"
	"blas/rimbt"
)

// DecodeTransaction reverses EncodeTransaction. Used only by the verifier
// to inspect a returned transaction's lid/tsn (needed for the LG
// completeness check, R-QUERY-015/R-VER-009) — never to change what is
// hashed.
func DecodeTransaction(data []byte) (Transaction, error) {
	const fixedPrefix = 8 + SigWidth + 8 + 8 + 8 + 8
	if len(data) < fixedPrefix+4 {
		return Transaction{}, fmt.Errorf("alis: transaction encoding too short")
	}
	off := 0
	tsn := binary.BigEndian.Uint64(data[off : off+8])
	off += 8
	sig := append([]byte{}, data[off:off+SigWidth]...)
	off += SigWidth
	lid := binary.BigEndian.Uint64(data[off : off+8])
	off += 8
	fid := binary.BigEndian.Uint64(data[off : off+8])
	off += 8
	tsMin := int64(binary.BigEndian.Uint64(data[off : off+8]))
	off += 8
	tsMax := int64(binary.BigEndian.Uint64(data[off : off+8]))
	off += 8
	numFW := binary.BigEndian.Uint32(data[off : off+4])
	off += 4
	fw := make([]string, numFW)
	for i := uint32(0); i < numFW; i++ {
		if len(data) < off+4 {
			return Transaction{}, fmt.Errorf("alis: truncated FW length prefix")
		}
		l := binary.BigEndian.Uint32(data[off : off+4])
		off += 4
		if len(data) < off+int(l) {
			return Transaction{}, fmt.Errorf("alis: truncated FW content")
		}
		fw[i] = string(data[off : off+int(l)])
		off += int(l)
	}
	if len(data) < off+8 {
		return Transaction{}, fmt.Errorf("alis: truncated BmEW length field")
	}
	bmN := binary.BigEndian.Uint64(data[off : off+8])
	off += 8
	if len(data) < off+4 {
		return Transaction{}, fmt.Errorf("alis: truncated BmEW byte-length prefix")
	}
	bmByteLen := binary.BigEndian.Uint32(data[off : off+4])
	off += 4
	if len(data) < off+int(bmByteLen) {
		return Transaction{}, fmt.Errorf("alis: truncated BmEW content")
	}
	bm, err := bitmap.FromBytes(bmN, data[off:off+int(bmByteLen)])
	if err != nil {
		return Transaction{}, err
	}
	off += int(bmByteLen)
	if len(data) < off+32+32 {
		return Transaction{}, fmt.Errorf("alis: truncated RH digests")
	}
	var rhIIMT, rhOMT bhash.Digest
	copy(rhIIMT[:], data[off:off+32])
	off += 32
	copy(rhOMT[:], data[off:off+32])

	return Transaction{
		TSN: tsn, Sig: sig, LID: lid, FID: fid, TsMin: tsMin, TsMax: tsMax,
		FW: fw, BmEW: bm, RHIIMT: rhIIMT, RHOMT: rhOMT,
	}, nil
}

// TxQuery is Q_on's per-block share at the transaction level
// (R-QUERY-014..016): an LG identifier, an optional file-keyword
// expression, and an optional time range.
type TxQuery struct {
	LG       uint64
	FK       *iomf.Expr
	HasRange bool
	TS, TE   int64
}

// TxLevelVO is the transaction-level verification object.
type TxLevelVO struct {
	LGProofs        []omt.Proof // all of lg's transactions in this block, via the OMT
	LGLeftNeighbor  *omt.Proof  // nil iff lg's run starts at tsn=0
	LGRightNeighbor *omt.Proof  // nil iff lg's run ends at the last tsn

	FKEvidence       []iomf.KeywordEvidence
	FKAbsentKeywords []string
	FKAbsenceProofs  []iimt.AbsenceProof

	HasRange   bool
	TsMinRange rimbt.RangeResult
	TsMaxRange rimbt.RangeResult

	ResultProofs []omt.Proof

	// AggregatedResultTxs is populated INSTEAD OF ResultProofs when
	// Optimization 2 is active (GAP-32 closure, F6: aggregation replaces
	// the per-block OMT membership proofs it supersedes — see
	// ProcessTxLevelQueryAggregated/TRACKER.md for the exact, documented
	// scope of what is and is not replaced).
	AggregatedResultTxs []AggregatedTxProof
}

// AggregatedTxProof is one matched transaction's Optimization-2 membership
// proof: instead of an OMT proof against this block's own transaction
// -level root, a proof against the segment-wide per-lid generator
// -aggregation root (R-OPT-012), whose leaf is H(BlockHeight‖tsn‖H(tx))
// (R-CRYPTO-008/F5).
type AggregatedTxProof struct {
	BlockHeight uint64
	Transaction Transaction
	Proof       optimize.AggProof
}

// ProcessTxLevelQuery implements R-QUERY-014..016: the LG sub-query is a
// contiguous-run lookup (transactions are sorted by (lid,fid), so lg's
// transactions are contiguous in tsn, R-ALIS-012) with left/right neighbour
// completeness evidence (R-QUERY-015), rather than an IIMT bitmap lookup —
// the transaction level has no LG IIMT (R-ALIS-010 lists only a file-keyword
// IIMT at this level).
func ProcessTxLevelQuery(store *iomf.Store, txs []Transaction, q TxQuery) (TxLevelVO, error) {
	return ProcessTxLevelReaders(txReaders(store), txs, q)
}
func ProcessTxLevelReaders(store TxReaders, txs []Transaction, q TxQuery) (TxLevelVO, error) {
	n := uint64(len(txs))
	var vo TxLevelVO
	vo.HasRange = q.HasRange

	lo := sort.Search(len(txs), func(i int) bool { return txs[i].LID >= q.LG })
	hi := sort.Search(len(txs), func(i int) bool { return txs[i].LID > q.LG })
	lgBM := bitmap.New(n)
	for i := lo; i < hi; i++ {
		lgBM.Set(uint64(i))
		p, ok := store.OMT.Prove(uint64(i))
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing OMT proof for tsn %d", i)
		}
		vo.LGProofs = append(vo.LGProofs, p)
	}
	if lo > 0 {
		p, ok := store.OMT.Prove(uint64(lo - 1))
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing left-neighbour OMT proof")
		}
		vo.LGLeftNeighbor = &p
	}
	if hi < len(txs) {
		p, ok := store.OMT.Prove(uint64(hi))
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing right-neighbour OMT proof")
		}
		vo.LGRightNeighbor = &p
	}

	fkBM, fkEvidence, fkAbsent, fkAbsence, err := computeFKBitmap(store, n, q.FK)
	if err != nil {
		return TxLevelVO{}, err
	}
	vo.FKEvidence = fkEvidence
	vo.FKAbsentKeywords = fkAbsent
	vo.FKAbsenceProofs = fkAbsence

	rangeBM, tsMinRange, tsMaxRange, err := computeTxRangeBitmap(store, n, q.HasRange, q.TS, q.TE)
	if err != nil {
		return TxLevelVO{}, err
	}
	vo.TsMinRange = tsMinRange
	vo.TsMaxRange = tsMaxRange

	finalBM := bitmap.And(bitmap.And(lgBM, fkBM), rangeBM)
	for _, tsn := range finalBM.SetIndices() {
		p, ok := store.OMT.Prove(tsn)
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing OMT proof for result tsn %d", tsn)
		}
		vo.ResultProofs = append(vo.ResultProofs, p)
	}
	return vo, nil
}

// ProcessTxLevelQueryAggregated is ProcessTxLevelQuery's Optimization-2
// counterpart (GAP-32 closure): the LG contiguous-run/neighbour evidence
// and the FK/range sub-query evidence are computed IDENTICALLY (matching
// and their own completeness are unaffected by aggregation — F6 does not
// change what is computed, only how the final matched transactions are
// PROVEN genuine). What changes is the LAST step: instead of one OMT proof
// per matched transaction against this block's own transaction-level root,
// each matched transaction gets one proof against the segment-wide,
// shared per-lid generator-aggregation tree (aggTree), looked up via
// aggIndex (TxAggKey{height, tsn} -> leaf position, built by
// alis.BuildGeneratorAggregationTrees). ResultProofs is left empty;
// AggregatedResultTxs is populated instead (R-OPT-013: aggregation
// replaces the proofs it supersedes, not all of them — LGProofs/neighbours
// still use the per-block OMT, since completeness of "these are exactly
// lg's transactions in this block" is not something a per-lid aggregation
// tree can establish on its own; see TRACKER.md for the full reasoning).
func ProcessTxLevelQueryAggregated(store *iomf.Store, txs []Transaction, height uint64, aggTree *optimize.AggregatedTree, aggIndex map[TxAggKey]int, q TxQuery) (TxLevelVO, error) {
	n := uint64(len(txs))
	var vo TxLevelVO
	vo.HasRange = q.HasRange

	lo := sort.Search(len(txs), func(i int) bool { return txs[i].LID >= q.LG })
	hi := sort.Search(len(txs), func(i int) bool { return txs[i].LID > q.LG })
	lgBM := bitmap.New(n)
	for i := lo; i < hi; i++ {
		lgBM.Set(uint64(i))
	}
	if lo > 0 {
		p, ok := store.OMT.Prove(uint64(lo - 1))
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing left-neighbour OMT proof")
		}
		vo.LGLeftNeighbor = &p
	}
	if hi < len(txs) {
		p, ok := store.OMT.Prove(uint64(hi))
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing right-neighbour OMT proof")
		}
		vo.LGRightNeighbor = &p
	}
	for i := lo; i < hi; i++ {
		p, ok := store.OMT.Prove(uint64(i))
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: missing OMT proof for tsn %d", i)
		}
		vo.LGProofs = append(vo.LGProofs, p)
	}

	fkBM, fkEvidence, fkAbsent, fkAbsence, err := computeFKBitmap(txReaders(store), n, q.FK)
	if err != nil {
		return TxLevelVO{}, err
	}
	vo.FKEvidence = fkEvidence
	vo.FKAbsentKeywords = fkAbsent
	vo.FKAbsenceProofs = fkAbsence

	rangeBM, tsMinRange, tsMaxRange, err := computeTxRangeBitmap(txReaders(store), n, q.HasRange, q.TS, q.TE)
	if err != nil {
		return TxLevelVO{}, err
	}
	vo.TsMinRange = tsMinRange
	vo.TsMaxRange = tsMaxRange

	finalBM := bitmap.And(bitmap.And(lgBM, fkBM), rangeBM)
	for _, tsn := range finalBM.SetIndices() {
		idx, ok := aggIndex[TxAggKey{Height: height, TSN: tsn}]
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: no aggregation index entry for (height=%d, tsn=%d)", height, tsn)
		}
		proof, ok := aggTree.Prove(idx)
		if !ok {
			return TxLevelVO{}, fmt.Errorf("alis: no aggregated proof at index %d", idx)
		}
		vo.AggregatedResultTxs = append(vo.AggregatedResultTxs, AggregatedTxProof{
			BlockHeight: height, Transaction: txs[tsn], Proof: proof,
		})
	}
	return vo, nil
}

// computeFKBitmap evaluates a file-keyword expression against store's FK
// IIMT (or returns all-ones if fk is nil), applying the disclosed
// absent-leaf treatment documented on alis.ProcessBlockLevelQuery. Shared
// between the plain per-block path (ProcessTxLevelQuery) and the
// Optimization-2 aggregated path (ProcessTxLevelQueryAggregatedInputs),
// since keyword MATCHING is unaffected by aggregation — only how the
// resulting membership is PROVEN changes (F6/R-OPT-011).
func computeFKBitmap(store TxReaders, n uint64, fk *iomf.Expr) (bitmap.Bitmap, []iomf.KeywordEvidence, []string, []iimt.AbsenceProof, error) {
	if fk == nil {
		return bitmap.Not(bitmap.New(n)), nil, nil, nil, nil
	}
	var evidence []iomf.KeywordEvidence
	var absent []string
	var absenceProofs []iimt.AbsenceProof
	bmLookup := map[string]bitmap.Bitmap{}
	for _, w := range iomf.Keywords(fk) {
		_, proof, ok := store.IIMT.Query(w)
		if !ok {
			absent = append(absent, w)
			absence, _ := store.IIMT.ProveAbsent(w)
			absenceProofs = append(absenceProofs, absence)
			bmLookup[w] = bitmap.New(n)
			continue
		}
		evidence = append(evidence, iomf.KeywordEvidence{Keyword: w, Proof: proof})
		bmLookup[w] = proof.Bitmap
	}
	fkBM := iomf.Evaluate(fk, func(w string) bitmap.Bitmap { return bmLookup[w] })
	return fkBM, evidence, absent, absenceProofs, nil
}

// computeTxRangeBitmap evaluates the transaction-level time-range predicate
// (R-ALIS-015/R-QUERY-012 pattern reused at this level) via the two
// unbounded-side RIMBT range queries. Shared with the Optimization-2
// aggregated path for the same reason as computeFKBitmap.
func computeTxRangeBitmap(store TxReaders, n uint64, hasRange bool, ts, te int64) (bitmap.Bitmap, rimbt.RangeResult, rimbt.RangeResult, error) {
	if !hasRange {
		return bitmap.Not(bitmap.New(n)), rimbt.RangeResult{}, rimbt.RangeResult{}, nil
	}
	maxRes, err := store.RIMBTs["ts_max"].RangeQuery(ts, math.MaxInt64)
	if err != nil {
		return bitmap.Bitmap{}, rimbt.RangeResult{}, rimbt.RangeResult{}, err
	}
	minRes, err := store.RIMBTs["ts_min"].RangeQuery(math.MinInt64, te)
	if err != nil {
		return bitmap.Bitmap{}, rimbt.RangeResult{}, rimbt.RangeResult{}, err
	}
	maxBM := bitmap.New(n)
	for _, h := range maxRes.QR {
		maxBM.Set(h)
	}
	minBM := bitmap.New(n)
	for _, h := range minRes.QR {
		minBM.Set(h)
	}
	return bitmap.And(maxBM, minBM), minRes, maxRes, nil
}

// TxLevelRoots are the committed transaction-level roots for one block.
type TxLevelRoots struct {
	OMTRoot   bhash.Digest
	FKRoot    bhash.Digest
	TsMinRoot bhash.Digest
	TsMaxRoot bhash.Digest
}

// VerifyTxLevelQuery implements the transaction-level share of R-VER-009:
// integrity of the OMT/IIMT/RIMBT evidence, LG-run completeness via the
// neighbour proofs (T-037: a neighbour that itself belongs to lg is
// rejected), and correctness via independent bitmap recomputation
// (R-VER-005 discipline).
func VerifyTxLevelQuery(rh TxLevelRoots, n uint64, q TxQuery, vo TxLevelVO) bool {
	expected, ok := verifyTxLevelCommon(rh, n, q, vo)
	if !ok {
		return false
	}
	if len(expected) != len(vo.ResultProofs) {
		return false
	}
	resultTsns := make([]uint64, len(vo.ResultProofs))
	for i, p := range vo.ResultProofs {
		if !omt.Verify(rh.OMTRoot, p) {
			return false
		}
		resultTsns[i] = p.OID
	}
	sort.Slice(resultTsns, func(i, j int) bool { return resultTsns[i] < resultTsns[j] })
	for i := range expected {
		if expected[i] != resultTsns[i] {
			return false
		}
	}
	return true
}

// VerifyTxLevelQueryAggregated is VerifyTxLevelQuery's Optimization-2
// counterpart (GAP-32 closure): LG/FK/range are verified identically
// (verifyTxLevelCommon), but the final matched-transaction set is checked
// via AggregatedResultTxs against the segment-wide aggRoot instead of
// ResultProofs against rh.OMTRoot. blockHeight pins each proof to the block
// actually being verified, preventing a malicious full node from
// attributing a genuine transaction from a DIFFERENT block in the same
// segment (same lid, same aggregated tree) to this one.
func VerifyTxLevelQueryAggregated(rh TxLevelRoots, n uint64, q TxQuery, vo TxLevelVO, aggRoot bhash.Digest, blockHeight uint64) bool {
	expected, ok := verifyTxLevelCommon(rh, n, q, vo)
	if !ok {
		return false
	}
	if len(expected) != len(vo.AggregatedResultTxs) {
		return false
	}
	resultTsns := make([]uint64, len(vo.AggregatedResultTxs))
	for i, e := range vo.AggregatedResultTxs {
		if e.BlockHeight != blockHeight {
			return false
		}
		if e.Transaction.LID != q.LG {
			return false
		}
		txHash := HashTransaction(e.Transaction)
		expectedLeaf := optimize.GeneratorTxLeafHash(e.BlockHeight, e.Transaction.TSN, txHash)
		if e.Proof.LeafHash != expectedLeaf {
			return false
		}
		if !optimize.VerifyAggProof(aggRoot, e.Proof) {
			return false
		}
		resultTsns[i] = e.Transaction.TSN
	}
	sort.Slice(resultTsns, func(i, j int) bool { return resultTsns[i] < resultTsns[j] })
	for i := range expected {
		if expected[i] != resultTsns[i] {
			return false
		}
	}
	return true
}

// verifyTxLevelCommon verifies the LG contiguous-run completeness, FK
// evidence, and range evidence shared by both VerifyTxLevelQuery and
// VerifyTxLevelQueryAggregated, returning the independently-recomputed
// expected matching tsn set (R-VER-005 discipline) on success.
func verifyTxLevelCommon(rh TxLevelRoots, n uint64, q TxQuery, vo TxLevelVO) ([]uint64, bool) {
	lgTsns := make([]uint64, len(vo.LGProofs))
	for i, p := range vo.LGProofs {
		if !omt.Verify(rh.OMTRoot, p) {
			return nil, false
		}
		tx, err := DecodeTransaction(p.Object)
		if err != nil || tx.LID != q.LG || tx.TSN != p.OID {
			return nil, false
		}
		lgTsns[i] = p.OID
	}
	sort.Slice(lgTsns, func(i, j int) bool { return lgTsns[i] < lgTsns[j] })
	for i := 1; i < len(lgTsns); i++ {
		if lgTsns[i] != lgTsns[i-1]+1 {
			return nil, false // lg's run must be contiguous
		}
	}

	var lo, hi uint64
	if len(lgTsns) == 0 {
		// No transactions for lg in this block: both neighbours (if
		// present) must bracket an empty run, i.e. be adjacent to each
		// other or to a domain edge; lo/hi left at 0 is fine since the
		// loop below over an empty lgBM contributes nothing.
	} else {
		lo, hi = lgTsns[0], lgTsns[len(lgTsns)-1]+1
	}

	if vo.LGLeftNeighbor != nil {
		if !omt.Verify(rh.OMTRoot, *vo.LGLeftNeighbor) {
			return nil, false
		}
		lt, err := DecodeTransaction(vo.LGLeftNeighbor.Object)
		if err != nil || lt.LID == q.LG {
			return nil, false // a real neighbour must NOT belong to lg
		}
		if len(lgTsns) > 0 && vo.LGLeftNeighbor.OID != lo-1 {
			return nil, false
		}
	} else if len(lgTsns) > 0 && lo != 0 {
		return nil, false // missing left neighbour but lg's run doesn't start at 0
	}
	if vo.LGRightNeighbor != nil {
		if !omt.Verify(rh.OMTRoot, *vo.LGRightNeighbor) {
			return nil, false
		}
		rt, err := DecodeTransaction(vo.LGRightNeighbor.Object)
		if err != nil || rt.LID == q.LG {
			return nil, false
		}
		if len(lgTsns) > 0 && vo.LGRightNeighbor.OID != hi {
			return nil, false
		}
	} else if len(lgTsns) > 0 && hi != n {
		return nil, false // missing right neighbour but lg's run doesn't end at the last tsn
	}

	lgBM := bitmap.New(n)
	for _, tsn := range lgTsns {
		lgBM.Set(tsn)
	}

	for _, ke := range vo.FKEvidence {
		if ke.Keyword != ke.Proof.Keyword || ke.Proof.Bitmap.Len() != n || !iimt.Verify(rh.FKRoot, ke.Proof) {
			return nil, false
		}
	}
	if len(vo.FKAbsentKeywords) != len(vo.FKAbsenceProofs) {
		return nil, false
	}
	for i, w := range vo.FKAbsentKeywords {
		if !iimt.VerifyAbsent(rh.FKRoot, n, w, vo.FKAbsenceProofs[i]) {
			return nil, false
		}
	}
	fkBM := bitmap.Not(bitmap.New(n))
	if q.FK != nil {
		bmLookup := map[string]bitmap.Bitmap{}
		for _, ke := range vo.FKEvidence {
			bmLookup[ke.Keyword] = ke.Proof.Bitmap
		}
		for _, w := range vo.FKAbsentKeywords {
			bmLookup[w] = bitmap.New(n)
		}
		for _, w := range iomf.Keywords(q.FK) {
			if _, ok := bmLookup[w]; !ok {
				return nil, false
			}
		}
		fkBM = iomf.Evaluate(q.FK, func(w string) bitmap.Bitmap { return bmLookup[w] })
	}

	rangeBM := bitmap.Not(bitmap.New(n))
	if q.HasRange {
		if !rimbt.VerifyRangeResult(rh.TsMaxRoot, n, q.TS, math.MaxInt64, vo.TsMaxRange) {
			return nil, false
		}
		if !rimbt.VerifyRangeResult(rh.TsMinRoot, n, math.MinInt64, q.TE, vo.TsMinRange) {
			return nil, false
		}
		maxBM, ok := recomputeRangeBitmap(n, vo.TsMaxRange)
		if !ok {
			return nil, false
		}
		minBM, ok := recomputeRangeBitmap(n, vo.TsMinRange)
		if !ok {
			return nil, false
		}
		rangeBM = bitmap.And(maxBM, minBM)
	}

	expectedBM := bitmap.And(bitmap.And(lgBM, fkBM), rangeBM)
	return expectedBM.SetIndices(), true
}
