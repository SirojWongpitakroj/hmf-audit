package durable

import (
	"bytes"
	"context"
	"database/sql"
	"reflect"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

// Mode 0 is the original per-keyword lookup, mode 1 the batch prefetch, and
// mode 2 a prefetch whose tiny budget mixes both paths inside one batch. All
// must store identical rows, KIDs, checkpoints and native IDs, across a
// rolled-back step and a reopen.
func TestKeywordPrefetchMatchesPerKeywordLookups(t *testing.T) {
	es := generatorEntries(257)
	var snapshots [3]string
	var checkpoints [3][]byte
	var ids [3][]string
	var sqlBatches [3]uint64
	for mode := range 3 {
		db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
		a := generatorAdapter(db)
		a.DisableKeywordPrefetch = mode == 0
		if mode == 2 {
			a.Execution.CacheMaxEntries = 3
			a.Execution.CacheMaxBytes = 512
		}
		hash := a.Options.ConfigHash()
		if err := a.Migrate(context.Background()); err != nil {
			t.Fatal(err)
		}
		p := contract.Progress{Target: 257}
		step := func(n int, commit bool) {
			t.Helper()
			tx, err := a.DB.Begin()
			if err != nil {
				t.Fatal(err)
			}
			got, cp, err := a.Apply(context.Background(), tx, p, es[p.Committed:p.Committed+uint64(n)])
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
		before := generatorKeywordSnapshot(t, db)
		step(23, false)
		if before != generatorKeywordSnapshot(t, db) {
			t.Fatal("rollback leaked native state")
		}
		step(13, true)
		db.Close()
		reopened, err := contract.OpenPostgres(context.Background(), dsn, 6)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { reopened.Close() })
		a.DB = reopened
		for p.Committed < 257 {
			step(min(37, int(257-p.Committed)), true)
		}
		if a.Options.ConfigHash() != hash {
			t.Fatal("execution switch changed fingerprint")
		}
		snapshots[mode] = generatorKeywordSnapshot(t, reopened)
		checkpoints[mode] = p.Checkpoint
		sqlBatches[mode] = a.Metrics.SQLBatches.Load()
	}
	for mode := 1; mode < 3; mode++ {
		if snapshots[mode] != snapshots[0] || !bytes.Equal(checkpoints[mode], checkpoints[0]) || !reflect.DeepEqual(ids[mode], ids[0]) {
			t.Fatal("keyword prefetch changed stored BLAS state", mode)
		}
	}
	if sqlBatches[1] >= sqlBatches[0] {
		t.Fatal("prefetch did not reduce insert statements", sqlBatches)
	}
}

func generatorKeywordSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()
	return testfixture.Snapshot(t, db,
		`SELECT kid,keyword FROM blas.durable_keywords ORDER BY kid`,
		`SELECT lid,kid,keyword FROM blas.durable_generator_keywords ORDER BY lid,kid`,
		`SELECT kind,owner,oid,object,keywords,lids,ts_min,ts_max FROM blas.durable_objects ORDER BY kind,owner,oid`,
		`SELECT fid,block_height,entries,metadata FROM blas.durable_files ORDER BY fid`,
		`SELECT height,segment,payload FROM blas.durable_headers ORDER BY height`,
		`SELECT * FROM blas.index_pages ORDER BY scope,tree,page`,
		`SELECT * FROM blas.index_leaves ORDER BY scope,tree,pos`)
}
