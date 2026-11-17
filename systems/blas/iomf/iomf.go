// Package iomf implements the Index-Object Merkle Forest's query processing
// (Algorithm 3, IV-D) and verification (IV-E): a Boolean keyword expression
// K evaluated over an IIMT, a set of range sub-queries evaluated over one
// RIMBT per numerical attribute, and object retrieval through an OMT
// (R-IOMF-022..025, R-VER-001..006).
//
// Scope note: R governs "one range sub-query per range condition, covering
// all numerical attributes in R"; every worked example and every §12 query
// type in REQUIREMENTS.md combines range sub-queries with AND only (never
// OR/NOT). This package implements exactly that: R is a plain list of
// RangeCond, combined by intersection. Nothing in DECISIONS.md rules on an
// OR/NOT combinator for R, and none is needed by any acceptance test, so
// none is added (no unstated feature).
package iomf

import (
	"fmt"
	"sort"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"blas/omt"
	"blas/rimbt"
)

// ExprKind identifies the shape of one Boolean expression node.
type ExprKind int

const (
	ExprKeyword ExprKind = iota
	ExprNot
	ExprAnd
	ExprOr
)

// Expr is a Boolean keyword expression over K. Callers build the tree
// directly in the exact evaluation order they intend (R-QUERY-004): e.g.
// ¬a ∧ b ∨ c as Or(And(Not(Keyword("a")), Keyword("b")), Keyword("c")),
// which Evaluate walks exactly as written — ((¬a) ∧ b) ∨ c.
type Expr struct {
	Kind    ExprKind
	Keyword string
	Left    *Expr
	Right   *Expr
}

func Keyword(w string) *Expr { return &Expr{Kind: ExprKeyword, Keyword: w} }
func Not(e *Expr) *Expr      { return &Expr{Kind: ExprNot, Left: e} }
func And(a, b *Expr) *Expr   { return &Expr{Kind: ExprAnd, Left: a, Right: b} }
func Or(a, b *Expr) *Expr    { return &Expr{Kind: ExprOr, Left: a, Right: b} }

// Keywords returns the distinct keywords appearing in e, in left-to-right
// order of first occurrence (deterministic; the dedup lookup is a Go map
// used only as an accelerator, never iterated — R-GO-006).
func Keywords(e *Expr) []string {
	seen := map[string]bool{}
	var order []string
	var walk func(*Expr)
	walk = func(n *Expr) {
		if n == nil {
			return
		}
		switch n.Kind {
		case ExprKeyword:
			if !seen[n.Keyword] {
				seen[n.Keyword] = true
				order = append(order, n.Keyword)
			}
		case ExprNot:
			walk(n.Left)
		case ExprAnd, ExprOr:
			walk(n.Left)
			walk(n.Right)
		}
	}
	walk(e)
	return order
}

// Evaluate computes the bitmap for e using bm(w) to resolve each keyword
// leaf, applying bitwise NOT/AND/OR in exactly the tree's structure
// (R-QUERY-003, R-QUERY-004, R-IOMF-024).
func Evaluate(e *Expr, bm func(string) bitmap.Bitmap) bitmap.Bitmap {
	switch e.Kind {
	case ExprKeyword:
		return bm(e.Keyword)
	case ExprNot:
		return bitmap.Not(Evaluate(e.Left, bm))
	case ExprAnd:
		return bitmap.And(Evaluate(e.Left, bm), Evaluate(e.Right, bm))
	case ExprOr:
		return bitmap.Or(Evaluate(e.Left, bm), Evaluate(e.Right, bm))
	default:
		panic("iomf: unknown expr kind")
	}
}

// RangeCond is one range sub-query SR = [Alpha, Beta] on attribute Attr
// (R-IOMF-021: one RIMBT per numerical attribute).
type RangeCond struct {
	Attr  string
	Alpha int64
	Beta  int64
}

// Query is Q = <K, R> (R-IOMF-022). K may be nil (range-only query); R may
// be empty (keyword-only query) — R-IOMF-022's "when only one part is
// present" case.
type Query struct {
	K *Expr
	R []RangeCond
}

