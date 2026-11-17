package canonicalingestion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"time"
)

//go:embed schema.sql
var schema string

type Store struct{ DB *sql.DB }

func (s Store) Migrate(ctx context.Context) error {
	if s.DB == nil {
		return errors.New("canonical ingestion: database required")
	}
	// All live consumers may start together on a fresh database. PostgreSQL's
	// IF NOT EXISTS alone does not serialize concurrent catalog initialization.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(783426159)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, schema); err != nil {
		return err
	}
	return tx.Commit()
}

type Manifest struct {
	StreamID, Fingerprint string
	Committed             uint64
	PrefixHash            [32]byte
}

func (s Store) Create(ctx context.Context, id, fingerprint string) error {
	if id == "" || fingerprint == "" {
		return errors.New("stream ID and immutable fingerprint required")
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO canonical_ingestion.streams(stream_id,fingerprint,prefix_hash) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, id, fingerprint, make([]byte, 32))
	if err != nil {
		return err
	}
	m, err := s.Manifest(ctx, id)
	if err == nil && m.Fingerprint != fingerprint {
		return errors.New("stream fingerprint changed")
	}
	return err
}

func (s Store) Manifest(ctx context.Context, id string) (Manifest, error) {
	var m Manifest
	var n int64
	var h []byte
	err := s.DB.QueryRowContext(ctx, `SELECT stream_id,fingerprint,committed,prefix_hash FROM canonical_ingestion.streams WHERE stream_id=$1`, id).Scan(&m.StreamID, &m.Fingerprint, &n, &h)
	if err != nil {
		return m, err
	}
	if n < 0 || len(h) != 32 {
		return m, errors.New("corrupt stream manifest")
	}
	m.Committed = uint64(n)
	copy(m.PrefixHash[:], h)
	return m, nil
}

func (s Store) Prefix(ctx context.Context, id string, n uint64) ([32]byte, error) {
	return prefixAt(ctx, s.DB, id, n)
}

