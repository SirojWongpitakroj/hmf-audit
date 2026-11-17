// Package query implements the BLAS query protocol (V-D, Fig. 4): the
// general query Q, its split into an on-chain share Q_on and an off-chain
// share Q_off, and the three-stage processing pipeline — block level and
// transaction level at the full node, entry level at the SP.
//
// R-1 (DECISIONS.md §C/D-2 residual): FK/EK expressions passed into Stage
// 1/2 (LG/FK sub-queries) are expected to already be the CONSERVATIVE
// Boolean expansion — negative literals replaced by TRUE — per the standard
// monotone over-approximation the ruling calls for. This package does not
// perform that rewrite itself (it is a query-construction step, arguably
// belonging to whoever builds Q_on from the caller's original Q); it
// documents the expectation so Stage 1/2 callers apply it before invoking
// Stage1/Stage2, and exact NOT is applied only at Stage 3 (EK) and by the
// auditor's own bm_EW filter (which iomf.Evaluate on the file's bitmap
// already computes precisely, since that IS the level EK/FW are defined
// at).
package query

import (
	"blas/chain"
	"blas/iomf"
)

// Q is the general log-entry query (R-QUERY-001): Q = <lg, [ts,te], FK, EK>.
type Q struct {
	LG       uint64
	HasRange bool
	TS, TE   int64
	FK       *iomf.Expr // nil => no file-keyword predicate
	EK       *iomf.Expr // nil => no entry-keyword predicate
}

// QOn is the on-chain share (R-QUERY-002): Q_on = <lg, [ts,te], FK>.
type QOn struct {
	LG       uint64
	HasRange bool
	TS, TE   int64
	FK       *iomf.Expr
}

// QOff is the off-chain share (R-QUERY-002): Q_off = <lg, [ts,te], EK>.
type QOff struct {
	LG       uint64
	HasRange bool
	TS, TE   int64
	EK       *iomf.Expr
}

// Split divides Q into Q_on and Q_off (R-QUERY-002, Fig. 4).
func Split(q Q) (QOn, QOff) {
	qon := QOn{LG: q.LG, HasRange: q.HasRange, TS: q.TS, TE: q.TE, FK: q.FK}
	qoff := QOff{LG: q.LG, HasRange: q.HasRange, TS: q.TS, TE: q.TE, EK: q.EK}
	return qon, qoff
}

// FileRef identifies one log file by its (lid, fid) pair (D4 in
// DECISIONS.md).
type FileRef struct {
	LID uint64
	FID uint64
}

// AnchorHeader returns, for a segment ending at globalHeight, the block
// header the auditor should check this segment's block-level roots
// against (E2: the segment's roots as committed by its most recent block
// so far).
func AnchorHeader(c *chain.Chain, globalHeight uint64) chain.BlockHeader {
	return c.Blocks()[globalHeight].Header
}