// KeywordEvidence is (BM_K element, π_K element) for one queried keyword.
type KeywordEvidence struct {
	Keyword string
	Proof   iimt.Proof
}

// RangeEvidence is (BM_SR, π_SR) for one range sub-query.
type RangeEvidence struct {
	Attr   string
	Result rimbt.RangeResult
}

// VO is the IOMF-level verification object (IV-E): <BM_K, π_K, BM_R, π_R,
// QR_O, π_O>, here as KeywordEvidence (carries BM_K+π_K), RangeEvidence
// (carries BM_R+π_R), QRO, and ObjectProofs (π_O, each also carrying the
// returned object bytes — the QR of R-IOMF-025).
type VO struct {
	KeywordEvidence []KeywordEvidence
	RangeEvidence   []RangeEvidence
	QRO             []uint64
	ObjectProofs    []omt.Proof
}

// Store bundles one IOMF instance: an IIMT, an OMT, and zero or more
// RIMBTs keyed by attribute name (R-IOMF-021, R-ALIS-002-style bundling
// used at every ALIS level).
type Store struct {
	IIMT   *iimt.Tree
	OMT    *omt.Tree
	RIMBTs map[string]*rimbt.Tree
}

// setToBitmap materializes a sorted oid-index set as a length-n bitmap.
func setToBitmap(n uint64, idxs []uint64) bitmap.Bitmap {
	bm := bitmap.New(n)
	for _, i := range idxs {
		bm.Set(i)
	}
	return bm
}

func allOnes(n uint64) bitmap.Bitmap {
	return bitmap.Not(bitmap.New(n))
}

// ProcessQuery implements Algorithm 3 (R-IOMF-023) against Store s, over an
// authenticated object population of size n (R-GO-016: n must come from the
// caller's own authenticated source, e.g. the tree's own N, never an
// unauthenticated field in the query).
func ProcessQuery(s *Store, n uint64, q Query) (VO, error) {
	var vo VO

	// Step 3-5: keyword sub-queries, one per distinct keyword in K.
	bmLookup := map[string]bitmap.Bitmap{}
	if q.K != nil {
		for _, w := range Keywords(q.K) {
			_, proof, ok := s.IIMT.Query(w)
			if !ok {
				return VO{}, fmt.Errorf("iomf: keyword %q has no IIMT leaf", w)
			}
			vo.KeywordEvidence = append(vo.KeywordEvidence, KeywordEvidence{Keyword: w, Proof: proof})
			bmLookup[w] = proof.Bitmap
		}
	}
	var qk bitmap.Bitmap
	if q.K != nil {
		qk = Evaluate(q.K, func(w string) bitmap.Bitmap { return bmLookup[w] })
	} else {
		qk = allOnes(n)
	}

	// Step 6-7: range sub-queries, intersected.
	qrr := allOnes(n)
	for _, rc := range q.R {
		rt, ok := s.RIMBTs[rc.Attr]
		if !ok {
			return VO{}, fmt.Errorf("iomf: no RIMBT for attribute %q", rc.Attr)
		}
		res, err := rt.RangeQuery(rc.Alpha, rc.Beta)
		if err != nil {
			return VO{}, err
		}
		vo.RangeEvidence = append(vo.RangeEvidence, RangeEvidence{Attr: rc.Attr, Result: res})
		qrr = bitmap.And(qrr, setToBitmap(n, res.QR))
	}

	// Step 8: QR_O = QR_K ∩ QR_R (bitwise AND).
	qroBM := bitmap.And(qk, qrr)
	vo.QRO = qroBM.SetIndices()

	// Step 9-10: retrieve QR_O in the OMT.
	for _, oid := range vo.QRO {
		proof, ok := s.OMT.Prove(oid)
		if !ok {
			return VO{}, fmt.Errorf("iomf: oid %d in QR_O has no OMT leaf", oid)
		}
		vo.ObjectProofs = append(vo.ObjectProofs, proof)
	}

	return vo, nil
}

// RootHashes are the committed roots the verifier reconstructs against
// (IV-E, R-VER-001): RH.
type RootHashes struct {
	IIMTRoot   bhash.Digest
	RIMBTRoots map[string]bhash.Digest
	OMTRoot    bhash.Digest
}

