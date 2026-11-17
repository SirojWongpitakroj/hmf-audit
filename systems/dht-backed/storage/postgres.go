package storage

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"dhtbacked/internal/hashing"
)

//go:embed schema.sql
var schemaSQL string

// Schema returns the DDL of the record table (Q43).
func Schema() string { return schemaSQL }

// Postgres is the PostgreSQL 17 data lake, reached through exactly one
// ordinary pgx.Conn — no pool, and no concurrent database access (Q43).
type Postgres struct {
	conn *pgx.Conn
}

// Open connects to PostgreSQL using the DSN from DHT_POSTGRES_DSN and fails
// fast if the server is unreachable (Q46).
func Open(ctx context.Context, dsn string) (*Postgres, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("storage: connect: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		conn.Close(ctx)
		return nil, fmt.Errorf("storage: ping: %w", err)
	}
	return &Postgres{conn: conn}, nil
}

// Migrate creates the record table and its one authorized unique constraint.
func (p *Postgres) Migrate(ctx context.Context) error {
	_, err := p.conn.Exec(ctx, schemaSQL)
	return err
}

// Reset removes fixture rows while retaining the schema. It is intended for
// isolated evaluation databases, never for a production data lake.
func (p *Postgres) Reset(ctx context.Context) error {
	_, err := p.conn.Exec(ctx, "TRUNCATE TABLE records")
	return err
}

const insertSQL = `INSERT INTO records (
	record_id, epoch, log_type, leaf_index, producer_id, timestamp_ns,
	c, t, ad, d, sigma, producer_public_key
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`

// Insert stores one accepted record. leaf_index is a uint64 in Go and is
// converted to int64 at the database boundary (Q43).
func (p *Postgres) Insert(ctx context.Context, r Record) error {
	_, err := p.conn.Exec(ctx, insertSQL,
		r.RecordID, int64(r.Epoch), r.LogType, int64(r.LeafIndex), r.ProducerID,
		r.TimestampNanos, r.C, r.T, r.AD, r.D[:], r.Sigma, r.ProducerPublicKey)
	if err != nil {
		return fmt.Errorf("storage: insert: %w", err)
	}
	return nil
}

var recordColumns = []string{
	"record_id", "epoch", "log_type", "leaf_index", "producer_id", "timestamp_ns",
	"c", "t", "ad", "d", "sigma", "producer_public_key",
}

// CopyRecords bulk-loads a bounded batch of already-validated records using
// PostgreSQL COPY. The caller retains ownership of records and may reuse the
// slice as soon as this method returns.
//
// CopyFromSlice presents one row at a time to pgx; unlike CopyFromRows it does
// not first materialize a [][]any copy of the complete batch.
func (p *Postgres) CopyRecords(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	const maxInt64 = uint64(1<<63 - 1)
	for index := range records {
		if records[index].Epoch > maxInt64 || records[index].LeafIndex > maxInt64 {
			return fmt.Errorf("storage: COPY row %d has an integer outside PostgreSQL BIGINT", index)
		}
	}
	copied, err := p.conn.CopyFrom(
		ctx,
		pgx.Identifier{"records"},
		recordColumns,
		pgx.CopyFromSlice(len(records), func(index int) ([]any, error) {
			r := &records[index]
			return []any{
				r.RecordID, int64(r.Epoch), r.LogType, int64(r.LeafIndex), r.ProducerID,
				r.TimestampNanos, r.C, r.T, r.AD, r.D[:], r.Sigma, r.ProducerPublicKey,
			}, nil
		}),
	)
	if err != nil {
		return fmt.Errorf("storage: COPY records: %w", err)
	}
	if copied != int64(len(records)) {
		return fmt.Errorf("storage: COPY records copied %d rows, want %d", copied, len(records))
	}
	return nil
}

const selectSQL = `SELECT record_id, epoch, log_type, leaf_index, producer_id,
	timestamp_ns, c, t, ad, d, sigma, producer_public_key
	FROM records WHERE record_id = $1`

// Get resolves a record by record_id (Q44).
func (p *Postgres) Get(ctx context.Context, recordID string) (Record, error) {
	return scanRecord(p.conn.QueryRow(ctx, selectSQL, recordID))
}

const selectManySQL = `SELECT record_id, epoch, log_type, leaf_index, producer_id,
	timestamp_ns, c, t, ad, d, sigma, producer_public_key
	FROM records WHERE record_id = ANY($1)`

// GetMany resolves a batch of records by record_id in one query and returns
// them in request order. A repeated id is returned repeatedly; an id that is
// not stored yields a *MissingError.
func (p *Postgres) GetMany(ctx context.Context, recordIDs []string) ([]Record, error) {
	if len(recordIDs) == 0 {
		return nil, nil
	}
	rows, err := p.conn.Query(ctx, selectManySQL, recordIDs)
	if err != nil {
		return nil, fmt.Errorf("storage: get many: %w", err)
	}
	defer rows.Close()
	found := make(map[string]Record, len(recordIDs))
	for rows.Next() {
		rec, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		found[rec.RecordID] = rec
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: get many: %w", err)
	}
	out := make([]Record, 0, len(recordIDs))
	for _, id := range recordIDs {
		rec, ok := found[id]
		if !ok {
			return nil, &MissingError{RecordID: id}
		}
		out = append(out, rec)
	}
	return out, nil
}

// scanRecord decodes one row of the record table.
func scanRecord(row pgx.Row) (Record, error) {
	var (
		r         Record
		epoch     int64
		leafIndex int64
		d         []byte
	)
	err := row.Scan(&r.RecordID, &epoch, &r.LogType, &leafIndex, &r.ProducerID,
		&r.TimestampNanos, &r.C, &r.T, &r.AD, &d, &r.Sigma, &r.ProducerPublicKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return Record{}, ErrNotFound
	}
	if err != nil {
		return Record{}, fmt.Errorf("storage: get: %w", err)
	}
	if len(d) != hashing.Size {
		return Record{}, fmt.Errorf("storage: stored digest is %d bytes", len(d))
	}
	r.Epoch = uint64(epoch)
	r.LeafIndex = uint64(leafIndex)
	copy(r.D[:], d)
	return r, nil
}

const selectByLeafSQL = `SELECT record_id, epoch, log_type, leaf_index, producer_id,
	timestamp_ns, c, t, ad, d, sigma, producer_public_key
	FROM records WHERE epoch = $1 AND log_type = $2 AND leaf_index = $3`

// GetByLeaf resolves a record by its leaf position within an (epoch,
// log_type) tree, served by UNIQUE (epoch, log_type, leaf_index) (Q43).
func (p *Postgres) GetByLeaf(ctx context.Context, epoch uint64, logType string, leafIndex uint64) (Record, error) {
	row := p.conn.QueryRow(ctx, selectByLeafSQL, int64(epoch), logType, int64(leafIndex))
	return scanRecord(row)
}

// Close closes the single connection.
func (p *Postgres) Close(ctx context.Context) error { return p.conn.Close(ctx) }
