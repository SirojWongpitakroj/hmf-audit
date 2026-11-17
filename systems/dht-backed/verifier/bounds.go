package verifier

import (
	"errors"
	"fmt"

	"dhtbacked/blockchain"
	"dhtbacked/mktree"
	"dhtbacked/storage"
)

type RequestBounds struct{ Records, ProofRequests, MapBytes, ProofBytes int }

var errWorkingSet = fmt.Errorf("%w: configured request working-set limit exceeded", ErrRejected)

// NewBounded keeps the native verifier and certificate reuse unchanged. It
// rejects oversized work before allocating request/history maps. The proof and
// record sources must independently enforce their byte/batch limits.
func NewBounded(store storage.Store, proof ProofSource, anchor blockchain.RootSource, cfg mktree.Config, b RequestBounds) (*Verifier, error) {
	if store == nil || proof == nil || anchor == nil || b.Records < 1 || b.ProofRequests < 1 || b.MapBytes < 1 || cfg.EpochCapacity < 2 || cfg.EpochCapacity > 1000000 {
		return nil, errors.New("verifier: invalid explicit bounds or native epoch geometry")
	}
	v := New(store, proof, anchor, cfg)
	levels, stride := v.geometry()
	if stride > levels {
		return nil, errors.New("verifier: stride exceeds tree height")
	}
	v.bounds = &b
	return v, nil
}
func (v *Verifier) checkBounds(n int) error {
	if v.bounds == nil {
		return nil
	}
	b := v.bounds
	if n < 0 || n > b.Records {
		return errWorkingSet
	}
	levels, stride := v.geometry()
	if stride > levels {
		return errWorkingSet
	}
	// Conservative upper bound includes dedup maps, work/group descriptors and
	// slice/queue metadata. Raw proof/record bytes have separate source budgets.
	requests := uint64(n)*(levels-stride+2) + uint64(len(v.cfg.LogTypes))
	if b.ProofBytes < 0 || requests > uint64(b.ProofRequests) || requests*1024+uint64(b.ProofBytes) > uint64(b.MapBytes) {
		return errWorkingSet
	}
	return nil
}
