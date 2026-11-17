package blockchain

import (
	"context"
	"database/sql"
	"errors"

	"dhtbacked/internal/hashing"
)

// SQLAnchor is the same append-only PostgreSQL ledger emulator as
// PostgresAnchor, placed in the dedicated schema of the shared baseline RDS.
// It is not an independently operated blockchain.
type SQLAnchor struct {
	DB              *sql.DB
	Reads           uint64
	Rows, ReadBytes uint64
}

func (a *SQLAnchor) GetRoot(ctx context.Context, epoch uint64) (hashing.Digest, error) {
	a.Reads++
	var raw []byte
	var h hashing.Digest
	err := a.DB.QueryRowContext(ctx, `SELECT root FROM dhtbacked.dht_epoch_anchors WHERE epoch=$1`, int64(epoch)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrNotAnchored
	}
	if err != nil {
		return h, err
	}
	if len(raw) != 32 {
		return h, errors.New("blockchain: invalid stored root")
	}
	a.Rows++
	a.ReadBytes += 32
	copy(h[:], raw)
	return h, nil
}
func (a *SQLAnchor) Geometry(ctx context.Context, epoch uint64) (uint64, uint64, error) {
	a.Reads++
	var capacity, count uint64
	err := a.DB.QueryRowContext(ctx, `SELECT capacity,record_count FROM dhtbacked.dht_epoch_anchors WHERE epoch=$1`, int64(epoch)).Scan(&capacity, &count)
	if err != nil {
		return 0, 0, err
	}
	if capacity < 2 || count == 0 || count > capacity {
		return 0, 0, errors.New("blockchain: invalid stored epoch geometry")
	}
	a.Rows++
	a.ReadBytes += 16
	return capacity, count, nil
}
