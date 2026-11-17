package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

// Each independently isolated experiment is capped by ScaleRecords at 10K.
func TestGeneratorScaleComparison(t *testing.T) {
	n := testfixture.ScaleRecords(t)
	for _, rowLimit := range []int{1000, 2000, 4000, 8000} {
		modes := []string{"device-v1"}
		if rowLimit == 1000 {
			modes = []string{"legacy", "device-v1"}
		}
		for _, mode := range modes {
			t.Run(fmt.Sprintf("%s/rows=%d", mode, rowLimit), func(t *testing.T) {
				db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
				a := lowRiskAdapter(db)
				a.Options.EntriesPerFile = 4096
				a.Options.FilesPerBlock = 256
				a.Options.SegmentSize = 1024
				a.Options.ObjectBytes = 4 << 20
				a.Options.QueryBytes = 256 << 20
				a.Options.Index.PageBytes = 64 << 10
				a.Options.Index.BitmapBytes = 32 << 20
				if mode != "legacy" {
					a.Options.GeneratorMode = "device-v1"
					a.CompactZeroBitmaps = true
				}
				a.Execution.BatchMaxRecords = rowLimit
				if err := a.Execution.ConfigureRuntime(); err != nil {
					t.Fatal(err)
				}
				if err := a.Migrate(context.Background()); err != nil {
					t.Fatal(err)
				}
				db.Close()
				m := &contract.IngestMetrics{}
				ctx := contract.WithIngestMetrics(context.Background(), m)
				var err error
				db, err = contract.OpenPostgres(ctx, dsn, 6)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				a.DB = db
				a.Metrics = m
				p := contract.Progress{Target: uint64(n)}
				testfixture.Measure(t, "BLAS", fmt.Sprintf("%s-%d", mode, rowLimit), n, *a.Execution, m, db, func() {
					for p.Committed < uint64(n) {
						count := min(1000, int(uint64(n)-p.Committed))
						es := testfixture.Entries(p.Committed+1, count)
						tx, err := db.BeginTx(ctx, nil)
						if err != nil {
							t.Fatal(err)
						}
						_, cp, err := a.Apply(ctx, tx, p, es)
						if err != nil {
							tx.Rollback()
							t.Fatal(err)
						}
						if err := tx.Commit(); err != nil {
							t.Fatal(err)
						}
						p.Committed += uint64(count)
						p.Checkpoint = cp
						bytes := 0
						for _, e := range es {
							bytes += len(e.Record)
						}
						m.Committed(count, bytes)
					}
				})
				access := m.Access.Snapshot()
				calls := m.SQL.Count.Load()
				var files, keywords, leafCount, zeroCount int
				var native state
				json.Unmarshal(p.Checkpoint, &native)
				filesPerGenerator := map[string]uint64{}
				registeredGeneratorKeywords := native.NextKID
				if mode != "legacy" {
					registeredGeneratorKeywords = 0
					for _, g := range native.Generators {
						filesPerGenerator[fmt.Sprint(g.LID)] = g.Sequence
						registeredGeneratorKeywords += g.NextKID
					}
				} else {
					filesPerGenerator[fmt.Sprint(a.lid())] = native.Files
				}
				var mean, median float64
				if err := db.QueryRow(`SELECT count(*),avg(entries),percentile_cont(0.5) WITHIN GROUP(ORDER BY entries) FROM blas.durable_files`).Scan(&files, &mean, &median); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT count(*) FROM blas.durable_keywords`).Scan(&keywords); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT count(*) FROM blas.index_leaves WHERE tree LIKE 'file/%/iimt'`).Scan(&leafCount); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT count(*) FROM blas.index_pages WHERE tree LIKE 'file/%/iimt' AND ((page LIKE 'bitmap/%' AND payload=decode(repeat('00',octet_length(payload)),'hex')) OR (page LIKE 'leaf/%' AND convert_from(payload,'UTF8')::jsonb->>'Zero'='true'))`).Scan(&zeroCount); err != nil {
					t.Fatal(err)
				}
				summary := map[string]any{"mode": mode, "batch_rows": rowLimit, "records": n, "files": files, "mean_records_per_file": mean, "median_records_per_file": median, "registered_global_keywords": keywords, "file_index_rows": access.FileIndexRows, "file_index_rows_per_record": float64(access.FileIndexRows) / float64(n), "zero_keyword_percent": 100 * float64(zeroCount) / float64(leafCount), "sql_calls": calls, "index_insert_calls": access.IndexInsertCalls, "index_rows_per_insert": float64(access.IndexPageRows+access.IndexLeafRows) / float64(access.IndexInsertCalls), "index_payload_bytes_per_insert": float64(access.IndexPayloadBytes) / float64(access.IndexInsertCalls), "access": access}
				b, _ := json.Marshal(summary)
				summary["files_per_generator"] = filesPerGenerator
				summary["registered_generator_keyword_rows"] = registeredGeneratorKeywords
				b, _ = json.Marshal(summary)
				t.Log("GENERATOR_COMPARISON " + string(b))
				if access.PeakIndexBatchRows > int64(rowLimit) || access.PeakIndexBatchBytes > int64(a.Execution.BatchMaxBytes) {
					t.Fatal("persistence buffer bound exceeded")
				}
				var records int
				if err := db.QueryRow(`SELECT count(*) FROM blas.durable_objects WHERE kind='entry'`).Scan(&records); err != nil || records != n {
					t.Fatal("lost/duplicate entries", records, err)
				}
			})
		}
	}
}
