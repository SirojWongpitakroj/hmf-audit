package durable

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestSegmentComputationEveryBlockRestartRollbackAndStorage(t *testing.T) {
	for _, aggregate := range []bool{false, true} {
		t.Run(fmt.Sprint(aggregate), func(t *testing.T) {
			var snapshots [2][]string
			var checkpoints [2][][]byte
			var ids [2][]string
			for mode := range 2 {
				db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
				a := lowRiskAdapter(db)
				// This is a small equivalence fixture, not a production layout change.
				a.Options.FilesPerBlock = 2
				a.Options.SegmentSize = 16
				a.Options.Aggregation = aggregate
				ctx := context.Background()
				if err := a.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				p := contract.Progress{Target: 257}
				fingerprint := a.Options.ConfigHash()
				var previous state
				for p.Committed < 257 {
					if p.Committed == 37 && mode == 1 {
						a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
					}
					if p.Committed == 70 && mode == 1 {
						before := nativeSnapshot(t, a.DB)
						tx, _ := a.DB.Begin()
						if _, _, err := a.Apply(ctx, tx, p, testfixture.Entries(71, 40)); err != nil {
							tx.Rollback()
							t.Fatal(err)
						}
						tx.Rollback()
						if before != nativeSnapshot(t, a.DB) {
							t.Fatal("rollback exposed uncommitted generation")
						}
					}
					if p.Committed == 113 && mode == 1 {
						a.DB.Close()
						reopened, err := contract.OpenPostgres(ctx, dsn, 6)
						if err != nil {
							t.Fatal(err)
						}
						defer reopened.Close()
						a.DB = reopened
						a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
					}
					tx, err := a.DB.Begin()
					if err != nil {
						t.Fatal(err)
					}
					got, cp, err := a.Apply(ctx, tx, p, testfixture.Entries(p.Committed+1, 1))
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					ids[mode] = append(ids[mode], got...)
					p.Committed++
					p.Checkpoint = cp
					var current state
					if err := json.Unmarshal(cp, &current); err != nil {
						t.Fatal(err)
					}
					if current.Blocks != previous.Blocks {
						snapshots[mode] = append(snapshots[mode], nativeSnapshot(t, a.DB))
						checkpoints[mode] = append(checkpoints[mode], bytes.Clone(cp))
						// At commit, one latest generation per existing segment; no new snapshots.
						var generations uint64
						if err := a.DB.QueryRow(`SELECT count(DISTINCT split_part(tree,'/',2)||'/'||split_part(tree,'/',3)) FROM blas.index_pages WHERE scope=$1 AND tree LIKE 'segment/%'`, a.Options.RunID).Scan(&generations); err != nil || generations != (current.Blocks+15)/16 {
							t.Fatal("generation cleanup changed", generations, err)
						}
					}
					previous = current
				}
				if a.Options.ConfigHash() != fingerprint {
					t.Fatal("memo changed semantic fingerprint")
				}
				if mode == 1 && (a.Metrics.Access.SegmentHashHits.Load() == 0 || a.Metrics.Access.PeakSegmentHashCacheBytes.Load() > 1<<20) {
					t.Fatal("reuse/bounds not exercised")
				}
			}
			if fmt.Sprint(snapshots[0]) != fmt.Sprint(snapshots[1]) || fmt.Sprint(checkpoints[0]) != fmt.Sprint(checkpoints[1]) || fmt.Sprint(ids[0]) != fmt.Sprint(ids[1]) {
				t.Fatal("per-block bytes, roots, membership, IDs or checkpoint differ")
			}
		})
	}
}

func TestSegmentComputationStoredProofVerification(t *testing.T) {
	_, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	t.Setenv("BLAS_TEST_POSTGRES_DSN", dsn)
	t.Setenv("BLAS_TEST_ACCESS_OPTIMIZED", "1")
	t.Setenv("BLAS_TEST_SEGMENT_COMPUTATIONS", "1")
	TestIncrementalRootsAndProofs(t)
}

func TestSegmentComputationConsumerFailureReplay(t *testing.T) {
	for _, phase := range []string{"node-write", "header-write", "before-cursor", "commit"} {
		t.Run(phase, func(t *testing.T) {
			db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			ctx := context.Background()
			a := lowRiskAdapter(db)
			a.Options.FilesPerBlock = 2
			a.Options.SegmentSize = 16
			a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
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
			if _, err := c.Begin(ctx, 1000000); err != nil {
				t.Fatal(err)
			}
			limits := contract.Limits{MaxRecords: 24, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
			if _, err := c.Step(ctx, source, limits, a.Apply); err != nil {
				t.Fatal(err)
			}
			before, err := c.ValidatePrefix(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := nativeSnapshot(t, db)
			trigger := `CREATE FUNCTION segment_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected segment failure'; END $$;`
			switch phase {
			case "node-write":
				trigger += `CREATE TRIGGER segment_fail BEFORE INSERT ON blas.index_pages FOR EACH ROW WHEN(NEW.tree LIKE 'segment/%' AND NEW.page LIKE 'node/%') EXECUTE FUNCTION segment_fail();`
			case "header-write":
				trigger += `CREATE TRIGGER segment_fail BEFORE INSERT ON blas.durable_headers FOR EACH ROW EXECUTE FUNCTION segment_fail();`
			case "commit":
				trigger += `CREATE CONSTRAINT TRIGGER segment_fail AFTER UPDATE ON canonical_ingestion.consumers DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION segment_fail();`
			}
			apply := a.Apply
			if phase == "before-cursor" {
				apply = func(ctx context.Context, tx *sql.Tx, p contract.Progress, es []contract.Entry) ([]string, []byte, error) {
					if _, _, err := a.Apply(ctx, tx, p, es); err != nil {
						return nil, nil, err
					}
					return nil, nil, fmt.Errorf("failure after roots, before cursor")
				}
			} else {
				if _, err := db.Exec(trigger); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := c.Step(ctx, source, limits, apply); err == nil {
				t.Fatal("injected failure accepted")
			}
			after, err := c.ValidatePrefix(ctx, source)
			if err != nil || after.Committed != before.Committed || !bytes.Equal(after.Checkpoint, before.Checkpoint) || snapshot != nativeSnapshot(t, db) {
				t.Fatal("authoritative state changed on failure", err)
			}
			if phase != "before-cursor" {
				if _, err := db.Exec(`DROP FUNCTION segment_fail() CASCADE`); err != nil {
					t.Fatal(err)
				}
			}
			// Retry using pure computations cached during the failed attempt. Hits
			// must never suppress the original authenticated SQL writes.
			if _, err := c.Step(ctx, source, limits, a.Apply); err != nil {
				t.Fatal(err)
			}
			db.Close()
			reopened, err := contract.OpenPostgres(ctx, dsn, 6)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			a.DB = reopened
			a.SegmentComputations = diskindex.NewComputationCache(4096, 1<<20)
			source.DB = reopened
			c.Store = source
			p, err := c.ValidatePrefix(ctx, source)
			if err != nil {
				t.Fatal(err)
			}
			for p.Committed < 100 {
				p, err = c.Step(ctx, source, limits, a.Apply)
				if err != nil {
					t.Fatal(err)
				}
			}
			var total, uniqueIDs int
			if err := reopened.QueryRow(`SELECT count(*),count(DISTINCT source_id) FROM canonical_ingestion.mappings`).Scan(&total, &uniqueIDs); err != nil || total != 100 || uniqueIDs != 100 {
				t.Fatal("duplicate/missing replay", total, uniqueIDs, err)
			}
		})
	}
}
