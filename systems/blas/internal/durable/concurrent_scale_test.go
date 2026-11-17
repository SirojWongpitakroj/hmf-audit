package durable

import (
	"context"
	"os"
	"testing"

	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestConcurrentBLASScale(t *testing.T) {
	n := testfixture.ScaleRecords(t)
	for _, mode := range testfixture.Modes() {
		t.Run(mode, func(t *testing.T) {
			db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			e := testfixture.Execution(t)
			m := &contract.IngestMetrics{}
			ctx := contract.WithIngestMetrics(context.Background(), m)
			db.Close()
			db, err := contract.OpenPostgres(ctx, dsn, e.SQLPoolSize)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			a := Adapter{DB: db, Options: Options{RunID: "scale-fixture", EntriesPerFile: 4096, FilesPerBlock: 256, Fanout: 4, SegmentSize: 1024, Index: diskindex.Limits{PageBytes: 64 << 10, CacheBytes: 64 << 20, CacheEntries: 4096, BitmapBytes: 32 << 20}, ObjectBytes: 4 << 20, QueryBytes: 256 << 20}, Metrics: m}
			if err := a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			fn := a.applySequentialReference
			if mode == "new" {
				a.Execution = &e
				a.DisableCompatibleIndexBatching = os.Getenv("BLAS_GROUP_INDEX_WRITES") == "false"
				if os.Getenv("BLAS_SEGMENT_COMPUTATION_REUSE") != "false" {
					a.SegmentComputations = diskindex.NewComputationCache(e.CacheMaxEntries, e.CacheMaxBytes)
				}
				fn = a.Apply
			}
			p := contract.Progress{Target: uint64(n)}
			testfixture.Measure(t, "BLAS", mode, n, e, m, db, func() {
				for p.Committed < uint64(n) {
					count := min(e.BatchMaxRecords, int(uint64(n)-p.Committed))
					entries := testfixture.Entries(p.Committed+1, count)
					tx, err := db.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					_, checkpoint, err := fn(ctx, tx, p, entries)
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					p.Committed += uint64(count)
					p.Checkpoint = checkpoint
					bytes := 0
					for _, entry := range entries {
						bytes += len(entry.Record)
					}
					m.Committed(count, bytes)
				}
			})
			var count int
			if err := db.QueryRow(`SELECT count(*) FROM blas.durable_objects WHERE kind='entry'`).Scan(&count); err != nil || count != n {
				t.Fatal("record count", count, err)
			}
		})
	}
}
