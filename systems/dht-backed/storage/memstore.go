package storage

import (
	"context"
	"errors"
	"fmt"
)

// Memory is a test double for the data lake, used only so that pure-logic
// tests can run without a PostgreSQL server. The reproduction's data lake is
// PostgreSQL 17 (Q27, Q43); this type is never used on any measured path.
//
// It enforces the same two constraints the schema does: record_id is unique,
// and (epoch, log_type, leaf_index) is unique (NAMED EXCEPTION 1).
type Memory struct {
	byID  map[string]Record
	taken map[leafKey]string
}

type leafKey struct {
	epoch     uint64
	logType   string
	leafIndex uint64
}

// ErrDuplicate reports a violated uniqueness constraint.
var ErrDuplicate = errors.New("storage: duplicate key")

// NewMemory constructs the in-memory test double.
func NewMemory() *Memory {
	return &Memory{byID: make(map[string]Record), taken: make(map[leafKey]string)}
}

// Insert stores one record, rejecting duplicates on either unique constraint.
func (m *Memory) Insert(_ context.Context, r Record) error {
	if _, ok := m.byID[r.RecordID]; ok {
		return fmt.Errorf("%w: record_id %q", ErrDuplicate, r.RecordID)
	}
	k := leafKey{r.Epoch, r.LogType, r.LeafIndex}
	if prev, ok := m.taken[k]; ok {
		return fmt.Errorf("%w: (epoch, log_type, leaf_index) = (%d, %s, %d) already held by %q",
			ErrDuplicate, r.Epoch, r.LogType, r.LeafIndex, prev)
	}
	m.byID[r.RecordID] = r
	m.taken[k] = r.RecordID
	return nil
}

// Get resolves a record by record_id.
func (m *Memory) Get(_ context.Context, recordID string) (Record, error) {
	r, ok := m.byID[recordID]
	if !ok {
		return Record{}, ErrNotFound
	}
	return r, nil
}

// GetByLeaf resolves a record by its leaf position.
func (m *Memory) GetByLeaf(_ context.Context, epoch uint64, logType string, leafIndex uint64) (Record, error) {
	id, ok := m.taken[leafKey{epoch, logType, leafIndex}]
	if !ok {
		return Record{}, ErrNotFound
	}
	return m.byID[id], nil
}

// Close is a no-op.
func (m *Memory) Close(context.Context) error { return nil }

// Len returns the number of stored records.
func (m *Memory) Len() int { return len(m.byID) }

// Replace overwrites a stored record in place, keeping its identity and leaf
// position. It exists so tests can model an untrusted data lake that mutates a
// stored record (R-SYS-04, R-SEC-04).
func (m *Memory) Replace(r Record) {
	m.byID[r.RecordID] = r
	m.taken[leafKey{r.Epoch, r.LogType, r.LeafIndex}] = r.RecordID
}