// Verify implements IV-E: integrity checking (every reconstructed root
// equals RH) plus correctness checking (QR'_O independently recomputed from
// the authenticated bitmaps equals QR_O; oids derived from the returned
// objects equal QR_O). Accept iff both pass (R-VER-004). n must come from
// the verifier's own authenticated source (R-GO-016), never from the VO.
func Verify(rh RootHashes, n uint64, q Query, vo VO) bool {
	// Defensive length check first: every bitmap the VO supplies must match
	// the verifier's own authenticated n (R-GO-016, T-080). A malformed or
	// adversarial VO with an inconsistent bitmap length is a reject, never
	// a panic (R-GO-026) — bitmap.And/Or/Not panic on length mismatch, so
	// this must be checked before any bitwise op runs.
	for _, ke := range vo.KeywordEvidence {
		if ke.Proof.Bitmap.Len() != n {
			return false
		}
	}
	for _, re := range vo.RangeEvidence {
		if re.Result.LowerReal && re.Result.LowerProof.Bitmap.Len() != n {
			return false
		}
		if re.Result.UpperReal && re.Result.UpperProof.Bitmap.Len() != n {
			return false
		}
	}

	// --- Integrity ---
	for _, ke := range vo.KeywordEvidence {
		if ke.Keyword != ke.Proof.Keyword || !iimt.Verify(rh.IIMTRoot, ke.Proof) {
			return false
		}
	}
	if len(vo.RangeEvidence) != len(q.R) {
		return false
	}
	for i, re := range vo.RangeEvidence {
		if re.Attr != q.R[i].Attr {
			return false
		}
		root, ok := rh.RIMBTRoots[re.Attr]
		if !ok {
			return false
		}
		if !rimbt.VerifyRangeResult(root, n, q.R[i].Alpha, q.R[i].Beta, re.Result) {
			return false
		}
	}
	for _, op := range vo.ObjectProofs {
		if !omt.Verify(rh.OMTRoot, op) {
			return false
		}
	}

	// --- Correctness: recompute QR'_O from the AUTHENTICATED bitmaps only,
	// never trusting vo.QRO itself (R-VER-005). ---
	bmLookup := map[string]bitmap.Bitmap{}
	for _, ke := range vo.KeywordEvidence {
		bmLookup[ke.Keyword] = ke.Proof.Bitmap
	}
	var qkPrime bitmap.Bitmap
	if q.K != nil {
		if !allKeywordsPresent(q.K, bmLookup) {
			return false
		}
		qkPrime = Evaluate(q.K, func(w string) bitmap.Bitmap { return bmLookup[w] })
	} else {
		qkPrime = allOnes(n)
	}

	qrrPrime := allOnes(n)
	for _, re := range vo.RangeEvidence {
		var lower, upper bitmap.Bitmap
		if re.Result.LowerReal {
			lower = re.Result.LowerProof.Bitmap
		} else {
			lower = bitmap.New(n)
		}
		if !re.Result.UpperReal {
			upper = bitmap.New(n)
		} else {
			upper = re.Result.UpperProof.Bitmap
		}
		qrrPrime = bitmap.And(qrrPrime, bitmap.And(bitmap.Not(lower), upper))
	}

	qroPrimeBM := bitmap.And(qkPrime, qrrPrime)
	qroPrime := qroPrimeBM.SetIndices()

	if !sortedUint64Equal(qroPrime, vo.QRO) {
		return false
	}

	// oids derived from the returned objects (vo.ObjectProofs) must equal
	// QR_O.
	oidsFromObjects := make([]uint64, len(vo.ObjectProofs))
	for i, op := range vo.ObjectProofs {
		oidsFromObjects[i] = op.OID
	}
	sort.Slice(oidsFromObjects, func(i, j int) bool { return oidsFromObjects[i] < oidsFromObjects[j] })
	if !sortedUint64Equal(oidsFromObjects, vo.QRO) {
		return false
	}

	return true
}

func allKeywordsPresent(e *Expr, bm map[string]bitmap.Bitmap) bool {
	for _, w := range Keywords(e) {
		if _, ok := bm[w]; !ok {
			return false
		}
	}
	return true
}

func sortedUint64Equal(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
