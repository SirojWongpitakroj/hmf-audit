package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"blas/live"
	"github.com/jackc/pgx/v5"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	mode := flag.String("mode", "ingest", "ingest or verify")
	runID := flag.String("run", "", "unique run identifier")
	input := flag.String("input", "-", "canonical JSONL file or - for stdin")
	dsn := flag.String("pg-dsn", os.Getenv("BLAS_POSTGRES_DSN"), "PostgreSQL DSN")
	entries := flag.Int("entries-per-file", 1<<12, "maximum entries per immutable file")
	files := flag.Int("files-per-block", 256, "maximum files per block")
	fanout := flag.Int("fanout", 4, "Merkle tree fanout")
	segment := flag.Uint64("segment-size", 1024, "blocks per segment")
	flag.Parse()
	if *runID == "" || *dsn == "" {
		return fmt.Errorf("-run and -pg-dsn (or BLAS_POSTGRES_DSN) are required")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, *dsn)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	var result live.Result
	switch *mode {
	case "ingest":
		f := os.Stdin
		if *input != "-" {
			f, err = os.Open(*input)
			if err != nil {
				return err
			}
			defer f.Close()
		}
		result, err = live.Ingest(ctx, f, conn, live.Options{RunID: *runID, EntriesPerFile: *entries, FilesPerBlock: *files, Fanout: *fanout, SegmentSize: *segment})
	case "verify":
		result, err = live.Verify(ctx, conn, *runID)
	default:
		return fmt.Errorf("unknown mode %q", *mode)
	}
	if err != nil {
		return err
	}
	fmt.Printf("BLAS %s run=%s records=%d files=%d blocks=%d\n", *mode, *runID, result.Records, result.Files, result.Blocks)
	return nil
}
