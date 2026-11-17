// Package benchmark wires the components into the paper's pipeline and runs
// the reproduction's measurements (Q28, Q30, Q39).
//
// Components communicate by ordinary in-process function calls; there is no
// REST, gRPC, message queue or microservice boundary anywhere (Q28).
package benchmark

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/ingestverifier"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
)

// Chain is the anchoring interface the pipeline needs.
type Chain interface {
	Commit(ctx context.Context, epoch uint64, root hashing.Digest) error
	GetRoot(ctx context.Context, epoch uint64) (hashing.Digest, error)
}

// ErrDiscarded reports a record the ingestion verifier refused. Failing tuples
// are discarded and contribute no leaf to any MK-Tree and no DHT record
// (R-ING-03).
var ErrDiscarded = errors.New("benchmark: record discarded by the ingestion verifier")

// Pipeline is Producer -> Encryptor/Attestation -> Ingestion Verifier ->
// accepted storage + Partitioner (Q20).
type Pipeline struct {
	enc    *encryptor.Encryptor
	keys   map[string]ed25519.PrivateKey
	ingest *ingestverifier.Verifier
	store  storage.Store
	part   *mktree.Partitioner
	chain  Chain
}

// NewPipeline wires the components together.
func NewPipeline(enc *encryptor.Encryptor, keys []producer.KeyPair, reg *ingestverifier.Registry,
	store storage.Store, part *mktree.Partitioner, chain Chain) *Pipeline {

	byID := make(map[string]ed25519.PrivateKey, len(keys))
	for _, kp := range keys {
		byID[kp.ID] = kp.Private
	}
	return &Pipeline{
		enc:    enc,
		keys:   byID,
		ingest: ingestverifier.New(reg),
		store:  store,
		part:   part,
		chain:  chain,
	}
}

// Partitioner exposes the partitioner for inspection.
func (p *Pipeline) Partitioner() *mktree.Partitioner { return p.part }

// Ingest runs one record through the whole pipeline and returns its assigned
// leaf index.
func (p *Pipeline) Ingest(ctx context.Context, rec encryptor.Record) (uint64, error) {
	sk, ok := p.keys[rec.ProducerID]
	if !ok {
		return 0, fmt.Errorf("benchmark: no signing key for %s", rec.ProducerID)
	}
	// Phase 1, producer side.
	em, err := p.enc.ProcessRecord(rec, sk)
	if err != nil {
		return 0, err
	}
	// Phase 2, ingestion gate.
	if err := p.ingest.Validate(em); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrDiscarded, err)
	}
	// Phase 3, partitioning and MK-Tree insertion.
	leafIndex, err := p.part.Insert(rec.LogType, em.D)
	if err != nil {
		return 0, err
	}
	// Accepted storage.
	err = p.store.Insert(ctx, storage.Record{
		RecordID:          rec.RecordID,
		Epoch:             p.part.Epoch(),
		LogType:           rec.LogType,
		LeafIndex:         leafIndex,
		ProducerID:        rec.ProducerID,
		TimestampNanos:    rec.TimestampNanos,
		C:                 em.C,
		T:                 em.T,
		AD:                em.AD,
		D:                 em.D,
		Sigma:             em.Sigma,
		ProducerPublicKey: em.ProducerPublicKey,
	})
	if err != nil {
		return 0, err
	}
	return leafIndex, nil
}

// FinalizeEpoch closes the current epoch, publishes its final DHT state,
// aggregates the per-type roots into R^(e)_agg and anchors (epoch, R_agg) on
// chain (Eq. (24)/(25), Q15).
func (p *Pipeline) FinalizeEpoch(ctx context.Context) (epoch uint64, agg hashing.Digest, err error) {
	epoch = p.part.Epoch()
	_, agg, err = p.part.Finalize()
	if err != nil {
		return 0, hashing.Digest{}, err
	}
	if err := p.chain.Commit(ctx, epoch, agg); err != nil {
		return 0, hashing.Digest{}, err
	}
	return epoch, agg, nil
}

// NextEpoch advances to a fresh epoch after finalization.
func (p *Pipeline) NextEpoch() error { return p.part.NextEpoch() }

// Run ingests a slice of records, finalizing and anchoring each epoch as it
// fills. It returns the epochs it anchored.
func (p *Pipeline) Run(ctx context.Context, recs []encryptor.Record) ([]uint64, error) {
	var anchored []uint64
	for _, rec := range recs {
		if _, err := p.Ingest(ctx, rec); err != nil {
			return anchored, err
		}
		if p.part.Full() {
			epoch, _, err := p.FinalizeEpoch(ctx)
			if err != nil {
				return anchored, err
			}
			anchored = append(anchored, epoch)
			if err := p.part.NextEpoch(); err != nil {
				return anchored, err
			}
		}
	}
	return anchored, nil
}

// Harness holds everything an end-to-end run needs.
type Harness struct {
	Pipeline *Pipeline
	DHT      *dht.DHT
	Store    storage.Store
	Chain    Chain
	Config   mktree.Config
}
