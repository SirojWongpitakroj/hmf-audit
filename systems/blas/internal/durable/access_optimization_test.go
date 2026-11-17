package durable

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"blas/alis"
	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestMetadataScratchFallbackKeepsPriorAcceptance(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	e := contract.DefaultExecution()
	a := Adapter{DB: db, Execution: &e, Options: Options{RunID: "budget", EntriesPerFile: 4, FilesPerBlock: 3, Fanout: 4, SegmentSize: 4, Index: diskindex.Limits{PageBytes: 4096, BitmapBytes: 1 << 20}, ObjectBytes: 4000, QueryBytes: 1 << 20}}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	var words []string
	for i := 0; i < 100; i++ {
		word := fmt.Sprintf("kw%03d", i)
		words = append(words, word)
		if _, err = tx.ExecContext(ctx, `INSERT INTO blas.durable_keywords VALUES('budget',$1,$2)`, i, word); err != nil {
			t.Fatal(err)
		}
	}
	raw, _ := json.Marshal(words)
	if _, err = tx.ExecContext(ctx, `INSERT INTO blas.durable_objects VALUES('budget','tx',0,0,'\x00',$1::jsonb,'[]',-5,9)`, string(raw)); err != nil {
		t.Fatal(err)
	}
	w := writer{a: a, tx: tx, ctx: ctx, state: state{NextKID: 100, InBlock: 1}, pages: diskindex.Store{Q: tx, Scope: "budget", Limits: a.Options.Index}}
	got, err := w.words("tx", 0)
	if err != nil || !reflect.DeepEqual(got, words) {
		t.Fatal("metadata fallback changed vocabulary acceptance", err)
	}
	w.a.Execution.StorageAccessOptimizations = false
	oldRoot, err := w.buildBitmap("old", "tx", 0, 1, "keywords")
	if err != nil {
		t.Fatal(err)
	}
	w.a.Execution.StorageAccessOptimizations = true
	root, err := w.buildBitmap("new", "tx", 0, 1, "keywords")
	if err != nil || root != oldRoot {
		t.Fatal("scratch fallback changed bitmap root", err)
	}
}

func TestOptimizedStoredProofVerification(t *testing.T) {
	_, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	t.Setenv("BLAS_TEST_POSTGRES_DSN", dsn)
	t.Setenv("BLAS_TEST_ACCESS_OPTIMIZED", "1")
	TestIncrementalRootsAndProofs(t)
}

func TestAccessOptimizationMatchesCurrentAndBudgetFallback(t *testing.T) {
	var snapshots [3]string
	var checkpoints [3][]byte
	for mode := 0; mode < 3; mode++ {
		db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
		e := contract.DefaultExecution()
		e.StorageAccessOptimizations = mode != 0
		if mode == 2 {
			e.CacheMaxBytes = 256
			e.CacheMaxEntries = 1
		}
		m := &contract.IngestMetrics{}
		a := Adapter{DB: db, Execution: &e, Metrics: m, Options: Options{RunID: "same-state", EntriesPerFile: 16, FilesPerBlock: 4, Fanout: 4, SegmentSize: 4, Index: diskindex.Limits{PageBytes: 4096, BitmapBytes: 1 << 20}, ObjectBytes: 1 << 20, QueryBytes: 16 << 20}}
		ctx := context.Background()
		if err := a.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		p := contract.Progress{Target: 257}
		for p.Committed < 257 {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			n := min(23, int(257-p.Committed))
			_, cp, err := a.Apply(ctx, tx, p, testfixture.Entries(p.Committed+1, n))
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			p.Committed += uint64(n)
			p.Checkpoint = cp
		}
		snapshots[mode] = testfixture.Snapshot(t, db, `SELECT kid,keyword FROM blas.durable_keywords ORDER BY kid`, `SELECT kind,owner,oid,object,keywords,lids,ts_min,ts_max FROM blas.durable_objects ORDER BY kind,owner,oid`, `SELECT fid,block_height,entries,metadata FROM blas.durable_files ORDER BY fid`, `SELECT height,segment,payload FROM blas.durable_headers ORDER BY height`, `SELECT * FROM blas.index_pages ORDER BY scope,tree,page`, `SELECT * FROM blas.index_leaves ORDER BY scope,tree,pos`)
		checkpoints[mode] = p.Checkpoint
		if m.Access.PeakSummaryBytes.Load() > int64(e.CacheMaxBytes) {
			t.Fatal("summary budget exceeded", m.Access.Snapshot())
		}
	}
	for i := 1; i < 3; i++ {
		if snapshots[0] != snapshots[i] || !bytes.Equal(checkpoints[0], checkpoints[i]) {
			t.Fatal("storage access path changed native output", i)
		}
	}
}

