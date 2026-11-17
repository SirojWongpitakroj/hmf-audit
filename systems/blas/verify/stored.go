package verify

import (
	"errors"

	"blas/alis"
	"blas/chain"
	"blas/optimize"
	"blas/query"
)

// NewStoredAuditor reads headers from the auditor's trusted persistent header
// store. The source must not be the full node's untrusted query response.
// Only counts/configuration and the current verification state stay in RAM.
func NewStoredAuditor(count int, segmentSize uint64, read func(uint64) (chain.BlockHeader, error)) (*Auditor, error) {
	if count < 0 || segmentSize == 0 || read == nil {
		return nil, errors.New("verify: invalid stored header configuration")
	}
	return &Auditor{storedHeaders: count, SegmentSize: segmentSize, headerReader: read}, nil
}
func (a *Auditor) headerCount() int {
	if a.headerReader != nil {
		return a.storedHeaders
	}
	return len(a.Headers)
}
func (a *Auditor) headerAt(n uint64) chain.BlockHeader {
	if n >= uint64(a.headerCount()) {
		a.headerError = errors.New("verify: header outside trusted extent")
		return chain.BlockHeader{}
	}
	if a.headerReader == nil {
		return a.Headers[n]
	}
	h, err := a.headerReader(n)
	if err != nil {
		a.headerError = err
	}
	if h.BlockHeight != n {
		a.headerError = errors.New("verify: stored header height mismatch")
	}
	return h
}
func (a *Auditor) storedIntersecting(q query.QOn) []uint64 {
	count := optimize.SegmentCount(a.headerCount(), a.SegmentSize)
	var out []uint64
	for i := count; i > 0; i-- {
		idx := i - 1
		h := a.headerAt(optimize.AnchorHeight(idx, a.SegmentSize, a.headerCount()))
		if !q.HasRange || (h.BlockTsMin <= q.TE && h.BlockTsMax >= q.TS) {
			out = append(out, idx)
		}
		if q.HasRange && h.PastTsMax < q.TS {
			break
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}
func (a *Auditor) Err() error { return a.headerError }
func (a *Auditor) VerifyStage1(q query.QOn, s query.Stage1Result, scan bool) ([]uint64, bool) {
	var ids []uint64
	var ok bool
	if scan {
		ids, ok = a.verifyStage1Optimized(q, s)
	} else {
		ids, ok = a.verifyStage1(q, s)
	}
	return ids, ok && a.Err() == nil
}
func (a *Auditor) VerifyStage2(q query.QOn, ids []uint64, s query.Stage2Result) ([]alis.Transaction, bool) {
	txs, ok := a.verifyStage2(q, ids, s)
	return txs, ok && a.Err() == nil
}
func (a *Auditor) VerifyStage2Aggregated(q query.QOn, ids []uint64, s query.Stage2Result, bindings map[uint64]query.AggregatedLGBinding) ([]alis.Transaction, bool) {
	txs, ok := a.verifyStage2Aggregated(q, ids, s, bindings)
	return txs, ok && a.Err() == nil
}
