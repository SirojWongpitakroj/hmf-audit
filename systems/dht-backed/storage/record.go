// Package storage is the reproduced Cloud Data Lake: PostgreSQL 17 holding the
// verified encrypted tuples (Q27, Q43). The storage layer is untrusted;
// integrity is enforced externally through Merkle proofs and the blockchain
// anchor (R-SYS-04).
package storage

import (
	"context"
	"errors"

	"dhtbacked/internal/hashing"
)

// ErrNotFound reports that no record carries the requested record_id.
var ErrNotFound = errors.New("storage: no such record")

// Record is one stored verified tuple. Its fields are exactly the Q43 schema.
//
// LeafIndex is the zero-based uint64 leaf position within the (epoch,
// log_type) tree, assigned in accepted-ingestion order and never changed after
// admission (Q44). It is converted to int64 only at the database boundary.
type Record struct {
	RecordID          string
	Epoch             uint64
	LogType           string
	LeafIndex         uint64
	ProducerID        string
	TimestampNanos    int64
	C                 []byte
	T                 []byte
	AD                []byte
	D                 hashing.Digest
	Sigma             []byte
	ProducerPublicKey []byte
}

// MissingError reports which record_id a batched read could not find. It
// unwraps to ErrNotFound.
type MissingError struct{ RecordID string }

func (e *MissingError) Error() string { return "storage: no such record: " + e.RecordID }
func (e *MissingError) Unwrap() error { return ErrNotFound }

// BatchGetter is an optional extension of Store: it resolves several records
// in one round trip and returns them in request order. A record_id that is not
// stored yields a *MissingError.
type BatchGetter interface {
	GetMany(ctx context.Context, recordIDs []string) ([]Record, error)
}

// Store is the data-lake interface used by the rest of the system.
type Store interface {
	// Insert stores one accepted record. A duplicate record_id, or a duplicate
	// (epoch, log_type, leaf_index), is rejected.
	Insert(ctx context.Context, r Record) error
	// Get resolves a record by its record_id (Q44).
	Get(ctx context.Context, recordID string) (Record, error)
	// GetByLeaf resolves a record by its position in an (epoch, log_type)
	// tree. This retrieval is served by the one authorized unique constraint
	// (Q43, §0.6 NAMED EXCEPTION 1).
	GetByLeaf(ctx context.Context, epoch uint64, logType string, leafIndex uint64) (Record, error)
	// Close releases the connection.
	Close(ctx context.Context) error
}
