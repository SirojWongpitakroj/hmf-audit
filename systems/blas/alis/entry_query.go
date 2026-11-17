package alis

import (
	"encoding/binary"
	"fmt"
	"sort"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/iomf"
	"blas/omt"
)

// DecodeLogEntry reverses EncodeLogEntry. Used only by the verifier to
// inspect a returned entry's ts (needed to check time-range completeness
// against boundary evidence, R-VER-010) — never to change what is hashed.
func DecodeLogEntry(data []byte) (LogEntry, error) {
	if len(data) < 8+8+4 {
		return LogEntry{}, fmt.Errorf("alis: log entry encoding too short")
	}
	esn := binary.BigEndian.Uint64(data[0:8])
	ts := int64(binary.BigEndian.Uint64(data[8:16]))
	off := 16
	numEW := binary.BigEndian.Uint32(data[off : off+4])
	off += 4
	ew := make([]string, numEW)
	for i := uint32(0); i < numEW; i++ {
		if len(data) < off+4 {
			return LogEntry{}, fmt.Errorf("alis: truncated EW length prefix")
		}
		l := binary.BigEndian.Uint32(data[off : off+4])
		off += 4
		if len(data) < off+int(l) {
			return LogEntry{}, fmt.Errorf("alis: truncated EW content")
		}
		ew[i] = string(data[off : off+int(l)])
		off += int(l)
	}
	if len(data) < off+4 {
		return LogEntry{}, fmt.Errorf("alis: truncated V length prefix")
	}
	vLen := binary.BigEndian.Uint32(data[off : off+4])
	off += 4
	if len(data) < off+int(vLen) {
		return LogEntry{}, fmt.Errorf("alis: truncated V content")
	}
	v := append([]byte{}, data[off:off+int(vLen)]...)
	return LogEntry{ESN: esn, TS: ts, EW: ew, V: v}, nil
}

// EntryQuery is Q_off's per-file share: an entry-keyword expression and an
// optional time range (R-QUERY-021).
type EntryQuery struct {
	EK       *iomf.Expr
	HasRange bool
	TS, TE   int64
}

// EntryVO is the entry-level verification object (R-QUERY-022, R-ALIS-007,
// D2 in DECISIONS.md): matching full entries with a merged OMT proof, the
// entry-keyword bitmaps/proof, and the temporal boundary OMT proofs.
type EntryVO struct {
	KeywordEvidence  []iomf.KeywordEvidence
	EKAbsentKeywords []string
	EKAbsenceProofs  []iimt.AbsenceProof
	HasRange         bool
	LowerBoundary    *omt.Proof // nil iff the range starts at esn=0
	UpperBoundary    *omt.Proof // nil iff the range runs to the last esn
	ResultProofs     []omt.Proof

	// ExtentProof authenticates N, the file's entry count, as the OMT
	// leaf for the largest esn (N-1) — C5 in DECISIONS.md: N must be
	// derived from the authenticated object population (here, OMT extent/
	// rightmost-leaf evidence), never an unauthenticated field. The
	// auditor has no other on-chain source for a file's entry count, so
	// this is always included.
	ExtentProof omt.Proof
}

// ProcessQuery implements the per-file share of R-QUERY-021/022.
func (f *EntryFile) ProcessQuery(q EntryQuery) (EntryVO, error) {
	return ProcessEntryReaders(EntryReaders{Count: uint64(len(f.Entries)), IIMT: f.IIMT, OMT: f.OMT, TimeRangeLookup: f.TimeRangeLookup}, q)
}
func ProcessEntryReaders(f EntryReaders, q EntryQuery) (EntryVO, error) {
	n := f.Count
	var vo EntryVO
	vo.HasRange = q.HasRange

	bmLookup := map[string]bitmap.Bitmap{}
	if q.EK != nil {
		for _, w := range iomf.Keywords(q.EK) {
			_, proof, ok := f.IIMT.Query(w)
			if !ok {
				// This file's entry-keyword universe never saw keyword w
				// (it was first introduced by the generator in some other,
				// e.g. later, file — C2 only guarantees a leaf for
				// keywords known AT THIS FILE's close time, not every
				// keyword the generator will ever learn). Treated as an
				// all-zero contribution backed by a complete-tree
				// non-membership witness.
				vo.EKAbsentKeywords = append(vo.EKAbsentKeywords, w)
				absence, _ := f.IIMT.ProveAbsent(w)
				vo.EKAbsenceProofs = append(vo.EKAbsenceProofs, absence)
				bmLookup[w] = bitmap.New(n)
				continue
			}
			vo.KeywordEvidence = append(vo.KeywordEvidence, iomf.KeywordEvidence{Keyword: w, Proof: proof})
			bmLookup[w] = proof.Bitmap
		}
	}
	var ekBM bitmap.Bitmap
	if q.EK != nil {
		ekBM = iomf.Evaluate(q.EK, func(w string) bitmap.Bitmap { return bmLookup[w] })
	} else {
		ekBM = bitmap.Not(bitmap.New(n))
	}

	var rangeBM bitmap.Bitmap
	if q.HasRange {
		esns, lower, upper, err := f.TimeRangeLookup(q.TS, q.TE)
		if err != nil {
			return EntryVO{}, err
		}
		vo.LowerBoundary = lower
		vo.UpperBoundary = upper
		rangeBM = bitmap.New(n)
		for _, esn := range esns {
			rangeBM.Set(esn)
		}
	} else {
		rangeBM = bitmap.Not(bitmap.New(n))
	}

	final := bitmap.And(ekBM, rangeBM)
	for _, esn := range final.SetIndices() {
		proof, ok := f.OMT.Prove(esn)
		if !ok {
			return EntryVO{}, fmt.Errorf("alis: esn %d has no OMT leaf", esn)
		}
		vo.ResultProofs = append(vo.ResultProofs, proof)
	}
	extentProof, ok := f.OMT.Prove(n - 1)
	if !ok {
		return EntryVO{}, fmt.Errorf("alis: missing extent proof for esn %d", n-1)
	}
	vo.ExtentProof = extentProof
	return vo, nil
}

