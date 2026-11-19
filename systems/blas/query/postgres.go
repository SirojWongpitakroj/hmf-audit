package query

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"math"
	"sync"

	"blas/alis"
	"github.com/jackc/pgx/v5"
)

// PostgresSP is BLAS's durable off-chain service provider. Files are decoded
// only for the current request and are discarded after producing the VO.
type PostgresSP struct {
	ctx         context.Context
	conn        *pgx.Conn
	workers     []*pgx.Conn // one connection per concurrent reader; nil means sequential on conn
	reads       int
	loadedBytes int
}

var _ StorageProvider = (*PostgresSP)(nil)

func NewPostgresSP(ctx context.Context, conn *pgx.Conn) *PostgresSP {
	return &PostgresSP{ctx: ctx, conn: conn}
}

// NewConcurrentPostgresSP reads, decodes and processes entry files with one
// goroutine per connection in conns (a pgx.Conn is not safe for concurrent
// use). Open the connections once and reuse them across requests, so that
// connection setup is not part of a request. With fewer than two connections
// it behaves like NewPostgresSP.
func NewConcurrentPostgresSP(ctx context.Context, conns []*pgx.Conn) *PostgresSP {
	sp := &PostgresSP{ctx: ctx, conn: conns[0]}
	if len(conns) > 1 {
		sp.workers = conns
	}
	return sp
}

// OpenWorkerConns opens n connections to the database conn is connected to.
func OpenWorkerConns(ctx context.Context, conn *pgx.Conn, n int) ([]*pgx.Conn, error) {
	conns := []*pgx.Conn{conn}
	for len(conns) < n {
		extra, err := pgx.ConnectConfig(ctx, conn.Config())
		if err != nil {
			for _, c := range conns[1:] {
				_ = c.Close(ctx)
			}
			return nil, err
		}
		conns = append(conns, extra)
	}
	return conns, nil
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
	entryQuery := alis.EntryQuery{EK: qoff.EK, HasRange: qoff.HasRange, TS: qoff.TS, TE: qoff.TE}
	if len(sp.workers) > 1 {
		for _, ref := range refs {
			if ref.LID > math.MaxInt64 || ref.FID > math.MaxInt64 {
				return nil, fmt.Errorf("query: file key overflows PostgreSQL bigint: %+v", ref)
			}
		}
		return sp.answerConcurrent(refs, entryQuery)
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
		loaded, err := fetchFiles(sp.ctx, sp.conn, lid, fidsByLID[lid])
		sp.reads++
		sp.loadedBytes += loaded.bytes
		if err != nil {
			return nil, err
		}
		for ref, file := range loaded.files {
			files[ref] = file
		}
	}
	results := make([]FileResult, 0, len(refs))
	for _, ref := range refs {
		file, found := files[ref]
		if !found {
			return nil, fmt.Errorf("query: PostgreSQL has no file for %+v", ref)
		}
		vo, err := file.ProcessQuery(entryQuery)
		if err != nil {
			return nil, err
		}
		results = append(results, FileResult{Ref: ref, VO: vo})
	}
	return results, nil
}

type loadedFiles struct {
	files map[FileRef]*alis.EntryFile
	bytes int
}

// fetchFiles runs one batched read for the given fids of one log generator and
// decodes every returned file.
func fetchFiles(ctx context.Context, conn *pgx.Conn, lid uint64, fids []int64) (loadedFiles, error) {
	out := loadedFiles{files: make(map[FileRef]*alis.EntryFile, len(fids))}
	rows, err := conn.Query(ctx,
		"SELECT lid, fid, payload FROM blas.entry_files WHERE lid = $1 AND fid = ANY($2::bigint[])",
		int64(lid), fids)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var storedLID, storedFID int64
		var payload []byte
		if err := rows.Scan(&storedLID, &storedFID, &payload); err != nil {
			return out, err
		}
		if storedLID < 0 || storedFID < 0 {
			return out, errors.New("query: PostgreSQL returned a negative file key")
		}
		var file alis.EntryFile
		if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&file); err != nil {
			return out, fmt.Errorf("query: decode file (%d,%d): %w", storedLID, storedFID, err)
		}
		out.files[FileRef{LID: uint64(storedLID), FID: uint64(storedFID)}] = &file
		out.bytes += len(payload)
	}
	return out, rows.Err()
}

// answerConcurrent splits the requested files into one contiguous chunk per
// connection; each worker reads its chunk with one batched query, decodes it
// and runs the entry-level query, so the results keep the order of refs.
func (sp *PostgresSP) answerConcurrent(refs []FileRef, entryQuery alis.EntryQuery) ([]FileResult, error) {
	type job struct {
		lid  uint64
		refs []FileRef // their results go to results[from:from+len(refs)]
		fids []int64
		from int
	}
	var jobs []job
	workers := min(len(sp.workers), len(refs))
	chunk := (len(refs) + workers - 1) / workers
	for from := 0; from < len(refs); {
		// A chunk never spans two log generators: end it at the next LID change.
		to := min(from+chunk, len(refs))
		for end := from + 1; end < to; end++ {
			if refs[end].LID != refs[from].LID {
				to = end
				break
			}
		}
		j := job{lid: refs[from].LID, refs: refs[from:to], from: from}
		for _, ref := range j.refs {
			j.fids = append(j.fids, int64(ref.FID))
		}
		jobs = append(jobs, j)
		from = to
	}
	results := make([]FileResult, len(refs))
	errs := make([]error, len(jobs))
	loadedBytes := make([]int, len(jobs))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(len(sp.workers), len(jobs)); w++ {
		wg.Add(1)
		go func(conn *pgx.Conn) {
			defer wg.Done()
			for index := range next {
				j := jobs[index]
				loaded, err := fetchFiles(sp.ctx, conn, j.lid, j.fids)
				loadedBytes[index] = loaded.bytes
				if err != nil {
					errs[index] = err
					continue
				}
				for offset, ref := range j.refs {
					file, found := loaded.files[ref]
					if !found {
						errs[index] = fmt.Errorf("query: PostgreSQL has no file for %+v", ref)
						break
					}
					vo, err := file.ProcessQuery(entryQuery)
					if err != nil {
						errs[index] = err
						break
					}
					results[j.from+offset] = FileResult{Ref: ref, VO: vo}
				}
			}
		}(sp.workers[w])
	}
	for index := range jobs {
		next <- index
	}
	close(next)
	wg.Wait()
	sp.reads += len(jobs)
	for index, err := range errs {
		sp.loadedBytes += loadedBytes[index]
		if err != nil {
			return nil, err
		}
	}
	return results, nil
}
