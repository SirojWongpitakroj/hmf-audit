package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestConcurrentBLASOldStateEquivalenceAndRollback(t *testing.T) {
	for _, aggregation := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "aggregation"}[aggregation], func(t *testing.T) {
			var snapshots [2]string
			var checkpoints [2][]byte
			var nativeIDs [2][]string
			for mode := 0; mode < 2; mode++ {
				db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
				a := Adapter{DB: db, Options: Options{RunID: "same-native-identity", EntriesPerFile: 16, FilesPerBlock: 4, Fanout: 4, SegmentSize: 4, Aggregation: aggregation, Index: diskindex.Limits{PageBytes: 4096, CacheBytes: 1 << 20, CacheEntries: 128, BitmapBytes: 1 << 20}, ObjectBytes: 1 << 20, QueryBytes: 16 << 20}}
				if err := a.Migrate(context.Background()); err != nil {
					t.Fatal(err)
				}
				fingerprint := a.Options.ConfigHash()
				p := contract.Progress{Target: 100}
				apply := func(fn contract.Apply, n int, commit bool) {
					t.Helper()
					tx, err := a.DB.BeginTx(context.Background(), nil)
					if err != nil {
						t.Fatal(err)
					}
					ids, checkpoint, err := fn(context.Background(), tx, p, testfixture.Entries(p.Committed+1, n))
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					if len(ids) != n {
						t.Fatal("lost native mappings")
					}
					if !commit {
						tx.Rollback()
						return
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					p.Committed += uint64(n)
					p.Checkpoint = checkpoint
					nativeIDs[mode] = append(nativeIDs[mode], ids...)
				}
				apply(a.applySequentialReference, 100, true)
				p.Target = 257
				var committed state
				if err := json.Unmarshal(p.Checkpoint, &committed); err != nil {
					t.Fatal(err)
				}
				prefixQueries := []string{fmt.Sprintf(`SELECT fid,metadata FROM blas.durable_files WHERE fid<%d ORDER BY fid`, committed.Files), fmt.Sprintf(`SELECT height,payload FROM blas.durable_headers WHERE height<%d ORDER BY height`, committed.Blocks)}
				prefix := testfixture.Snapshot(t, a.DB, prefixQueries...)
				if mode == 1 {
					e := contract.DefaultExecution()
					e.CPUWorkers = 8
					a.Execution = &e
					if a.Options.ConfigHash() != fingerprint {
						t.Fatal("execution changed semantic fingerprint")
					}
					apply(a.Apply, 23, false)
					var count int
					if err := db.QueryRow(`SELECT count(*) FROM blas.durable_objects WHERE kind='entry'`).Scan(&count); err != nil || count != 100 {
						t.Fatal("rollback changed entries", count, err)
					}
					db.Close()
					reopened, err := contract.OpenPostgres(context.Background(), dsn, 16)
					if err != nil {
						t.Fatal(err)
					}
					defer reopened.Close()
					a.DB = reopened
				}
				for p.Committed < 257 {
					fn := a.applySequentialReference
					if mode == 1 {
						fn = a.Apply
					}
					apply(fn, min(23, int(257-p.Committed)), true)
				}
				if prefix != testfixture.Snapshot(t, a.DB, prefixQueries...) {
					t.Fatal("committed files/headers changed after upgrade")
				}
				snapshots[mode] = testfixture.Snapshot(t, a.DB, `SELECT kid,keyword FROM blas.durable_keywords ORDER BY kid`, `SELECT kind,owner,oid,object,keywords,lids,ts_min,ts_max FROM blas.durable_objects ORDER BY kind,owner,oid`, `SELECT fid,block_height,entries,metadata FROM blas.durable_files ORDER BY fid`, `SELECT height,segment,payload FROM blas.durable_headers ORDER BY height`, `SELECT * FROM blas.index_pages ORDER BY scope,tree,page`, `SELECT * FROM blas.index_leaves ORDER BY scope,tree,pos`)
				checkpoints[mode] = p.Checkpoint
			}
			if snapshots[0] != snapshots[1] || !bytes.Equal(checkpoints[0], checkpoints[1]) || !slices.Equal(nativeIDs[0], nativeIDs[1]) {
				t.Fatal("BLAS native artifacts differ", snapshots)
			}
		})
	}
}
