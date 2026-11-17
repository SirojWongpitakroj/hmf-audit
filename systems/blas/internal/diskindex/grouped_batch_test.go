package diskindex

import (
	"context"
	"fmt"
	"testing"

	"blas/bhash"
	"blas/bitmap"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestGroupedTreesKeepRowsUnderRecordAndByteLimits(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	limits := Limits{PageBytes: 4096, BitmapBytes: 4096}
	if err := (Store{Q: db, Scope: "grouped", Limits: limits}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, bound := range []struct{ rows, bytes int }{{7, 1 << 20}, {1000, 2048}} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			var snapshots [2]string
			var roots [2][]bhash.Digest
			for mode := range 2 {
				tx, _ := db.Begin()
				metrics := &contract.IngestMetrics{}
				batch, _ := NewWriteBatch(tx, bound.rows, bound.bytes, metrics)
				batch.HoldFinish = mode == 1
				prefix := fmt.Sprintf("%d/%d/%d/", bound.rows, bound.bytes, mode)
				for _, name := range []string{"a", "b", "c", "d"} {
					b, err := NewBuilder(Store{Q: tx, Scope: "grouped", Limits: limits, Batch: batch}, prefix+name, "iimt", 4, 1024)
					if err != nil {
						t.Fatal(err)
					}
					for i := range 17 {
						bm := bitmap.New(1024)
						bm.Set(uint64(i))
						if err := b.AddBitmap(ctx, fmt.Sprint(i), 0, bm); err != nil {
							t.Fatal(err)
						}
					}
					meta, err := b.Finish(ctx)
					if err != nil {
						t.Fatal(err)
					}
					roots[mode] = append(roots[mode], meta.Root)
				}
				if err := batch.Flush(ctx); err != nil {
					t.Fatal(err)
				}
				if batch.rows != 0 || batch.bytes != 0 || metrics.Access.PeakIndexBatchRows.Load() > int64(bound.rows) || metrics.Access.PeakIndexBatchBytes.Load() > int64(bound.bytes) {
					t.Fatal("batch capacity/flush violated")
				}
				if metrics.Access.IndexCapacityFlushes.Load() == 0 {
					t.Fatal("hard capacity did not force flush")
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				snapshots[mode] = testfixture.Snapshot(t, db, `SELECT substr(tree,length('`+prefix+`')+1),page,payload FROM blas.index_pages WHERE tree LIKE '`+prefix+`%' ORDER BY tree,page`, `SELECT substr(tree,length('`+prefix+`')+1),pos,keyword,value FROM blas.index_leaves WHERE tree LIKE '`+prefix+`%' ORDER BY tree,pos`)
			}
			if snapshots[0] != snapshots[1] || fmt.Sprint(roots[0]) != fmt.Sprint(roots[1]) {
				t.Fatal("grouped builder changed complete persisted generation")
			}
		})
	}
}
