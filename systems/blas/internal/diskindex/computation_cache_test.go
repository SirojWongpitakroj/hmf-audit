package diskindex

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"testing"

	"blas/bhash"
	"blas/bitmap"
	"canonicalingestion/testfixture"
)

func TestComputationCacheExactInputOwnershipBoundsAndReset(t *testing.T) {
	c := NewComputationCache(3, 800)
	c.BeginSegment("run/0")
	input := []byte("immutable input")
	want := bhash.H(input)
	if c.Hash(input) != want {
		t.Fatal("wrong hash")
	}
	input[0] = 'x'
	if c.Hash([]byte("immutable input")) != want || c.Stats().Hits != 1 {
		t.Fatal("cache key aliased input")
	}
	for i := range 100 {
		data := bytes.Repeat([]byte{byte(i)}, i*10)
		if c.Hash(data) != bhash.H(data) {
			t.Fatal("eviction changed hash")
		}
		s := c.Stats()
		if s.Bytes > 800 || s.Entries > 3 {
			t.Fatal("cache grew", s)
		}
	}
	c.BeginSegment("run/1")
	s := c.Stats()
	if s.Entries != 0 || s.Bytes != 0 || s.Resets != 2 {
		t.Fatal("previous segment retained", s)
	}
	// Only pure computations are shared. Concurrent requests are safe, and no
	// caller may receive a result for a different full byte sequence.
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := range 50 {
				data := []byte(fmt.Sprintf("%d/%d", i, j))
				if c.Hash(data) != bhash.H(data) {
					t.Error("wrong concurrent hash")
				}
			}
		}(i)
	}
	wg.Wait()
	if s := c.Stats(); s.Requests != s.Hits+s.Computations {
		t.Fatal("hash accounting differs", s)
	}
	disabled := NewComputationCache(0, 0)
	for range 2 {
		if disabled.Hash([]byte("same")) != bhash.H([]byte("same")) {
			t.Fatal("disabled cache changed result")
		}
	}
	if s := disabled.Stats(); s.Hits != 0 || s.Entries != 0 || s.Computations != 2 {
		t.Fatal("disabled cache retained inputs", s)
	}
}

func TestSegmentComputationMemoPreservesEveryStoredPage(t *testing.T) {
	db, _ := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
	ctx := context.Background()
	limits := Limits{PageBytes: 1024, BitmapBytes: 4096}
	if err := (Store{Q: db, Scope: "memo", Limits: limits}).Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"iimt", "rimbt"} {
		t.Run(kind, func(t *testing.T) {
			memo := NewComputationCache(4096, 1<<20)
			memo.BeginSegment("0")
			// Bitmap populations are mathematical inputs, not ingested records.
			for _, n := range []uint64{1, 2, 7, 8, 9, 15, 16, 17, 32, 127, 128, 129, 255, 256, 257, 511, 512, 513, 767, 768, 769, 1023, 1024} {
				var roots [2]bhash.Digest
				var snapshots [2]string
				for mode := range 2 {
					tx, err := db.Begin()
					if err != nil {
						t.Fatal(err)
					}
					tree := fmt.Sprintf("%s/%d/%d", kind, n, mode)
					batch, _ := NewWriteBatch(tx, 1000, 1<<20, nil)
					store := Store{Q: tx, Scope: "memo", Limits: limits, Batch: batch}
					if mode == 1 {
						store.Computations = memo
					}
					b, err := NewBuilder(store, tree, kind, 4, n)
					if err != nil {
						t.Fatal(err)
					}
					for i := range 17 {
						bm := bitmap.New(n)
						for pos := uint64(0); pos < n; pos++ {
							if pos <= uint64(i) {
								bm.Set(pos)
							}
						}
						if err := b.AddBitmap(ctx, fmt.Sprintf("word-%02d", i), int64(i)-9, bm); err != nil {
							t.Fatal(err)
						}
					}
					meta, err := b.Finish(ctx)
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
					t.Fatal("memo changed authenticated generation", n)
				}
			}
			if memo.Stats().Hits == 0 {
				t.Fatal("reuse was not exercised")
			}
		})
	}
}
