package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"blas/alis"
	"blas/chain"
	"blas/internal/diskindex"
	"blas/iomf"
	"blas/live"
	"blas/query"
	"blas/verify"
	contract "canonicalingestion"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestIncrementalRootsAndProofs(t *testing.T) {
	dsn := os.Getenv("BLAS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BLAS_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	for _, aggregate := range []bool{false, true} {
		t.Run(fmt.Sprint(aggregate), func(t *testing.T) {
			db, err := sql.Open("pgx", dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			run := fmt.Sprintf("durable-test-%d", time.Now().UnixNano())
			options := Options{RunID: run, EntriesPerFile: 4, FilesPerBlock: 3, Fanout: 4, SegmentSize: 3, Aggregation: aggregate, Index: diskindex.Limits{PageBytes: 1024, CacheBytes: 8192, CacheEntries: 20, BitmapBytes: 4096}, ObjectBytes: 1 << 20, QueryBytes: 8 << 20}
			a := Adapter{DB: db, Options: options}
			if os.Getenv("BLAS_TEST_ACCESS_OPTIMIZED") == "1" {
				e := contract.DefaultExecution()
				a.Execution = &e
			}
			if os.Getenv("BLAS_TEST_SEGMENT_COMPUTATIONS") == "1" {
				a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
			}
			if err = a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				for _, table := range []string{"durable_objects", "durable_keywords", "durable_files", "durable_headers"} {
					db.Exec(`DELETE FROM blas.`+table+` WHERE run_id=$1`, run)
				}
				db.Exec(`DELETE FROM blas.index_pages WHERE scope=$1`, run)
				db.Exec(`DELETE FROM blas.index_leaves WHERE scope=$1`, run)
			}()
			entries := make([]contract.Entry, 43)
			for i := range entries {
				dataset := []string{"WADI", "HAI", "SWAT"}[i%3]
				source := contract.SourceObservation{Dataset: dataset, File: dataset + "/raw/test.csv", Row: uint64(i + 1), Column: 2}
				observation := live.Observation{Dataset: dataset, Site: dataset + "/site", Device: "sensor", Type: "sensor", Timestamp: time.Unix(1700000000+int64(i%7), 0).UTC(), Value: fmt.Sprint(i), Label: "normal"}
				raw, _ := json.Marshal(struct {
					live.Observation
					Source contract.SourceObservation `json:"source"`
				}{observation, source})
				entries[i] = contract.Entry{Ordinal: uint64(i + 1), SourceID: source.ID(), Record: raw}
			}
			progress := contract.Progress{Target: 17}
			oracle, _ := chain.NewChainWithOptions(4, 3, aggregate)
			generator := alis.NewLogGenerator(a.lid())
			var oracleFiles []*alis.EntryFile
			var metas []alis.FileMetadata
			var builder *alis.EntryFileBuilder
			inFile := 0
			lastTS := int64(0)
			closeBlock := func() {
				if len(metas) > 0 {
					if _, e := oracle.AddBlock(metas); e != nil {
						t.Fatal(e)
					}
					metas = nil
				}
			}
			closeFile := func() {
				if inFile > 0 {
					f, m, e := builder.Close()
					if e != nil {
						t.Fatal(e)
					}
					oracleFiles = append(oracleFiles, f)
					metas = append(metas, m)
					inFile = 0
					oracle.RegisterKeywordUniverse(a.lid(), generator.KeywordUniverse())
					if len(metas) == 3 {
						closeBlock()
					}
				}
			}
			applyOracle := func(es []contract.Entry, finish bool) {
				for _, e := range es {
					var o live.Observation
					json.Unmarshal(e.Record, &o)
					ts := o.Timestamp.UnixMilli()
					if inFile > 0 && (inFile == 4 || ts < lastTS) {
						closeFile()
					}
					if inFile == 0 {
						builder, _ = alis.NewEntryFileBuilder(generator, uint64(len(oracleFiles)), 4)
					}
					if _, err := builder.AddEntry(ts, o.Keywords(), e.Record); err != nil {
						t.Fatal(err)
					}
					inFile++
					lastTS = ts
					if inFile == 4 {
						closeFile()
					}
				}
				if finish {
					closeFile()
					closeBlock()
				}
			}
			// Failure after native objects/index pages were written but before commit.
			tx, _ := db.BeginTx(ctx, nil)
			if _, _, err = a.Apply(ctx, tx, progress, entries[:9]); err != nil {
				t.Fatal(err)
			}
			tx.Rollback()
			var count int
			db.QueryRow(`SELECT count(*) FROM blas.durable_objects WHERE run_id=$1`, run).Scan(&count)
			if count != 0 {
				t.Fatal("rollback retained native records")
			}
			var nativeIDs []string
			for _, cut := range []int{6, 17, 24, 43} {
				start := int(progress.Committed)
				if cut > 17 {
					progress.Target = 43
				}
				tx, err = db.BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				ids, checkpoint, e := a.Apply(ctx, tx, progress, entries[start:cut])
				if e != nil {
					tx.Rollback()
					t.Fatal(e)
				}
				if len(ids) != cut-start {
					t.Fatal("lost native mappings")
				}
				if err = tx.Commit(); err != nil {
					t.Fatal(err)
				}
				nativeIDs = append(nativeIDs, ids...)
				applyOracle(entries[start:cut], uint64(cut) == progress.Target)
				progress.Committed = uint64(cut)
				progress.Checkpoint = checkpoint
				// A fresh SQL connection/adapter restores only the bounded checkpoint.
				reopened, e := sql.Open("pgx", dsn)
				if e != nil {
					t.Fatal(e)
				}
				a.DB = reopened
				r := a.NewReader(ctx, nil)
				gotCount, e := r.BlockCount()
				if e != nil || gotCount != uint64(len(oracle.Blocks())) {
					t.Fatal(gotCount, e, len(oracle.Blocks()))
				}
				for i, b := range oracle.Blocks() {
					got, e := r.Header(uint64(i))
					if e != nil || !reflect.DeepEqual(b.Header, got) {
						t.Fatalf("root/header changed at block %d: %v\nwant %+v\ngot %+v", i, e, b.Header, got)
					}
				}
				reopened.Close()
				a.DB = db
			}
			cache, _ := diskindex.NewCache(options.Index)
			control := contract.Store{DB: db}
			if err = control.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			consumer := contract.Consumer{Store: control, ID: run, StreamID: "fixture", Fingerprint: "fixture-v1", ConfigHash: options.ConfigHash()}
			if err = consumer.Open(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() {
				db.Exec(`DELETE FROM canonical_ingestion.mappings WHERE consumer_id=$1`, run)
				db.Exec(`DELETE FROM canonical_ingestion.consumers WHERE consumer_id=$1`, run)
			}()
			for i, e := range entries {
				hash := e.PayloadHash()
				if _, err = db.Exec(`INSERT INTO canonical_ingestion.mappings(consumer_id,ordinal,source_id,native_id,payload_hash) VALUES($1,$2,$3,$4,$5)`, run, int64(e.Ordinal), e.SourceID, nativeIDs[i], hash[:]); err != nil {
					t.Fatal(err)
				}
			}
			metrics, e := a.Audit(ctx, consumer, contract.AuditRequest{Version: 1, StreamID: "fixture", Fingerprint: "fixture-v1", ExpectedN: 43, Entries: entries[:3]}, cache, true)
			if e != nil || metrics.RequestedRecords != 3 || metrics.EvidenceBytes == 0 || metrics.BitmapBytes == 0 || metrics.StorageReads == 0 {
				t.Fatal("streamed native audit", metrics, e)
			}
			node := query.NewFullNode(oracle)
			for _, hasRange := range []bool{false, true} {
				r := a.NewReader(ctx, cache)
				q := query.QOn{LG: a.lid(), HasRange: hasRange, TS: 1700000001000, TE: 1700000004000}
				want, e := node.Stage1(q)
				if e != nil {
					t.Fatal(e)
				}
				got, e := r.Stage1(q, false)
				if e != nil || !reflect.DeepEqual(want, got) {
					t.Fatal("stage1 proof changed", e)
				}
				want2, e := node.Stage2(q, want.CandidateHeights)
				if e != nil {
					t.Fatal(e)
				}
				got2, e := r.Stage2(q, got.CandidateHeights)
				if e != nil || !reflect.DeepEqual(want2, got2) {
					t.Fatal("stage2 proof changed", e)
				}
				trusted := a.NewReader(ctx, cache)
				aud, e := verify.NewStoredAuditor(len(oracle.Blocks()), 3, trusted.Header)
				if e != nil {
					t.Fatal(e)
				}
				ids, ok := aud.VerifyStage1(q, got, false)
				if !ok {
					t.Fatal("stored header stage1 rejected", aud.Err())
				}
				if _, ok = aud.VerifyStage2(q, ids, got2); !ok {
					t.Fatal("stored header stage2 rejected", aud.Err())
				}
				if aggregate {
					wantAgg, wantBindings, e := node.Stage2Aggregated(q, want)
					if e != nil {
						t.Fatal(e)
					}
					gotAgg, gotBindings, e := r.Stage2Aggregated(q, got)
					if e != nil || !reflect.DeepEqual(wantAgg, gotAgg) || !reflect.DeepEqual(wantBindings, gotBindings) {
						t.Fatalf("aggregated proof changed: %v; resultEqual=%v bindingsEqual=%v", e, reflect.DeepEqual(wantAgg, gotAgg), reflect.DeepEqual(wantBindings, gotBindings))
					}
					if _, ok := aud.VerifyStage2Aggregated(q, ids, gotAgg, gotBindings); !ok {
						t.Fatal("stored aggregated verification rejected", aud.Err())
					}
				}
				optimized, e := r.Stage1(q, true)
				if e != nil {
					t.Fatal(e)
				}
				wantOptimized, e := node.Stage1Optimized(q)
				if e != nil || !reflect.DeepEqual(wantOptimized, optimized) {
					t.Fatal("backward scan changed", e)
				}
			}
			for fid, f := range oracleFiles {
				for _, word := range []string{"dataset:WADI", "missing"} {
					r := a.NewReader(ctx, cache)
					q := alis.EntryQuery{EK: iomf.Keyword(word), HasRange: true, TS: 1700000001000, TE: 1700000004000}
					if word == "missing" {
						q.HasRange = false
					}
					want, e := f.ProcessQuery(q)
					if e != nil {
						t.Fatal(e)
					}
					got, e := r.Entry(uint64(fid), q)
					if e != nil || !reflect.DeepEqual(want, got) {
						t.Fatal("entry proof changed", fid, e)
					}
					ir, _ := f.IIMT.Root()
					or, _ := f.OMT.Root()
					if !alis.VerifyEntryQuery(ir, or, q, got) {
						t.Fatal("entry verification changed")
					}
				}
			}
			if s := cache.Stats(); s.CacheBytes > 8192 || s.CacheEntries > 20 {
				t.Fatal(s)
			}
		})
	}
}
