package durable

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"blas/alis"
	"blas/bhash"
	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func lowRiskAdapter(db *sql.DB) Adapter {
	e := contract.DefaultExecution()
	e.CPUWorkers, e.MaxProcs, e.IOWorkers, e.SQLPoolSize = 2, 2, 8, 6
	return Adapter{DB: db, Execution: &e, Metrics: &contract.IngestMetrics{}, Options: Options{RunID: "low-risk", EntriesPerFile: 16, FilesPerBlock: 4, Fanout: 4, SegmentSize: 4, Index: diskindex.Limits{PageBytes: 4096, BitmapBytes: 1 << 20}, ObjectBytes: 1 << 20, QueryBytes: 16 << 20}}
}

func nativeSnapshot(t *testing.T, db *sql.DB) string {
	return testfixture.Snapshot(t, db,
		`SELECT kid,keyword FROM blas.durable_keywords ORDER BY kid`,
		`SELECT kind,owner,oid,object,keywords,lids,ts_min,ts_max FROM blas.durable_objects ORDER BY kind,owner,oid`,
		`SELECT fid,block_height,entries,metadata FROM blas.durable_files ORDER BY fid`,
		`SELECT height,segment,payload FROM blas.durable_headers ORDER BY height`,
		`SELECT * FROM blas.index_pages ORDER BY scope,tree,page`,
		`SELECT * FROM blas.index_leaves ORDER BY scope,tree,pos`)
}

// Both paths use the previous concurrent/access-optimized implementation. Only
// the new low-risk switch differs, including after a stored milestone/reopen.
func TestLowRiskMatchesPreviousConcurrentStateAndRestart(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		t.Run(fmt.Sprint(aggregate), func(t *testing.T) {
			var snapshots [3]string
			var checkpoints [3][]byte
			var ids [3][]string
			for mode := range 3 {
				db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
				a := lowRiskAdapter(db)
				a.Options.Aggregation = aggregate
				a.DisableLowRiskOptimizations = true
				if err := a.Migrate(context.Background()); err != nil {
					t.Fatal(err)
				}
				hash := a.Options.ConfigHash()
				p := contract.Progress{Target: 100}
				step := func(n int, commit bool) {
					t.Helper()
					tx, err := a.DB.Begin()
					if err != nil {
						t.Fatal(err)
					}
					got, cp, err := a.Apply(context.Background(), tx, p, testfixture.Entries(p.Committed+1, n))
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					if !commit {
						tx.Rollback()
						return
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					p.Committed += uint64(n)
					p.Checkpoint = cp
					ids[mode] = append(ids[mode], got...)
				}
				step(100, true)
				var old state
				if err := json.Unmarshal(p.Checkpoint, &old); err != nil {
					t.Fatal(err)
				}
				prefixQueries := []string{fmt.Sprintf(`SELECT fid,metadata FROM blas.durable_files WHERE fid<%d ORDER BY fid`, old.Files), fmt.Sprintf(`SELECT height,payload FROM blas.durable_headers WHERE height<%d ORDER BY height`, old.Blocks)}
				prefix := testfixture.Snapshot(t, db, prefixQueries...)
				a.DisableLowRiskOptimizations = mode == 0
				if mode == 2 {
					a.Execution.CacheMaxBytes = 256
					a.Execution.CacheMaxEntries = 1
				}
				if a.Options.ConfigHash() != hash {
					t.Fatal("execution changed fingerprint")
				}
				p.Target = 257
				before := nativeSnapshot(t, db)
				step(23, false)
				if before != nativeSnapshot(t, db) {
					t.Fatal("rollback leaked native state")
				}
				// Reopen while a partial file is durable: there is no RAM cache.
				step(13, true)
				db.Close()
				reopened, err := contract.OpenPostgres(context.Background(), dsn, 6)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { reopened.Close() })
				a.DB = reopened
				for p.Committed < 257 {
					step(min(23, int(257-p.Committed)), true)
				}
				if prefix != testfixture.Snapshot(t, reopened, prefixQueries...) {
					t.Fatal("historical files/headers rewritten")
				}
				snapshots[mode] = nativeSnapshot(t, reopened)
				checkpoints[mode] = p.Checkpoint
				if a.Metrics.Access.PeakFileInputBytes.Load() > int64(a.Execution.CacheMaxBytes) || a.Metrics.Access.PeakTransactionObjectBytes.Load() > int64(a.Execution.BatchMaxBytes) {
					t.Fatal("working budget exceeded")
				}
			}
			for mode := 1; mode < 3; mode++ {
				if snapshots[mode] != snapshots[0] || !bytes.Equal(checkpoints[mode], checkpoints[0]) || !reflect.DeepEqual(ids[mode], ids[0]) {
					t.Fatal("native bytes, IDs or cursor changed", mode)
				}
			}
		})
	}
}

