package diskindex

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"blas/bhash"
	"blas/bitmap"
	"blas/iimt"
	"canonicalingestion/testfixture"
)

func TestExplicitZeroCompactionRootsProofsAndMissingEvidence(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	limits := Limits{PageBytes: 1024, BitmapBytes: 8192}
	if err := (Store{Q: db, Limits: limits}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range []uint64{1, 7, 8, 9, 65, 4096} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var roots [2]bhash.Digest
			var proofs [2][]iimt.Proof
			var counts [2]int
			for mode := range 2 {
				tree := fmt.Sprintf("zero-%d-%d", n, mode)
				tx, _ := db.Begin()
				batch, _ := NewWriteBatch(tx, 7, 8192, nil)
				s := Store{Q: tx, Scope: "zero", Limits: limits, Batch: batch, CompactZeroBitmaps: mode == 1}
				b, err := NewBuilder(s, tree, "iimt", 4, n)
				if err != nil {
					t.Fatal(err)
				}
				for i, word := range []string{"absent-a", "absent-b", "present"} {
					bm := bitmap.New(n)
					if i == 2 {
						bm.Set(0)
					}
					if err := b.AddBitmap(ctx, word, 0, bm); err != nil {
						t.Fatal(err)
					}
				}
				m, err := b.Finish(ctx)
				if err != nil {
					t.Fatal(err)
				}
				roots[mode] = m.Root
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				if err := db.QueryRow(`SELECT count(*) FROM blas.index_pages WHERE scope='zero' AND tree=$1 AND page LIKE 'bitmap/%'`, tree).Scan(&counts[mode]); err != nil {
					t.Fatal(err)
				}
				r, err := Open(ctx, Store{Q: db, Scope: "zero", Limits: limits}, tree, &Budget{MaxBytes: 1 << 20})
				if err != nil {
					t.Fatal(err)
				}
				for _, word := range []string{"absent-a", "absent-b", "present"} {
					_, p, ok := r.Query(word)
					if !ok || r.Err() != nil || !iimt.Verify(m.Root, p) {
						t.Fatal("zero reconstruction proof rejected", word, r.Err())
					}
					proofs[mode] = append(proofs[mode], p)
				}
			}
			if roots[0] != roots[1] || !reflect.DeepEqual(proofs[0], proofs[1]) || counts[1] != 1 || counts[0] != 3 {
				t.Fatal("zero compaction changed logical leaves or failed to remove payload copies", counts)
			}
			tree := fmt.Sprintf("zero-%d-1", n)
			// Missing nonzero bitmap evidence must fail, never become implicit zero.
			if _, err := db.Exec(`DELETE FROM blas.index_pages WHERE scope='zero' AND tree=$1 AND page='bitmap/2/0'`, tree); err != nil {
				t.Fatal(err)
			}
			r, _ := Open(ctx, Store{Q: db, Scope: "zero", Limits: limits}, tree, &Budget{MaxBytes: 1 << 20})
			_, _, ok := r.Query("present")
			if ok && r.Err() == nil {
				t.Fatal("missing evidence silently accepted")
			}
			// A forged zero marker for a nonzero leaf cannot verify the trusted root.
			data, _ := json.Marshal(leaf{Keyword: []byte("present"), Zero: true})
			if _, err := db.Exec(`UPDATE blas.index_pages SET payload=$3 WHERE scope='zero' AND tree=$1 AND page=$2`, tree, "leaf/2", data); err != nil {
				t.Fatal(err)
			}
			r, _ = Open(ctx, Store{Q: db, Scope: "zero", Limits: limits}, tree, &Budget{MaxBytes: 1 << 20})
			_, p, _ := r.Query("present")
			if iimt.Verify(roots[1], p) {
				t.Fatal("forged zero marker verified")
			}
		})
	}
}
