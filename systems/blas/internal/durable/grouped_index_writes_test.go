package durable

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"testing"

	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestGroupedIndexWritesPriorStateRestartAndBounds(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		for _, limit := range []int{1000, 7} {
			t.Run(fmt.Sprintf("aggregate=%v/rows=%d", aggregate, limit), func(t *testing.T) {
				var snapshots [2]string
				var checkpoints [2][]byte
				var ids [2][]string
				var metrics [2]contract.AccessSnapshot
				for mode := range 2 {
					db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
					a := lowRiskAdapter(db)
					a.Options.Aggregation = aggregate
					a.Options.FilesPerBlock = 4
					a.Options.SegmentSize = 4
					a.Execution.BatchMaxRecords = limit
					a.DisableCompatibleIndexBatching = true
					a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
					ctx := context.Background()
					if err := a.Migrate(ctx); err != nil {
						t.Fatal(err)
					}
					// Test-only barriers reject any header exposed inside the transaction
					// before its original complete tree generations have been written.
					if _, err := db.Exec(`CREATE FUNCTION check_header_pages() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF (SELECT count(*) FROM blas.index_pages WHERE scope=NEW.run_id AND page='metadata' AND tree IN('block/'||NEW.height||'/omt','block/'||NEW.height||'/fk','block/'||NEW.height||'/min','block/'||NEW.height||'/max'))<>4 THEN RAISE EXCEPTION 'block trees not flushed'; END IF;
 IF (SELECT count(*) FROM blas.index_pages WHERE scope=NEW.run_id AND page='metadata' AND tree IN('segment/'||NEW.segment||'/'||NEW.height||'/lg','segment/'||NEW.segment||'/'||NEW.height||'/fk','segment/'||NEW.segment||'/'||NEW.height||'/min','segment/'||NEW.segment||'/'||NEW.height||'/max'))<>4 THEN RAISE EXCEPTION 'segment trees not flushed'; END IF;
 IF EXISTS(SELECT 1 FROM blas.durable_files f WHERE f.run_id=NEW.run_id AND (NOT EXISTS(SELECT 1 FROM blas.index_pages p WHERE p.scope=f.run_id AND p.tree='file/'||f.fid||'/iimt' AND p.page='metadata') OR NOT EXISTS(SELECT 1 FROM blas.index_pages p WHERE p.scope=f.run_id AND p.tree='file/'||f.fid||'/omt' AND p.page='metadata'))) THEN RAISE EXCEPTION 'file trees not flushed'; END IF;
 RETURN NEW; END $$; CREATE TRIGGER check_header_pages BEFORE INSERT ON blas.durable_headers FOR EACH ROW EXECUTE FUNCTION check_header_pages();`); err != nil {
						t.Fatal(err)
					}
					p := contract.Progress{Target: 100}
					fingerprint := a.Options.ConfigHash()
					step := func(n int, commit bool) {
						t.Helper()
						tx, err := a.DB.Begin()
						if err != nil {
							t.Fatal(err)
						}
						got, cp, err := a.Apply(ctx, tx, p, testfixture.Entries(p.Committed+1, n))
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
					for p.Committed < 100 {
						step(min(limit, 23, int(100-p.Committed)), true)
					}
					prefix := nativeSnapshot(t, a.DB)
					a.DisableCompatibleIndexBatching = mode == 0
					p.Target = 257
					step(min(limit, 23), false)
					if prefix != nativeSnapshot(t, a.DB) {
						t.Fatal("rollback changed old native state")
					}
					a.DB.Close()
					reopened, err := contract.OpenPostgres(ctx, dsn, 6)
					if err != nil {
						t.Fatal(err)
					}
					defer reopened.Close()
					a.DB = reopened
					a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
					for p.Committed < 257 {
						step(min(limit, 23, int(257-p.Committed)), true)
					}
					if a.Options.ConfigHash() != fingerprint {
						t.Fatal("batch execution changed fingerprint")
					}
					snapshots[mode] = nativeSnapshot(t, a.DB)
					checkpoints[mode] = p.Checkpoint
					metrics[mode] = a.Metrics.Access.Snapshot()
					if metrics[mode].PeakIndexBatchRows > int64(limit) || metrics[mode].PeakIndexBatchBytes > int64(a.Execution.BatchMaxBytes) {
						t.Fatal("batch exceeded limit", metrics[mode])
					}
				}
				if snapshots[0] != snapshots[1] || !bytes.Equal(checkpoints[0], checkpoints[1]) || !reflect.DeepEqual(ids[0], ids[1]) {
					t.Fatal("grouping changed rows, roots, IDs or checkpoint")
				}
				if metrics[0].IndexPageRows != metrics[1].IndexPageRows || metrics[0].IndexLeafRows != metrics[1].IndexLeafRows || metrics[0].IndexPayloadBytes != metrics[1].IndexPayloadBytes {
					t.Fatal("different index payloads/row counts")
				}
				if limit == 1000 && metrics[1].IndexInsertCalls >= metrics[0].IndexInsertCalls {
					t.Fatal("fragmented index writes were not reduced", metrics[0].IndexInsertCalls, metrics[1].IndexInsertCalls)
				}
			})
		}
	}
}