// VerifyEntryQuery implements R-VER-010: integrity of bitmaps/entries
// against the (Stage-2-verified) entry-level roots, consistency of the
// result with EK, and time-range completeness via the boundary proofs.
func VerifyEntryQuery(iimtRoot, omtRoot bhash.Digest, q EntryQuery, vo EntryVO) bool {
	// Derive N from the authenticated OMT extent (C5 in DECISIONS.md): the
	// rightmost leaf's oid, plus one, is the file's entry count. Both the
	// proof itself AND its rightmost-position shape must check out before
	// N is trusted for anything else (otherwise a shorter, still-valid
	// proof for some smaller esn could understate N).
	if !omt.Verify(omtRoot, vo.ExtentProof) || !vo.ExtentProof.IsRightmost() {
		return false
	}
	n := vo.ExtentProof.OID + 1

	for _, ke := range vo.KeywordEvidence {
		if ke.Proof.Bitmap.Len() != n {
			return false
		}
		if ke.Keyword != ke.Proof.Keyword || !iimt.Verify(iimtRoot, ke.Proof) {
			return false
		}
	}
	if len(vo.EKAbsentKeywords) != len(vo.EKAbsenceProofs) {
		return false
	}
	for i, w := range vo.EKAbsentKeywords {
		if !iimt.VerifyAbsent(iimtRoot, n, w, vo.EKAbsenceProofs[i]) {
			return false
		}
	}
	for _, p := range vo.ResultProofs {
		if !omt.Verify(omtRoot, p) {
			return false
		}
	}
	if vo.LowerBoundary != nil && !omt.Verify(omtRoot, *vo.LowerBoundary) {
		return false
	}
	if vo.UpperBoundary != nil && !omt.Verify(omtRoot, *vo.UpperBoundary) {
		return false
	}

	// Recompute EK' from authenticated bitmaps and absence witnesses.
	bmLookup := map[string]bitmap.Bitmap{}
	for _, ke := range vo.KeywordEvidence {
		if ke.Proof.Bitmap.Len() != n {
			return false
		}
		bmLookup[ke.Keyword] = ke.Proof.Bitmap
	}
	for _, w := range vo.EKAbsentKeywords {
		bmLookup[w] = bitmap.New(n)
	}
	var ekBM bitmap.Bitmap
	if q.EK != nil {
		for _, w := range iomf.Keywords(q.EK) {
			if _, ok := bmLookup[w]; !ok {
				return false
			}
		}
		ekBM = iomf.Evaluate(q.EK, func(w string) bitmap.Bitmap { return bmLookup[w] })
	} else {
		ekBM = bitmap.Not(bitmap.New(n))
	}

	// Decode every result entry for a redundant sanity check (its own ts
	// must fall inside the range, if any), and collect the claimed oid set.
	resultESNs := make([]uint64, len(vo.ResultProofs))
	for i, p := range vo.ResultProofs {
		e, err := DecodeLogEntry(p.Object)
		if err != nil || e.ESN != p.OID {
			return false
		}
		resultESNs[i] = p.OID
		if q.HasRange && (e.TS < q.TS || e.TS > q.TE) {
			return false
		}
	}
	sort.Slice(resultESNs, func(i, j int) bool { return resultESNs[i] < resultESNs[j] })

	// Derive the TRUE time-range esn interval [lowerESN, upperExclusive)
	// analytically from the boundary proofs alone — never from the
	// server's claimed result set (R-VER-005) — then build its bitmap
	// directly. This is the completeness check: a hidden matching entry,
	// or a forged/omitted boundary, changes this interval and therefore
	// the final expected set.
	rangeBM := bitmap.Not(bitmap.New(n)) // no range predicate: everything passes
	if q.HasRange {
		var lowerESN uint64 = 0
		if vo.LowerBoundary != nil {
			le, err := DecodeLogEntry(vo.LowerBoundary.Object)
			if err != nil {
				return false
			}
			if le.TS >= q.TS {
				return false // not truly a "before range" boundary
			}
			lowerESN = vo.LowerBoundary.OID + 1
		}
		upperExclusive := n
		if vo.UpperBoundary != nil {
			ue, err := DecodeLogEntry(vo.UpperBoundary.Object)
			if err != nil {
				return false
			}
			if ue.TS <= q.TE {
				return false // not truly an "after range" boundary
			}
			upperExclusive = vo.UpperBoundary.OID
		}
		rangeBM = bitmap.New(n)
		for esn := lowerESN; esn < upperExclusive; esn++ {
			rangeBM.Set(esn)
		}
	}

	// Recompute final expected esn set from ekBM ∩ range and compare to
	// the returned proofs' oids (R-VER-005 discipline: never trust the
	// server's own selection).
	expectedBM := bitmap.And(ekBM, rangeBM)
	expected := expectedBM.SetIndices()
	if len(expected) != len(resultESNs) {
		return false
	}
	for i := range expected {
		if expected[i] != resultESNs[i] {
			return false
		}
	}

	return true
}