func TestLowRiskEveryCachedFileMatchesDatabaseTrees(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	a := lowRiskAdapter(db)
	ctx := context.Background()
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range []int{1, 3, 9, 16} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			words := []string{"a", "absent", "z", "α"}
			for kid, word := range words {
				if _, err = tx.Exec(`INSERT INTO blas.durable_keywords VALUES('low-risk',$1,$2)`, kid, word); err != nil {
					t.Fatal(err)
				}
			}
			batch, _ := diskindex.NewWriteBatch(tx, 1000, 1<<20, a.Metrics)
			w := writer{a: a, tx: tx, ctx: ctx, state: state{NextKID: uint64(len(words))}, pages: diskindex.Store{Q: tx, Scope: a.Options.RunID, Limits: a.Options.Index, Batch: batch}}
			for oid := range n {
				kw := []string{"a", "α"}
				if oid%2 == 0 {
					kw = append(kw, "z")
				}
				object := alis.EncodeLogEntry(alis.LogEntry{ESN: uint64(oid), TS: int64(oid), EW: kw, V: []byte(fmt.Sprint(oid))})
				if err := w.object("entry", 0, uint64(oid), object, kw, []string{"1"}, int64(oid), int64(oid)); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.flushObjects(); err != nil {
				t.Fatal(err)
			}
			f := w.completeFileInput(0, uint64(n))
			if f == nil {
				t.Fatal("complete file not cached")
			}
			for oid, entry := range f.entries {
				var object []byte
				var raw []byte
				if err := tx.QueryRow(`SELECT object,keywords FROM blas.durable_objects WHERE run_id='low-risk' AND kind='entry' AND owner=0 AND oid=$1`, oid).Scan(&object, &raw); err != nil {
					t.Fatal(err)
				}
				var got []string
				if err := json.Unmarshal(raw, &got); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(object, entry.object) || !reflect.DeepEqual(got, entry.words) {
					t.Fatal("cached input differs from SQL")
				}
			}
			var roots [2][2]bhash.Digest
			var snapshots [2]string
			for mode := range 2 {
				w.a.DisableLowRiskOptimizations = mode == 0
				base := fmt.Sprintf("mode%d", mode)
				roots[mode][0], err = w.dictionaryBitmap(base+"/iimt", 0, uint64(n))
				if err != nil {
					t.Fatal(err)
				}
				roots[mode][1], err = w.buildObjects(base+"/omt", "entry", 0, uint64(n))
				if err != nil {
					t.Fatal(err)
				}
				// Read in this transaction, including pending builders' acknowledged pages.
				var raw string
				if err = tx.QueryRow(`SELECT jsonb_build_array((SELECT jsonb_agg(jsonb_build_array(replace(tree,$1,''),page,encode(payload,'hex')) ORDER BY tree,page) FROM blas.index_pages WHERE tree LIKE $1||'%'),(SELECT jsonb_agg(jsonb_build_array(pos,encode(keyword,'hex'),value) ORDER BY pos) FROM blas.index_leaves WHERE tree=$1||'/iimt'))::text`, base).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				snapshots[mode] = raw
			}
			if roots[0] != roots[1] || snapshots[0] != snapshots[1] {
				t.Fatal("cached file trees/pages differ")
			}
			if a.Metrics.Access.EntryObjectRereadsAvoided.Load() == 0 || a.Metrics.Access.EntryKeywordRereadsAvoided.Load() == 0 {
				t.Fatal("cache path not exercised")
			}
		})
	}
}