func TestGroupedIndexWritesFlushBeforeCursorAndLeafFailureReplay(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	a := lowRiskAdapter(db)
	a.Options.FilesPerBlock = 256 // no block-close barrier in this 24-record step
	source := contract.Store{DB: db}
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.Create(ctx, "fixture", "same-source"); err != nil {
		t.Fatal(err)
	}
	l := contract.Limits{MaxRecords: 24, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
	if err := source.Append(ctx, "fixture", testfixture.Entries(1, 24), l); err != nil {
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
	if _, err := db.Exec(`CREATE FUNCTION check_cursor_pages() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.committed>OLD.committed AND EXISTS(SELECT 1 FROM blas.durable_files f WHERE f.run_id='low-risk' AND (NOT EXISTS(SELECT 1 FROM blas.index_pages p WHERE p.scope=f.run_id AND p.tree='file/'||f.fid||'/iimt' AND p.page='metadata') OR NOT EXISTS(SELECT 1 FROM blas.index_pages p WHERE p.scope=f.run_id AND p.tree='file/'||f.fid||'/omt' AND p.page='metadata'))) THEN RAISE EXCEPTION 'cursor before file pages'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER check_cursor_pages BEFORE UPDATE ON canonical_ingestion.consumers FOR EACH ROW EXECUTE FUNCTION check_cursor_pages();
 CREATE FUNCTION fail_lookup_insert() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected lookup batch failure'; END $$;
 CREATE TRIGGER fail_lookup_insert BEFORE INSERT ON blas.index_leaves FOR EACH ROW EXECUTE FUNCTION fail_lookup_insert();`); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(ctx, source, l, a.Apply); err == nil {
		t.Fatal("lookup write failure accepted")
	}
	p, err := c.ValidatePrefix(ctx, source)
	if err != nil || p.Committed != 0 {
		t.Fatal("failed final flush advanced cursor", p, err)
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM blas.index_pages`).Scan(&count); err != nil || count != 0 {
		t.Fatal("acknowledged page INSERT survived transaction rollback", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM blas.durable_files`).Scan(&count); err != nil || count != 0 {
		t.Fatal("file metadata survived failed batch", count, err)
	}
	if _, err := db.Exec(`DROP FUNCTION fail_lookup_insert() CASCADE`); err != nil {
		t.Fatal(err)
	}
	p, err = c.Step(ctx, source, l, a.Apply)
	if err != nil || p.Committed != 24 {
		t.Fatal("retry or cursor barrier failed", p, err)
	}
	if _, err := c.ValidatePrefix(ctx, source); err != nil {
		t.Fatal(err)
	}
}
