package live

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"blas/alis"
	"blas/bhash"
	"blas/chain"
	"blas/query"
	"github.com/jackc/pgx/v5"
)

type Observation struct {
	Dataset   string    `json:"dataset"`
	Site      string    `json:"site"`
	Device    string    `json:"device"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Value     string    `json:"value"`
	Label     string    `json:"label"`
}

func (o Observation) Validate() error {
	if o.Dataset == "" || o.Site == "" || o.Device == "" || o.Type == "" || o.Timestamp.IsZero() || o.Value == "" || o.Label == "" {
		return errors.New("blas live: incomplete canonical observation")
	}
	return nil
}

func (o Observation) Keywords() []string {
	return []string{"dataset:" + o.Dataset, "site:" + o.Site, "device:" + o.Device, "type:" + o.Type, "label:" + o.Label}
}

type Options struct {
	RunID          string
	EntriesPerFile int
	FilesPerBlock  int
	Fanout         int
	SegmentSize    uint64
}

type Result struct{ Records, Files, Blocks uint64 }

func (o Options) validate() error {
	if o.RunID == "" || o.EntriesPerFile < 1 || o.FilesPerBlock < 1 || o.Fanout < 2 || o.SegmentSize < 1 {
		return errors.New("blas live: run ID and positive geometry are required")
	}
	return nil
}

func lidForRun(run string) uint64 {
	d := bhash.H([]byte(run))
	return binary.BigEndian.Uint64(d[:8]) & math.MaxInt64
}

// Ingest keeps only the current entry file and block's metadata in memory.
// The BLAS chain is the scheme's full-node blockchain emulator.
func Ingest(ctx context.Context, input io.Reader, conn *pgx.Conn, o Options) (Result, error) {
	if err := o.validate(); err != nil {
		return Result{}, err
	}
	if conn == nil {
		return Result{}, errors.New("blas live: PostgreSQL connection required")
	}
	provider := query.NewPostgresSP(ctx, conn)
	if err := provider.Migrate(); err != nil {
		return Result{}, err
	}
	if err := migrate(ctx, conn); err != nil {
		return Result{}, err
	}
	lid := lidForRun(o.RunID)
	_, err := conn.Exec(ctx, `INSERT INTO blas.live_runs (run_id,lid,entries_per_file,files_per_block,fanout,segment_size,status,record_count,file_count,block_count)
		VALUES ($1,$2,$3,$4,$5,$6,'ingesting',0,0,0)`, o.RunID, int64(lid), o.EntriesPerFile, o.FilesPerBlock, o.Fanout, int64(o.SegmentSize))
	if err != nil {
		return Result{}, fmt.Errorf("blas live: create run: %w", err)
	}
	gen := alis.NewLogGenerator(lid)
	ch, err := chain.NewChain(o.Fanout, o.SegmentSize)
	if err != nil {
		return Result{}, err
	}
	var result Result
	var builder *alis.EntryFileBuilder
	var inFile int
	var lastTS int64
	metas := make([]alis.FileMetadata, 0, o.FilesPerBlock)
	commitBlock := func() error {
		if len(metas) == 0 {
			return nil
		}
		if _, err := ch.AddBlock(metas); err != nil {
			return err
		}
		result.Blocks++
		metas = metas[:0]
		var snapshot bytes.Buffer
		if err := gob.NewEncoder(&snapshot).Encode(ch); err != nil {
			return err
		}
		command, err := conn.Exec(ctx, `UPDATE blas.live_runs SET record_count=$2,file_count=$3,block_count=$4,chain_snapshot=$5
			WHERE run_id=$1 AND status='ingesting'`, o.RunID, int64(result.Records), int64(result.Files), int64(result.Blocks), snapshot.Bytes())
		if err != nil {
			return err
		}
		if command.RowsAffected() != 1 {
			return errors.New("blas live: block checkpoint was not stored")
		}
		return nil
	}
	closeFile := func() error {
		if inFile == 0 {
			return nil
		}
		file, meta, err := builder.Close()
		if err != nil {
			return err
		}
		if err := provider.Store(query.FileRef{LID: lid, FID: result.Files}, file); err != nil {
			return err
		}
		result.Files++
		metas = append(metas, meta)
		ch.RegisterKeywordUniverse(lid, gen.KeywordUniverse())
		inFile = 0
		if len(metas) >= o.FilesPerBlock {
			return commitBlock()
		}
		return nil
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		line := bytes.TrimSpace(scanner.Bytes())
		var rec Observation
		if err := json.Unmarshal(line, &rec); err != nil {
			return result, fmt.Errorf("blas live: decode record %d: %w", result.Records+1, err)
		}
		if err := rec.Validate(); err != nil {
			return result, fmt.Errorf("record %d: %w", result.Records+1, err)
		}
		ts := rec.Timestamp.UnixMilli()
		if inFile > 0 && (inFile >= o.EntriesPerFile || ts < lastTS) {
			if err := closeFile(); err != nil {
				return result, err
			}
		}
		if len(metas) >= o.FilesPerBlock {
			if err := commitBlock(); err != nil {
				return result, err
			}
		}
		if inFile == 0 {
			builder, err = alis.NewEntryFileBuilder(gen, result.Files, o.Fanout)
			if err != nil {
				return result, err
			}
		}
		payload := append([]byte(nil), line...)
		if _, err := builder.AddEntry(ts, rec.Keywords(), payload); err != nil {
			return result, err
		}
		inFile++
		lastTS = ts
		result.Records++
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if result.Records == 0 {
		return result, errors.New("blas live: empty input")
	}
	if err := closeFile(); err != nil {
		return result, err
	}
	if err := commitBlock(); err != nil {
		return result, err
	}
	command, err := conn.Exec(ctx, `UPDATE blas.live_runs SET status='complete' WHERE run_id=$1 AND status='ingesting'`, o.RunID)
	if err != nil {
		return result, err
	}
	if command.RowsAffected() != 1 {
		return result, errors.New("blas live: run could not be completed")
	}
	return result, nil
}

func migrate(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS blas.live_runs (
		run_id text PRIMARY KEY, lid bigint NOT NULL UNIQUE, entries_per_file integer NOT NULL,
		files_per_block integer NOT NULL, fanout integer NOT NULL, segment_size bigint NOT NULL,
		status text NOT NULL, record_count bigint, file_count bigint, block_count bigint, chain_snapshot bytea)`)
	return err
}
