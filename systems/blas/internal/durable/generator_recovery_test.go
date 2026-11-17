package durable

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"blas/internal/diskindex"
	"blas/live"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestGeneratorFailuresReplayAndAudit(t *testing.T) {
	for _, phase := range []string{"file-close", "index-insert", "transaction", "block-header", "after-native", "commit"} {
		t.Run(phase, func(t *testing.T) {
			db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			a := generatorAdapter(db)
			a.CompactZeroBitmaps = true
			ctx := context.Background()
			source := contract.Store{DB: db}
			es := generatorEntries(257)
			if err := source.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := source.Create(ctx, "fixture", "same-source"); err != nil {
				t.Fatal(err)
			}
			if err := source.Append(ctx, "fixture", es, contract.Limits{MaxRecords: 257, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}); err != nil {
				t.Fatal(err)
			}
			if err := a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			c := contract.Consumer{Store: source, ID: "blas/low-risk", StreamID: "fixture", Fingerprint: "same-source", ConfigHash: a.Options.ConfigHash(), Execution: a.Execution, Metrics: a.Metrics}
			if err := c.Open(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Begin(ctx, 1000000); err != nil {
				t.Fatal(err)
			}
			limit := contract.Limits{MaxRecords: 24, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
			before, err := c.Step(ctx, source, limit, a.Apply)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := nativeSnapshot(t, db)
			keywords := testfixture.Snapshot(t, db, `SELECT * FROM blas.durable_generator_keywords ORDER BY lid,kid`)
			sourceBefore := testfixture.Snapshot(t, db, `SELECT * FROM canonical_ingestion.entries ORDER BY ordinal`, `SELECT * FROM canonical_ingestion.streams ORDER BY stream_id`)
			trigger := `CREATE FUNCTION generator_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected generator failure'; END $$;`
			switch phase {
			case "file-close":
				trigger += `CREATE TRIGGER generator_fail BEFORE INSERT ON blas.durable_files FOR EACH ROW EXECUTE FUNCTION generator_fail();`
			case "index-insert":
				trigger += `CREATE TRIGGER generator_fail BEFORE INSERT ON blas.index_pages FOR EACH ROW EXECUTE FUNCTION generator_fail();`
			case "transaction":
				trigger += `CREATE TRIGGER generator_fail BEFORE INSERT ON blas.durable_objects FOR EACH ROW WHEN(NEW.kind='tx') EXECUTE FUNCTION generator_fail();`
			case "block-header":
				trigger += `CREATE TRIGGER generator_fail BEFORE INSERT ON blas.durable_headers FOR EACH ROW EXECUTE FUNCTION generator_fail();`
			case "commit":
				trigger += `CREATE CONSTRAINT TRIGGER generator_fail AFTER UPDATE ON canonical_ingestion.consumers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION generator_fail();`
			}
			apply := a.Apply
			if phase == "after-native" {
				apply = func(ctx context.Context, tx *sql.Tx, p contract.Progress, es []contract.Entry) ([]string, []byte, error) {
					if _, _, err := a.Apply(ctx, tx, p, es); err != nil {
						return nil, nil, err
					}
					return nil, nil, fmt.Errorf("failure after native writes")
				}
			} else {
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			limit.MaxRecords = 64
			if _, err := c.Step(ctx, source, limit, apply); err == nil {
				t.Fatal("failure not propagated")
			}
			after, err := c.Progress(ctx)
			if err != nil || after.Committed != before.Committed || !bytes.Equal(after.Checkpoint, before.Checkpoint) || nativeSnapshot(t, db) != snapshot || testfixture.Snapshot(t, db, `SELECT * FROM blas.durable_generator_keywords ORDER BY lid,kid`) != keywords {
				t.Fatal("rollback changed durable state", err)
			}
			if phase != "after-native" {
				if _, err := db.Exec(`DROP FUNCTION generator_fail() CASCADE`); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			db, err = contract.OpenPostgres(ctx, dsn, 6)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			a.DB = db
			source.DB = db
			c.Store = source
			for after.Committed < 257 {
				after, err = c.Step(ctx, source, limit, a.Apply)
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.ValidatePrefix(ctx, source); err != nil {
				t.Fatal(err)
			}
			// Finalize this small native fixture without changing its canonical cursor.
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			p := after
			p.Target = 257
			_, cp, err := a.Apply(ctx, tx, p, nil)
			if err != nil {
				tx.Rollback()
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			oracle := buildGeneratorOracle(t, es, a.Options)
			for _, b := range oracle.chain.Blocks() {
				h, err := a.NewReader(ctx, nil).Header(b.Header.BlockHeight)
				if err != nil || h.RH != b.Header.RH {
					t.Fatal("replay root differs", err)
				}
			}
			var st state
			if err := json.Unmarshal(cp, &st); err != nil || st.Records != 257 {
				t.Fatal(err)
			}
			cache, _ := diskindex.NewCache(a.Options.Index)
			metrics, err := a.Audit(ctx, c, contract.AuditRequest{Version: 1, StreamID: "fixture", Fingerprint: "same-source", ExpectedN: 257, Entries: es[:12]}, cache, true)
			if err != nil || metrics.RequestedRecords != 12 {
				t.Fatal("generator-aware audit failed", metrics, err)
			}
			if sourceBefore != testfixture.Snapshot(t, db, `SELECT * FROM canonical_ingestion.entries ORDER BY ordinal`, `SELECT * FROM canonical_ingestion.streams ORDER BY stream_id`) {
				t.Fatal("BLAS modified canonical source")
			}
		})
	}
}

func TestGeneratorBoundAndLegacyFingerprint(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	a := generatorAdapter(db)
	a.Options.MaxGenerators = 1
	ctx := context.Background()
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	tx, _ := db.Begin()
	defer tx.Rollback()
	if _, _, err := a.Apply(ctx, tx, contract.Progress{Target: 10}, generatorEntries(2)); err == nil {
		t.Fatal("generator bound ignored")
	}
	tx.Rollback()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM blas.durable_objects`).Scan(&count); err != nil || count != 0 {
		t.Fatal("bound failure left ghost entries", err)
	}
	legacy := a.Options
	legacy.GeneratorMode = ""
	if legacy.ConfigHash() == a.Options.ConfigHash() {
		t.Fatal("old native state could be resumed under new semantics")
	}
	// Distinct tuple framing must not conflate concatenated source names.
	seen := map[uint64]string{}
	for _, e := range generatorEntries(100) {
		var o live.Observation
		json.Unmarshal(e.Record, &o)
		key, lid := generatorIdentity(o)
		if old, ok := seen[lid]; ok && old != key {
			t.Fatal("distinct generator tuple collided")
		}
		seen[lid] = key
	}
	if len(seen) != 12 {
		t.Fatal("dataset/site/device grouping changed", len(seen))
	}
}
