package diskindex

import (
	"blas/bhash"
	"blas/bitmap"
	"canonicalingestion/testfixture"
	"context"
	"fmt"
	"testing"
)

func TestPreparedBitmapMatchesOriginalBytesAndOwnership(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	limits := Limits{PageBytes: 4096, BitmapBytes: 4096}
	if err := (Store{Q: db, Scope: "prepared", Limits: limits}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint64{1, 7, 8, 9, 16, 65} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var roots [2]bhash.Digest
			var snapshots [2]string
			for mode := range 2 {
				tree := fmt.Sprintf("tree-%d-%d", n, mode)
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				batch, _ := NewWriteBatch(tx, 7, 4096, nil)
				builder, err := NewBuilder(Store{Q: tx, Scope: "prepared", Limits: limits, Batch: batch}, tree, "iimt", 4, n)
				if err != nil {
					t.Fatal(err)
				}
				zero := PrepareBitmap(bitmap.New(n))
				for i, word := range []string{"", "a", "absent", "α", "long-constant-keyword"} {
					bm := bitmap.New(n)
					if i%2 == 1 {
						bm.Set(uint64(i) % n)
					}
					if mode == 0 {
						err = builder.AddBitmap(ctx, word, 0, bm)
					} else {
						input := zero
						if i%2 == 1 {
							input = PrepareBitmap(bm)
							bm.Set((uint64(i) + 1) % n)
						}
						err = builder.AddPreparedKeywordBitmap(ctx, PrepareKeyword(word), input)
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
				t.Fatal("immutable input reuse changed tree or aliased mutable bitmap")
			}
		})
	}
}

func TestPreparedBitmapRejectsWrongPopulationAndUninitializedInput(t *testing.T) {
	for _, kind := range []string{"iimt", "rimbt", "omt"} {
		b, err := NewBuilder(Store{Limits: Limits{PageBytes: 1024, BitmapBytes: 4096}}, "invalid", kind, 4, 9)
		if err != nil {
			t.Fatal(err)
		}
		if err := b.AddPreparedKeywordBitmap(context.Background(), PrepareKeyword("key"), PrepareBitmap(bitmap.New(8))); err == nil {
			t.Fatal("wrong population accepted")
		}
		if err := b.AddPreparedKeywordBitmap(context.Background(), PrepareKeyword("key"), PreparedBitmap{}); err == nil {
			t.Fatal("uninitialized bitmap accepted")
		}
		if err := b.AddPreparedKeywordBitmap(context.Background(), PreparedKeyword{}, PrepareBitmap(bitmap.New(9))); err == nil {
			t.Fatal("uninitialized keyword accepted")
		}
	}
}
