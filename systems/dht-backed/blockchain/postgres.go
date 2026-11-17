package blockchain

import (
	"context"
	"errors"
	"fmt"

	"dhtbacked/internal/hashing"
	"github.com/jackc/pgx/v5"
)

// PostgresAnchor is a durable append-only test anchor for live experiments.
// It models the contract's no-overwrite rule but is not an independent chain.
type PostgresAnchor struct{ conn *pgx.Conn }

func OpenPostgresAnchor(ctx context.Context, dsn string) (*PostgresAnchor, error) {
	if dsn == "" {
		return nil, errors.New("blockchain: PostgreSQL DSN required")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	_, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS dht_epoch_anchors (
		epoch bigint PRIMARY KEY, root bytea NOT NULL CHECK (octet_length(root)=32),
		capacity bigint NOT NULL, record_count bigint NOT NULL, committed_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		conn.Close(ctx)
		return nil, err
	}
	return &PostgresAnchor{conn: conn}, nil
}

func (a *PostgresAnchor) Close(ctx context.Context) error { return a.conn.Close(ctx) }

func (a *PostgresAnchor) Commit(ctx context.Context, epoch uint64, root hashing.Digest) error {
	return a.CommitWithCounts(ctx, epoch, root, 0, 0)
}

func (a *PostgresAnchor) CommitWithCounts(ctx context.Context, epoch uint64, root hashing.Digest, capacity, count uint64) error {
	if epoch > 1<<63-1 || capacity > 1<<63-1 || count > 1<<63-1 {
		return errors.New("blockchain: epoch/count overflows PostgreSQL bigint")
	}
	_, err := a.conn.Exec(ctx, `INSERT INTO dht_epoch_anchors (epoch,root,capacity,record_count) VALUES ($1,$2,$3,$4)`, int64(epoch), root[:], int64(capacity), int64(count))
	if err != nil {
		return fmt.Errorf("blockchain: commit epoch %d: %w", epoch, err)
	}
	return nil
}

func (a *PostgresAnchor) GetRoot(ctx context.Context, epoch uint64) (hashing.Digest, error) {
	var root hashing.Digest
	if epoch > 1<<63-1 {
		return root, ErrNotAnchored
	}
	var raw []byte
	err := a.conn.QueryRow(ctx, `SELECT root FROM dht_epoch_anchors WHERE epoch=$1`, int64(epoch)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return root, ErrNotAnchored
	}
	if err != nil {
		return root, err
	}
	if len(raw) != len(root) {
		return root, errors.New("blockchain: invalid stored root length")
	}
	copy(root[:], raw)
	return root, nil
}

func (a *PostgresAnchor) Geometry(ctx context.Context, epoch uint64) (uint64, uint64, error) {
	var capacity, count int64
	err := a.conn.QueryRow(ctx, `SELECT capacity,record_count FROM dht_epoch_anchors WHERE epoch=$1`, int64(epoch)).Scan(&capacity, &count)
	if err != nil {
		return 0, 0, err
	}
	if capacity <= 0 || count <= 0 || count > capacity {
		return 0, 0, errors.New("blockchain: invalid stored geometry")
	}
	return uint64(capacity), uint64(count), nil
}
