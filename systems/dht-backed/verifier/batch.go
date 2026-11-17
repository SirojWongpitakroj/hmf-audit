package verifier

import (
	"context"
	"errors"
	"fmt"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/encryptor"
	"dhtbacked/internal/hashing"
	"dhtbacked/storage"
)

// loadRecords resolves every requested record_id, in request order. A store
// that implements storage.BatchGetter serves the whole batch in one call;
// otherwise each record is read on its own (Q44).
func (v *Verifier) loadRecords(ctx context.Context, recordIDs []string) ([]storage.Record, error) {
	if batch, ok := v.store.(storage.BatchGetter); ok {
		records, err := batch.GetMany(ctx, recordIDs)
		if err != nil {
			var missing *storage.MissingError
			if errors.As(err, &missing) {
				return nil, fmt.Errorf("%w: %s", errRecordNotFound, missing.RecordID)
			}
			return nil, err
		}
		if len(records) != len(recordIDs) {
			return nil, errProofMalformed
		}
		for i, rec := range records {
			if err := validateRequestedRecord(recordIDs[i], rec); err != nil {
				return nil, err
			}
		}
		return records, nil
	}
	records := make([]storage.Record, 0, len(recordIDs))
	for _, id := range recordIDs {
		rec, err := v.store.Get(ctx, id)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", errRecordNotFound, id)
		}
		if err != nil {
			return nil, err
		}
		if err := validateRequestedRecord(id, rec); err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

// validateRequestedRecord compares the caller's requested ID and the row's
// lookup metadata with AD, which is included in the anchored leaf digest.
func validateRequestedRecord(id string, rec storage.Record) error {
	producerID, logType, timestamp, recordID, err := encryptor.ParseAD(rec.AD)
	if err != nil || rec.RecordID != id || recordID != id ||
		producerID != rec.ProducerID || logType != rec.LogType || timestamp != rec.TimestampNanos {
		return fmt.Errorf("%w: record identity or metadata does not match authenticated AD", errProofMalformed)
	}
	return nil
}

// prefetch hands a BatchProofSource every proof record the batch needs. Which
// records those are is fixed by the leaf positions alone: the sibling of each
// leaf at every level below the ancestor, one certificate per distinct
// ancestor, and the root record of every log type in the epoch. A record that
// several leaves share is requested once. With a plain ProofSource prefetch does
// nothing and the fold reads records one at a time.
func (v *Verifier) prefetch(tok *auth.Token, sess *session, epoch uint64,
	work []leafWork, levels, stride uint64) error {

	source, ok := v.proof.(BatchProofSource)
	if !ok {
		return nil
	}
	seen := make(map[hashing.Digest]struct{})
	var requests []ProofRequest
	add := func(r ProofRequest) {
		if _, dup := seen[r.Key]; dup {
			return
		}
		seen[r.Key] = struct{}{}
		requests = append(requests, r)
	}
	for _, w := range work {
		idx := w.record.LeafIndex
		for j := levels; j > stride; j-- {
			add(ProofRequest{Kind: ProofParent, Epoch: w.record.Epoch, LogType: w.record.LogType,
				A: j, B: idx ^ 1, Key: dht.KeyP(w.treeID, j, idx^1)})
			idx >>= 1
		}
		add(ProofRequest{Kind: ProofCert, Epoch: w.record.Epoch, LogType: w.record.LogType,
			A: stride, B: w.ancestor, Key: dht.KeyC(w.treeID, stride, w.ancestor)})
	}
	for _, logType := range v.cfg.LogTypes {
		add(ProofRequest{Kind: ProofRoot, Epoch: epoch, LogType: logType,
			Key: dht.KeyR(dht.TreeID(epoch, logType))})
	}
	fetched, err := source.GetMany(tok, requests)
	if err != nil {
		return fmt.Errorf("%w: %v", errProofUnavailable, err)
	}
	sess.fetched = fetched
	return nil
}