func nextHash(previous [32]byte, e Entry) [32]byte {
	return nextHashPayload(previous, e, sha256.Sum256(e.Record))
}
func nextHashPayload(previous [32]byte, e Entry, payload [32]byte) [32]byte {
	h := sha256.New()
	h.Write(previous[:])
	var n [8]byte
	binary.BigEndian.PutUint64(n[:], e.Ordinal)
	h.Write(n[:])
	h.Write([]byte(e.SourceID))
	h.Write(payload[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// Append publishes one bounded finalized prefix. It never changes an existing
// record. A replay is allowed only when every byte and ordinal agrees.
func (s Store) Append(ctx context.Context, id string, entries []Entry, limits Limits) error {
	_, err := s.AppendWithResult(ctx, id, entries, limits)
	return err
}

// AppendTimings measures client elapsed work, including database/network waits.
// The phases are disjoint within one append; they are not server-only timings.
type AppendTimings struct {
	Validation, BeginLock, BuildRows, Insert, Update, Commit time.Duration
}

type AppendResult struct {
	Committed  uint64
	PrefixHash [32]byte
	Timings    AppendTimings
}

// AppendWithResult returns the transaction's cursor/hash only after COMMIT is
// acknowledged. On failure only timings are returned; callers must reopen the
// durable manifest to resolve an uncertain commit, exactly as with Append.
func (s Store) AppendWithResult(ctx context.Context, id string, entries []Entry, limits Limits) (result AppendResult, err error) {
	started := time.Now()
	defer func() {
		if err != nil {
			result.Committed, result.PrefixHash = 0, [32]byte{}
		}
	}()
	err = limits.Validate()
	if err == nil {
		err = validateBatch(entries, limits)
	}
	result.Timings.Validation = time.Since(started)
	if err != nil {
		return result, err
	}
	started = time.Now()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		result.Timings.BeginLock = time.Since(started)
		return result, err
	}
	defer tx.Rollback()
	var n int64
	var hash []byte
	if err = tx.QueryRowContext(ctx, `SELECT committed,prefix_hash FROM canonical_ingestion.streams WHERE stream_id=$1 FOR UPDATE`, id).Scan(&n, &hash); err != nil {
		result.Timings.BeginLock = time.Since(started)
		return result, err
	}
	result.Timings.BeginLock = time.Since(started)
	if len(hash) != 32 {
		return result, errors.New("invalid prefix hash")
	}
	started = time.Now()
	buildStarted := started
	buildComplete := false
	defer func() {
		if !buildComplete {
			result.Timings.BuildRows = time.Since(buildStarted)
		}
	}()
	var prefix [32]byte
	copy(prefix[:], hash)
	var insertRows [][]any
	for _, e := range entries {
		payloadHash := sha256.Sum256(e.Record)
		if e.Ordinal <= uint64(n) {
			var source string
			var payload []byte
			if err = tx.QueryRowContext(ctx, `SELECT source_id,payload FROM canonical_ingestion.entries WHERE stream_id=$1 AND ordinal=$2`, id, int64(e.Ordinal)).Scan(&source, &payload); err != nil {
				return result, err
			}
			if source != e.SourceID || !bytes.Equal(payload, e.Record) {
				return result, errors.New("immutable stream replay differs")
			}
			continue
		}
		if e.Ordinal != uint64(n)+1 {
			return result, errors.New("non-contiguous canonical stream")
		}
		prefix = nextHashPayload(prefix, e, payloadHash)
		// Copy the rolling prefix: later iterations mutate prefix in place.
		checkpoint := append([]byte(nil), prefix[:]...)
		insertRows = append(insertRows, []any{id, int64(e.Ordinal), e.SourceID, []byte(e.Record), payloadHash[:], checkpoint})
		n++
	}
	result.Timings.BuildRows = time.Since(started)
	buildComplete = true
	started = time.Now()
	err = InsertRows(ctx, tx, `INSERT INTO canonical_ingestion.entries(stream_id,ordinal,source_id,payload,payload_hash,prefix_hash) VALUES `, "", insertRows, limits.MaxRecords, limits.MaxBatchBytes, nil)
	result.Timings.Insert = time.Since(started)
	if err != nil {
		return result, err
	}
	started = time.Now()
	_, err = tx.ExecContext(ctx, `UPDATE canonical_ingestion.streams SET committed=$2,prefix_hash=$3 WHERE stream_id=$1`, id, n, prefix[:])
	result.Timings.Update = time.Since(started)
	if err != nil {
		return result, err
	}
	started = time.Now()
	err = tx.Commit()
	result.Timings.Commit = time.Since(started)
	if err != nil {
		return result, err
	}
	result.Committed, result.PrefixHash = uint64(n), prefix
	return result, nil
}

type Limits struct{ MaxRecords, MaxBatchBytes, MaxRecordBytes int }

func (l Limits) Validate() error {
	if l.MaxRecords <= 0 || l.MaxBatchBytes <= 0 || l.MaxRecordBytes <= 0 || l.MaxRecordBytes > l.MaxBatchBytes {
		return errors.New("positive record and byte limits required")
	}
	return nil
}
func validateBatch(es []Entry, l Limits) error {
	if len(es) > l.MaxRecords {
		return errors.New("record batch limit exceeded")
	}
	total := 0
	sources := make(map[string]struct{}, len(es))
	for i, e := range es {
		if e.Ordinal > math.MaxInt64 {
			return errors.New("ordinal overflow")
		}
		if err := e.Validate(l.MaxRecordBytes); err != nil {
			return err
		}
		total += len(e.Record) + len(e.SourceID) + 8
		if total > l.MaxBatchBytes {
			return errors.New("batch byte limit exceeded")
		}
		if i > 0 && e.Ordinal != es[i-1].Ordinal+1 {
			return errors.New("non-contiguous batch")
		}
		// Cross-batch uniqueness is the producer journal's job, not an index here.
		if _, ok := sources[e.SourceID]; ok {
			return errors.New("duplicate source in batch")
		}
		sources[e.SourceID] = struct{}{}
	}
	return nil
}

// Read seeks by ordinal, never scans a previously consumed prefix. SQL LIMIT
// bounds rows; MaxRecordBytes is enforced before allocating a payload in Go.
func (s Store) Read(ctx context.Context, id string, after, through uint64, l Limits) ([]Entry, error) {
	return s.read(ctx, id, after, through, l, true)
}
func (s Store) read(ctx context.Context, id string, after, through uint64, l Limits, validate bool) ([]Entry, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if after > math.MaxInt64 || through > math.MaxInt64 || after > through {
		return nil, errors.New("invalid prefix range")
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT ordinal,source_id,octet_length(payload),CASE WHEN octet_length(payload)<=$5 THEN payload ELSE NULL END FROM canonical_ingestion.entries WHERE stream_id=$1 AND ordinal>$2 AND ordinal<=$3 ORDER BY ordinal LIMIT $4`, id, int64(after), int64(through), l.MaxRecords, l.MaxRecordBytes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Entry
	total := 0
	for rows.Next() {
		var e Entry
		var n int64
		var size int
		var payload []byte
		if err = rows.Scan(&n, &e.SourceID, &size, &payload); err != nil {
			return nil, err
		}
		if size > l.MaxRecordBytes {
			return nil, errors.New("stored canonical record exceeds configured limit")
		}
		e.Ordinal = uint64(n)
		e.Record = payload
		cost := size + len(e.SourceID) + 8
		if cost > l.MaxBatchBytes {
			return nil, errors.New("one canonical entry exceeds batch budget")
		}
		if total+cost > l.MaxBatchBytes {
			break
		}
		if e.Ordinal != after+uint64(len(out))+1 {
			return nil, errors.New("canonical stream gap")
		}
		if validate {
			if err = e.Validate(l.MaxRecordBytes); err != nil {
				return nil, err
			}
		}
		total += cost
		out = append(out, e)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func prefixAt(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, id string, n uint64) ([32]byte, error) {
	var h [32]byte
	if n == 0 {
		return h, nil
	}
	var b []byte
	err := db.QueryRowContext(ctx, `SELECT prefix_hash FROM canonical_ingestion.entries WHERE stream_id=$1 AND ordinal=$2`, id, int64(n)).Scan(&b)
	if err != nil {
		return h, err
	}
	if len(b) != 32 {
		return h, fmt.Errorf("invalid prefix at %d", n)
	}
	copy(h[:], b)
	return h, nil
}
