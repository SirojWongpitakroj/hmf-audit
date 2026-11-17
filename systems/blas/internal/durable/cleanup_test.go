package durable

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func cleanupArgs(scope string, aggregation bool) []any {
	args := []any{scope}
	for _, tree := range segmentGenerationTrees(0, 998, aggregation) {
		args = append(args, tree)
	}
	return args
}

type cleanupQuerier interface {
	Query(string, ...any) (*sql.Rows, error)
}

func cleanupKeys(t *testing.T, q cleanupQuerier, table, predicate string, args ...any) []string {
	t.Helper()
	key := "page"
	if table == "index_leaves" {
		key = "pos::text"
	}
	rows, err := q.Query("SELECT scope,tree,"+key+" FROM blas."+table+" WHERE "+predicate+" ORDER BY 1,2,3", args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var scope, tree, key string
		if err := rows.Scan(&scope, &tree, &key); err != nil {
			t.Fatal(err)
		}
		keys = append(keys, fmt.Sprintf("%q/%q/%q", scope, tree, key))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return keys
}

func TestExactCleanupRowSetsOptionalTreesAndScopeSafety(t *testing.T) {
	for _, optional := range [][]string{nil, {"generator"}, {"extended-lg"}, {"generator", "extended-lg"}} {
		t.Run(fmt.Sprint(optional), func(t *testing.T) {
			db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			a := lowRiskAdapter(db)
			a.Options.Aggregation = len(optional) != 0
			ctx := context.Background()
			if err := a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			// Adjacent generations/segments and another run must remain untouched.
			for _, scope := range []string{a.Options.RunID, "another-run"} {
				for _, base := range []string{"segment/0/997/", "segment/0/998/", "segment/0/999/", "segment/1/998/", "segment/0/9980/"} {
					suffixes := append([]string{"lg", "fk", "min", "max"}, optional...)
					for _, suffix := range suffixes {
						for n := range 2 {
							if _, err := db.Exec(`INSERT INTO blas.index_pages VALUES($1,$2,$3,$4)`, scope, base+suffix, fmt.Sprint(n), []byte{byte(n)}); err != nil {
								t.Fatal(err)
							}
							if _, err := db.Exec(`INSERT INTO blas.index_leaves VALUES($1,$2,$3,$4,$5)`, scope, base+suffix, n, []byte{byte(n)}, n); err != nil {
								t.Fatal(err)
							}
						}
					}
				}
			}
			before := nativeSnapshot(t, db)
			var deleted int
			for _, table := range []string{"index_pages", "index_leaves"} {
				prefix := cleanupKeys(t, db, table, "scope=$1 AND tree LIKE $2", a.Options.RunID, "segment/0/998/%")
				exact := cleanupKeys(t, db, table, generationCleanupPredicate(a.Options.Aggregation), cleanupArgs(a.Options.RunID, a.Options.Aggregation)...)
				if len(prefix) == 0 || !reflect.DeepEqual(prefix, exact) {
					t.Fatal("prefix/exact primary keys differ", table, prefix, exact)
				}
				deleted += len(exact)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			w := writer{a: a, tx: tx, ctx: ctx}
			if err := w.cleanupGeneration(0, 998); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"index_pages", "index_leaves"} {
				remaining := cleanupKeys(t, tx, table, "TRUE")
				expected := cleanupKeys(t, db, table, "NOT (scope=$1 AND tree LIKE $2)", a.Options.RunID, "segment/0/998/%")
				if !reflect.DeepEqual(remaining, expected) {
					t.Fatal("cleanup changed a neighboring scope/generation", table)
				}
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if nativeSnapshot(t, db) != before {
				t.Fatal("cleanup survived rollback")
			}
			m := a.Metrics.Access.Snapshot()
			// Very short statements may round to zero on coarse platform clocks.
			if m.CleanupCalls != 1 || m.CleanupSQLCalls != 2 || m.CleanupRowsDeleted != uint64(deleted) || m.CleanupPageRowsDeleted+m.CleanupLeafRowsDeleted != m.CleanupRowsDeleted || m.CleanupGeneration != 998 || m.CleanupSegment != 0 || m.CleanupTreesRequested != uint64(len(cleanupArgs(a.Options.RunID, a.Options.Aggregation))-1) || m.CleanupPagesNanos+m.CleanupLeavesNanos > m.CleanupNanos {
				t.Fatal("incorrect attempted-work telemetry", m)
			}
			// Committed cleanup must remove exactly the same primary keys too.
			tx, err = db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			w.tx, w.a.Metrics = tx, nil // library users need not enable telemetry
			if err := w.cleanupGeneration(0, 998); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			for _, table := range []string{"index_pages", "index_leaves"} {
				if len(cleanupKeys(t, db, table, "scope=$1 AND tree LIKE $2", a.Options.RunID, "segment/0/998/%")) != 0 {
					t.Fatal("previous generation not removed", table)
				}
			}
		})
	}
}

func TestExactCleanupRollbackRestartAndCursor(t *testing.T) {
	for _, phase := range []string{"after-cleanup", "leaf-delete", "commit"} {
		t.Run(phase, func(t *testing.T) {
			db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			a := lowRiskAdapter(db)
			a.Options.FilesPerBlock, a.Options.SegmentSize = 2, 4
			ctx := context.Background()
			source := contract.Store{DB: db}
			if err := source.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := source.Create(ctx, "fixture", "same-source"); err != nil {
				t.Fatal(err)
			}
			limits := contract.Limits{MaxRecords: 24, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
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
			// A partial 100-record fixture; target is not a million-record test.
			if _, err := c.Begin(ctx, 1000000); err != nil {
				t.Fatal(err)
			}
			before, err := c.Step(ctx, source, limits, a.Apply)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := nativeSnapshot(t, db)
			calls := a.Metrics.Access.CleanupCalls.Load()
			apply := a.Apply
			if phase == "after-cleanup" {
				apply = func(ctx context.Context, tx *sql.Tx, p contract.Progress, es []contract.Entry) ([]string, []byte, error) {
					if _, _, err := a.Apply(ctx, tx, p, es); err != nil {
						return nil, nil, err
					}
					return nil, nil, fmt.Errorf("injected failure after cleanup before cursor")
				}
			} else {
				trigger := `CREATE FUNCTION cleanup_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected cleanup failure'; END $$;`
				if phase == "leaf-delete" {
					trigger += `CREATE TRIGGER cleanup_fail BEFORE DELETE ON blas.index_leaves FOR EACH STATEMENT EXECUTE FUNCTION cleanup_fail();`
				} else {
					trigger += `CREATE CONSTRAINT TRIGGER cleanup_fail AFTER UPDATE ON canonical_ingestion.consumers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION cleanup_fail();`
				}
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.Step(ctx, source, limits, apply); err == nil {
				t.Fatal("injected failure accepted")
			}
			after, err := c.ValidatePrefix(ctx, source)
			if err != nil || after.Committed != before.Committed || !bytes.Equal(after.Checkpoint, before.Checkpoint) || snapshot != nativeSnapshot(t, db) || a.Metrics.Access.CleanupCalls.Load() <= calls {
				t.Fatal("rollback changed previous generation, cursor or new generation", err)
			}
			if phase != "after-cleanup" {
				if _, err := db.Exec(`DROP FUNCTION cleanup_fail() CASCADE`); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			reopened, err := contract.OpenPostgres(ctx, dsn, 6)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			a.DB, source.DB = reopened, reopened
			c.Store = source
			for after.Committed < 100 {
				after, err = c.Step(ctx, source, limits, a.Apply)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.ValidatePrefix(ctx, source); err != nil {
				t.Fatal(err)
			}
			var count, unique int
			if err := reopened.QueryRow(`SELECT count(*),count(DISTINCT source_id) FROM canonical_ingestion.mappings`).Scan(&count, &unique); err != nil || count != 100 || unique != 100 {
				t.Fatal("replay duplicated/skipped observations", count, unique, err)
			}
			// Compare the entire recovered native state against uninterrupted ingest.
			refDB, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			ref := lowRiskAdapter(refDB)
			ref.Options = a.Options
			if err := ref.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			p := contract.Progress{Target: 1000000}
			for p.Committed < 100 {
				tx, err := refDB.Begin()
				if err != nil {
					t.Fatal(err)
				}
				_, cp, err := ref.Apply(ctx, tx, p, testfixture.Entries(p.Committed+1, min(24, int(100-p.Committed))))
				if err != nil {
					tx.Rollback()
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				p.Committed += uint64(min(24, int(100-p.Committed)))
				p.Checkpoint = cp
			}
			if !bytes.Equal(p.Checkpoint, after.Checkpoint) || nativeSnapshot(t, reopened) != nativeSnapshot(t, refDB) {
				t.Fatal("restart changed roots, membership, tree set or checkpoint")
			}
		})
	}
}

func TestExactCleanupPlans(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	a := lowRiskAdapter(db)
	if err := a.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Disposable fixture ONLY: reproduce a non-C English collation on Windows.
	// Production schema/index definitions are never altered by this optimization.
	if _, err := db.Exec(`CREATE COLLATION cleanup_english (provider=icu, locale='en-US');
ALTER TABLE blas.index_pages ALTER COLUMN tree TYPE text COLLATE cleanup_english;
ALTER TABLE blas.index_leaves ALTER COLUMN tree TYPE text COLLATE cleanup_english;
INSERT INTO blas.index_pages SELECT 'low-risk',CASE WHEN n<=4 THEN 'segment/0/998/'||(ARRAY['lg','fk','min','max'])[n] ELSE 'file/'||n||'/iimt' END,'metadata',decode(repeat('ab',1024),'hex') FROM generate_series(1,10000) n;
INSERT INTO blas.index_leaves SELECT scope,tree,0,decode(repeat('cd',64),'hex'),0 FROM blas.index_pages;
ANALYZE blas.index_pages; ANALYZE blas.index_leaves;`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"index_pages", "index_leaves"} {
		for _, exact := range []bool{false, true} {
			predicate := "scope=$1 AND tree LIKE $2"
			args := []any{a.Options.RunID, "segment/0/998/%"}
			if exact {
				predicate, args = generationCleanupPredicate(false), cleanupArgs(a.Options.RunID, false)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			var raw []byte
			err = tx.QueryRow("EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) DELETE FROM blas."+table+" WHERE "+predicate, args...).Scan(&raw)
			rollbackErr := tx.Rollback() // EXPLAIN ANALYZE executes DELETE: never commit.
			if err != nil || rollbackErr != nil {
				t.Fatal(err, rollbackErr)
			}
			var plan []map[string]any
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			hasSequential := strings.Contains(string(raw), `"Node Type": "Seq Scan"`)
			hasIndex := strings.Contains(string(raw), "Index Scan") || strings.Contains(string(raw), "Index Only Scan")
			if exact && (hasSequential || !hasIndex) {
				t.Fatal("exact cleanup did not use indexed lookup", string(raw))
			}
			if !exact && !hasSequential {
				t.Fatal("fixture did not reproduce non-C prefix scan", string(raw))
			}
			t.Logf("table=%s exact=%v execution_ms=%v plan=%s", table, exact, plan[0]["Execution Time"], raw)
		}
	}
}
