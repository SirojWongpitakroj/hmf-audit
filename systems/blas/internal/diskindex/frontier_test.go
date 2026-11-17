package diskindex

import (
	"blas/bhash"
	"blas/bitmap"
	"canonicalingestion/testfixture"
	"context"
	"fmt"
	"testing"
)

func TestBatchedFrontierMatchesOriginalStoredPages(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	limits := Limits{PageBytes: 4096, CacheBytes: 0, CacheEntries: 0, BitmapBytes: 4096}
	if err := (Store{Q: db, Scope: "fixture", Limits: limits}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"iimt", "rimbt", "omt", "aggregate"} {
		for _, n := range []int{1, 2, 3, 4, 5, 15, 16, 17, 31, 64} {
			t.Run(fmt.Sprintf("%s/%d", kind, n), func(t *testing.T) {
				var snapshots [2]string
				var roots [2]bhash.Digest
				for mode := 0; mode < 2; mode++ {
					tree := fmt.Sprintf("%s-%d-%d", kind, n, mode)
					tx, err := db.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					store := Store{Q: tx, Scope: "fixture", Limits: limits}
					if mode == 1 {
						batch, err := NewWriteBatch(tx, 7, 4096, nil)
						if err != nil {
							t.Fatal(err)
						}
						store.Batch = batch
					}
					builder, err := NewBuilder(store, tree, kind, 4, uint64(n))
					if err != nil {
						t.Fatal(err)
					}
					for i := 0; i < n; i++ {
						switch kind {
						case "iimt", "rimbt":
							bm := bitmap.New(uint64(n))
							bm.Set(uint64(i))
							err = builder.AddBitmap(ctx, fmt.Sprint(i), int64(i), bm)
						case "omt":
							err = builder.AddObject(ctx, uint64(i), []byte(fmt.Sprint(i)))
						case "aggregate":
							err = builder.AddHash(ctx, bhash.H([]byte(fmt.Sprint(i))))
						}
						if err != nil {
							tx.Rollback()
							t.Fatal(err)
						}
					}
					meta, err := builder.Finish(ctx)
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					roots[mode] = meta.Root
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					snapshots[mode] = testfixture.Snapshot(t, db, `SELECT page,payload FROM blas.index_pages WHERE tree='`+tree+`' ORDER BY page`, `SELECT pos,keyword,value FROM blas.index_leaves WHERE tree='`+tree+`' ORDER BY pos`)
				}
				if roots[0] != roots[1] || snapshots[0] != snapshots[1] {
					t.Fatal("streamed construction changed root or stored proof pages", roots, snapshots)
				}
			})
		}
	}
}
