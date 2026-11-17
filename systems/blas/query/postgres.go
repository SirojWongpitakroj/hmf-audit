package query

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"math"

	"blas/alis"
	"github.com/jackc/pgx/v5"
)

// PostgresSP is BLAS's durable off-chain service provider. Files are decoded
// only for the current request and are discarded after producing the VO.
type PostgresSP struct {
	ctx         context.Context
	conn        *pgx.Conn
	reads       int
	loadedBytes int
}

var _ StorageProvider = (*PostgresSP)(nil)

func NewPostgresSP(ctx context.Context, conn *pgx.Conn) *PostgresSP {
	return &PostgresSP{ctx: ctx, conn: conn}
}

func OpenPostgresSP(ctx context.Context, dsn string) (*PostgresSP, error) {
	if dsn == "" {
		return nil, errors.New("query: PostgreSQL DSN is required")
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	provider := NewPostgresSP(ctx, conn)
	if err := provider.Migrate(); err != nil {
		_ = conn.Close(ctx)
		return nil, err
	}
	return provider, nil
}

func (sp *PostgresSP) Close() error { return sp.conn.Close(sp.ctx) }

func (sp *PostgresSP) Migrate() error {
	if _, err := sp.conn.Exec(sp.ctx, "CREATE SCHEMA IF NOT EXISTS blas"); err != nil {
		return err
	}
	_, err := sp.conn.Exec(sp.ctx, `
		CREATE TABLE IF NOT EXISTS blas.entry_files (
			lid bigint NOT NULL,
			fid bigint NOT NULL,
			payload bytea NOT NULL,
			PRIMARY KEY (lid, fid)
		)
	`)
	return err
}

func (sp *PostgresSP) Store(ref FileRef, file *alis.EntryFile) error {
	if ref.LID > math.MaxInt64 || ref.FID > math.MaxInt64 || file == nil {
		return errors.New("query: invalid file key or nil file")
	}
	var data bytes.Buffer
	if err := gob.NewEncoder(&data).Encode(file); err != nil {
		return err
	}
	command, err := sp.conn.Exec(sp.ctx, `
		INSERT INTO blas.entry_files (lid, fid, payload) VALUES ($1, $2, $3)
		ON CONFLICT (lid, fid) DO NOTHING`,
		int64(ref.LID), int64(ref.FID), data.Bytes())
	if err != nil {
		return err
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("query: file %+v already exists", ref)
	}
	return nil
}

// Stats reports database reads and bytes decoded by this provider. Create a
// fresh provider for each request to get per-request counts.
func (sp *PostgresSP) Stats() (reads, loadedBytes int) { return sp.reads, sp.loadedBytes }

func (sp *PostgresSP) AnswerQOff(refs []FileRef, qoff QOff) ([]FileResult, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	fidsByLID := make(map[uint64][]int64)
	lidOrder := make([]uint64, 0)
	for _, ref := range refs {
		if ref.LID > math.MaxInt64 || ref.FID > math.MaxInt64 {
			return nil, fmt.Errorf("query: file key overflows PostgreSQL bigint: %+v", ref)
		}
		if _, found := fidsByLID[ref.LID]; !found {
			lidOrder = append(lidOrder, ref.LID)
		}
		fidsByLID[ref.LID] = append(fidsByLID[ref.LID], int64(ref.FID))
	}
	files := make(map[FileRef]*alis.EntryFile, len(refs))
	for _, lid := range lidOrder {
		rows, err := sp.conn.Query(sp.ctx,
			"SELECT lid, fid, payload FROM blas.entry_files WHERE lid = $1 AND fid = ANY($2::bigint[])",
			int64(lid), fidsByLID[lid])
		sp.reads++
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var storedLID, storedFID int64
			var payload []byte
			if err := rows.Scan(&storedLID, &storedFID, &payload); err != nil {
				rows.Close()
				return nil, err
			}
			if storedLID < 0 || storedFID < 0 {
				rows.Close()
				return nil, errors.New("query: PostgreSQL returned a negative file key")
			}
			var file alis.EntryFile
			if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&file); err != nil {
				rows.Close()
				return nil, fmt.Errorf("query: decode file (%d,%d): %w", storedLID, storedFID, err)
			}
			files[FileRef{LID: uint64(storedLID), FID: uint64(storedFID)}] = &file
			sp.loadedBytes += len(payload)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
	}
	results := make([]FileResult, 0, len(refs))
	for _, ref := range refs {
		file, found := files[ref]
		if !found {
			return nil, fmt.Errorf("query: PostgreSQL has no file for %+v", ref)
		}
		vo, err := file.ProcessQuery(alis.EntryQuery{
			EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE,
		})
		if err != nil {
			return nil, err
		}
		results = append(results, FileResult{Ref: ref, VO: vo})
	}
	return results, nil
}