func TestOptimizedConsumerFailureDoesNotAdvanceCursor(t *testing.T) {
	for _, phase := range []string{"metadata-read", "bitmap-write", "cursor-update", "commit"} {
		t.Run(phase, func(t *testing.T) {
			db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			ctx := context.Background()
			e := contract.DefaultExecution()
			a := Adapter{DB: db, Execution: &e, Options: Options{RunID: "failure", EntriesPerFile: 4, FilesPerBlock: 3, Fanout: 4, SegmentSize: 4, Index: diskindex.Limits{PageBytes: 4096, BitmapBytes: 1 << 20}, ObjectBytes: 1 << 20, QueryBytes: 16 << 20}}
			if err := a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			source := contract.Store{DB: db}
			if err := source.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := source.Create(ctx, "fixture", "same-source"); err != nil {
				t.Fatal(err)
			}
			l := contract.Limits{MaxRecords: 12, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
			if err := source.Append(ctx, "fixture", testfixture.Entries(1, 100), contract.Limits{MaxRecords: 100, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}); err != nil {
				t.Fatal(err)
			}
			c := contract.Consumer{Store: source, ID: "blas/failure", StreamID: "fixture", Fingerprint: "same-source", ConfigHash: a.Options.ConfigHash(), Execution: &e}
			if err := c.Open(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Begin(ctx, 1000000); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Step(ctx, source, l, a.Apply); err != nil {
				t.Fatal(err)
			}
			p, _ := c.Progress(ctx)
			before := testfixture.Snapshot(t, db, `SELECT * FROM canonical_ingestion.mappings ORDER BY ordinal`, `SELECT height,payload FROM blas.durable_headers ORDER BY height`)
			apply := a.Apply
			if phase == "metadata-read" {
				apply = func(ctx context.Context, tx *sql.Tx, p contract.Progress, es []contract.Entry) ([]string, []byte, error) {
					if _, err := tx.ExecContext(ctx, "UPDATE blas.durable_files SET metadata='\\x00'::bytea WHERE run_id='failure' AND fid=(SELECT max(fid) FROM blas.durable_files WHERE run_id='failure')"); err != nil {
						return nil, nil, err
					}
					var count uint64
					if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM blas.durable_files WHERE run_id='failure'").Scan(&count); err != nil {
						return nil, nil, err
					}
					w := writer{a: a, tx: tx, ctx: ctx}
					err := w.files(0, count, func(uint64, alis.FileMetadata) error { return nil })
					if err == nil {
						return nil, nil, fmt.Errorf("metadata corruption was not read")
					}
					return nil, nil, err
				}
			}
			trigger := `CREATE FUNCTION fixture_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected optimization failure'; END $$;`
			if phase == "bitmap-write" {
				trigger += `CREATE TRIGGER fixture_fail BEFORE INSERT ON blas.index_pages FOR EACH ROW EXECUTE FUNCTION fixture_fail();`
			}
			if phase == "cursor-update" {
				trigger += `CREATE TRIGGER fixture_fail BEFORE UPDATE ON canonical_ingestion.consumers FOR EACH ROW EXECUTE FUNCTION fixture_fail();`
			}
			if phase == "commit" {
				trigger += `CREATE CONSTRAINT TRIGGER fixture_fail AFTER UPDATE ON canonical_ingestion.consumers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION fixture_fail();`
			}
			if phase != "metadata-read" {
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.Step(ctx, source, l, apply); err == nil {
				t.Fatal("injected failure accepted")
			}
			after, err := c.ValidatePrefix(ctx, source)
			if err != nil || after.Committed != p.Committed || !bytes.Equal(after.Checkpoint, p.Checkpoint) {
				t.Fatal("cursor leap", after, err)
			}
			if before != testfixture.Snapshot(t, db, `SELECT * FROM canonical_ingestion.mappings ORDER BY ordinal`, `SELECT height,payload FROM blas.durable_headers ORDER BY height`) {
				t.Fatal("transaction rollback changed committed state")
			}
			if phase != "metadata-read" {
				if _, err = db.Exec(`DROP FUNCTION fixture_fail() CASCADE`); err != nil {
					t.Fatal(err)
				}
			}
			for after.Committed < 100 {
				after, err = c.Step(ctx, source, l, a.Apply)
				if err != nil {
					t.Fatal(err)
				}
			}
			var count int
			if err = db.QueryRow(`SELECT count(*) FROM canonical_ingestion.mappings`).Scan(&count); err != nil || count != 100 {
				t.Fatal(fmt.Sprint(count), err)
			}
		})
	}
}