func TestLowRiskBufferedTransactionRollbackAndConflict(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	a := lowRiskAdapter(db)
	a.Execution.BatchMaxRecords = 2
	ctx := context.Background()
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"before-flush", "during-flush", "after-flush", "conflict"} {
		t.Run(phase, func(t *testing.T) {
			tx, _ := db.Begin()
			batch, _ := diskindex.NewWriteBatch(tx, 2, 1<<20, a.Metrics)
			w := writer{a: a, tx: tx, ctx: ctx, pages: diskindex.Store{Q: tx, Scope: a.Options.RunID, Limits: a.Options.Index, Batch: batch}}
			if phase == "during-flush" {
				if _, err := tx.Exec(`CREATE FUNCTION fixture_tx_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected batch failure'; END $$; CREATE TRIGGER fixture_tx_fail BEFORE INSERT ON blas.durable_objects FOR EACH ROW WHEN(NEW.kind='tx') EXECUTE FUNCTION fixture_tx_fail()`); err != nil {
					t.Fatal(err)
				}
			}
			if err := w.object("tx", 0, 0, []byte("one"), []string{"a"}, []string{"1"}, 1, 1); err != nil {
				t.Fatal(err)
			}
			var count int
			tx.QueryRow(`SELECT count(*) FROM blas.durable_objects`).Scan(&count)
			if count != 0 || len(w.objectRows) != 1 {
				t.Fatal("first transaction object was not buffered")
			}
			if phase == "during-flush" || phase == "conflict" {
				oid := uint64(1)
				if phase == "conflict" {
					oid = 0
				}
				if err := w.object("tx", 0, oid, []byte("two"), []string{"b"}, []string{"1"}, 2, 2); err == nil {
					t.Fatal("failed batch/duplicate accepted")
				}
			} else if phase == "after-flush" {
				if err := w.flushObjects(); err != nil {
					t.Fatal(err)
				}
				if err := tx.QueryRow(`SELECT count(*) FROM blas.durable_objects`).Scan(&count); err != nil || count != 1 {
					t.Fatal("flush barrier", count, err)
				}
			}
			tx.Rollback()
			if err := db.QueryRow(`SELECT count(*) FROM blas.durable_objects`).Scan(&count); err != nil || count != 0 {
				t.Fatal("ghost objects after rollback", count, err)
			}
			// A fresh writer retries the identical logical object successfully.
			tx, _ = db.Begin()
			w = writer{a: a, tx: tx, ctx: ctx}
			if err := w.object("tx", 0, 0, []byte("one"), []string{"a"}, []string{"1"}, 1, 1); err != nil {
				t.Fatal(err)
			}
			if err := w.flushObjects(); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`DELETE FROM blas.durable_objects`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLowRiskCommittedCursorReplayAfterReopen(t *testing.T) {
	db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	a := lowRiskAdapter(db)
	source := contract.Store{DB: db}
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.Create(ctx, "fixture", "same-source"); err != nil {
		t.Fatal(err)
	}
	if err := source.Append(ctx, "fixture", testfixture.Entries(1, 100), contract.Limits{MaxRecords: 100, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}); err != nil {
		t.Fatal(err)
	}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := contract.Consumer{Store: source, ID: "blas/low-risk", StreamID: "fixture", Fingerprint: "same-source", ConfigHash: a.Options.ConfigHash(), Execution: a.Execution, Metrics: a.Metrics}
	if err := c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	// A real milestone is selected, but this fixture publishes/consumes only 100.
	if _, err := c.Begin(ctx, 1000000); err != nil {
		t.Fatal(err)
	}
	l := contract.Limits{MaxRecords: 12, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
	failAfterNativeWrites := func(ctx context.Context, tx *sql.Tx, p contract.Progress, es []contract.Entry) ([]string, []byte, error) {
		if _, _, err := a.Apply(ctx, tx, p, es); err != nil {
			return nil, nil, err
		}
		return nil, nil, errors.New("simulated failure after writes, before cursor update")
	}
	if _, err := c.Step(ctx, source, l, failAfterNativeWrites); err == nil {
		t.Fatal("injected failure ignored")
	}
	p, err := c.Progress(ctx)
	if err != nil || p.Committed != 0 {
		t.Fatal("cursor leap after aborted native writes", p, err)
	}
	if _, err = c.Step(ctx, source, l, a.Apply); err != nil {
		t.Fatal(err)
	}
	before, err := c.ValidatePrefix(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate losing the response after a successful atomic native/cursor commit.
	db.Close()
	reopened, err := contract.OpenPostgres(ctx, dsn, 6)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	a.DB = reopened
	source.DB = reopened
	c.Store = source
	p, err = c.ValidatePrefix(ctx, source)
	if err != nil || p.Committed != before.Committed || !bytes.Equal(p.Checkpoint, before.Checkpoint) {
		t.Fatal("restart lost committed cursor", p, err)
	}
	for p.Committed < 100 {
		p, err = c.Step(ctx, source, l, a.Apply)
		if err != nil {
			t.Fatal(err)
		}
	}
	var total, distinctOrdinals, distinctSources int
	if err := reopened.QueryRow(`SELECT count(*),count(DISTINCT ordinal),count(DISTINCT source_id) FROM canonical_ingestion.mappings`).Scan(&total, &distinctOrdinals, &distinctSources); err != nil || total != 100 || distinctOrdinals != 100 || distinctSources != 100 {
		t.Fatal("duplicate or missing mappings", total, distinctOrdinals, distinctSources, err)
	}
	if _, err := c.ValidatePrefix(ctx, source); err != nil {
		t.Fatal(err)
	}
}

func TestLowRiskImmutableAndFileWorkingBounds(t *testing.T) {
	a := lowRiskAdapter(nil)
	a.Execution.CacheMaxBytes = 1024
	a.Execution.CacheMaxEntries = 3
	w := writer{a: a}
	for i := range 100 {
		w.keywordInput(fmt.Sprint(i))
		w.zeroInput(uint64(i%16 + 1))
		zeroBytes := 0
		if w.zeroBitmap != nil {
			zeroBytes = int((w.zeroBitmap.Len()+7)/8) + 128
		}
		if len(w.keywordInputs) > 3 || w.keywordInputBytes+zeroBytes > 1024 {
			t.Fatal("immutable cache grew beyond limits")
		}
		w.rememberFileInput(0, uint64(i), make([]byte, 100), []string{"keyword"})
		if w.fileInput != nil && (w.fileInput.bytes > 1024 || len(w.fileInput.entries) > 3) {
			t.Fatal("file cache grew beyond limits")
		}
	}
	if w.fileInput != nil {
		t.Fatal("incomplete over-budget file should use SQL fallback")
	}
	w.rememberFileInput(1, 0, []byte("new-file"), []string{"keyword"})
	if w.completeFileInput(1, 1) == nil {
		t.Fatal("new file did not reset cache")
	}
	if w.completeFileInput(1, 2) != nil {
		t.Fatal("partial input accepted as a full file")
	}
}
