package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// SQLStore is the bounded pool-backed log/object store used by incremental
// ingestion and audits. Authentication entries belong to the DHT proof backend
// (Cassandra in the active deployment), not this relational store.
type SQLStore struct {
	DB                              *sql.DB
	MaxRecords, BatchRows, MaxBytes int
	Reads, Writes, ReadBytes        uint64
	Rows                            uint64
	ControlReadBytes                uint64
}

func (s *SQLStore) Migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS dhtbacked;"+strings.Replace(schemaSQL, "CREATE TABLE IF NOT EXISTS records", "CREATE TABLE IF NOT EXISTS dhtbacked.records", 1))
	return err
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func insertRecord(ctx context.Context, q sqlExecutor, r Record) error {
	_, err := q.ExecContext(ctx, strings.Replace(insertSQL, "INSERT INTO records", "INSERT INTO dhtbacked.records", 1), r.RecordID, int64(r.Epoch), r.LogType, int64(r.LeafIndex), r.ProducerID, r.TimestampNanos, r.C, r.T, r.AD, r.D[:], r.Sigma, r.ProducerPublicKey)
	return err
}
func InsertTransaction(ctx context.Context, tx *sql.Tx, r Record) error {
	return insertRecord(ctx, tx, r)
}
func (s *SQLStore) Insert(ctx context.Context, r Record) error {
	s.Writes++
	return insertRecord(ctx, s.DB, r)
}
func (s *SQLStore) Get(ctx context.Context, id string) (Record, error) {
	records, err := s.GetMany(ctx, []string{id})
	if err != nil {
		return Record{}, err
	}
	return records[0], nil
}
func (s *SQLStore) GetMany(ctx context.Context, ids []string) ([]Record, error) {
	if s.MaxRecords < 1 || s.BatchRows < 1 || s.MaxBytes < 1 || len(ids) > s.MaxRecords {
		return nil, errors.New("storage: configured record request limit exceeded")
	}
	if len(ids) == 0 {
		return nil, nil
	}
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	s.Reads++
	var bytes uint64
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(sum(octet_length(c)+octet_length(t)+octet_length(ad)+octet_length(d)+octet_length(sigma)+octet_length(producer_public_key)+octet_length(record_id)+octet_length(log_type)+octet_length(producer_id)+512),0) FROM dhtbacked.records WHERE record_id=ANY($1::text[])`, ids).Scan(&bytes)
	if err != nil {
		return nil, err
	}
	s.Rows++ // One aggregate-size row was returned.
	s.ControlReadBytes += 8
	if bytes+uint64(len(ids))*256 > uint64(s.MaxBytes) {
		return nil, errors.New("storage: configured record byte limit exceeded")
	}
	out := make([]Record, 0, len(ids))
	for offset := 0; offset < len(ids); offset += s.BatchRows {
		part := ids[offset:min(offset+s.BatchRows, len(ids))]
		s.Reads++
		rows, e := tx.QueryContext(ctx, `SELECT `+strings.Join(recordColumns, ",")+` FROM dhtbacked.records WHERE record_id=ANY($1::text[])`, part)
		if e != nil {
			return nil, e
		}
		records := make(map[string]Record, len(part))
		for rows.Next() {
			var r Record
			var epoch, leaf int64
			var digest []byte
			if e = rows.Scan(&r.RecordID, &epoch, &r.LogType, &leaf, &r.ProducerID, &r.TimestampNanos, &r.C, &r.T, &r.AD, &digest, &r.Sigma, &r.ProducerPublicKey); e != nil {
				break
			}
			if epoch < 1 || leaf < 0 || len(digest) != len(r.D) {
				e = errors.New("storage: invalid persistent record metadata")
				break
			}
			r.Epoch = uint64(epoch)
			r.LeafIndex = uint64(leaf)
			copy(r.D[:], digest)
			records[r.RecordID] = r
			s.Rows++
			s.ReadBytes += uint64(len(r.RecordID) + len(r.LogType) + len(r.ProducerID) + 3*8 + len(r.C) + len(r.T) + len(r.AD) + len(r.D) + len(r.Sigma) + len(r.ProducerPublicKey))
		}
		readErr := rows.Err()
		rows.Close()
		if e != nil {
			return nil, e
		}
		if readErr != nil {
			return nil, readErr
		}
		for _, id := range part {
			r, ok := records[id]
			if !ok {
				return nil, &MissingError{RecordID: id}
			}
			out = append(out, r)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
func (s *SQLStore) GetByLeaf(ctx context.Context, epoch uint64, logType string, leaf uint64) (Record, error) {
	s.Reads++
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT record_id FROM dhtbacked.records WHERE epoch=$1 AND log_type=$2 AND leaf_index=$3`, int64(epoch), logType, int64(leaf)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("storage: leaf lookup: %w", err)
	}
	return s.Get(ctx, id)
}

// The caller owns the shared connection pool.
func (s *SQLStore) Close(context.Context) error { return nil }
