package diskindex

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"blas/bitmap"
	"blas/iimt"
	"blas/omt"
	"blas/rimbt"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestCacheBounds(t *testing.T) {
	c, err := NewCache(Limits{PageBytes: 4096, CacheBytes: 2048, CacheEntries: 3, BitmapBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10000; i++ {
		c.put(fmt.Sprint(i), make([]byte, 256))
		s := c.Stats()
		if s.CacheBytes > 2048 || s.CacheEntries > 3 {
			t.Fatal(s)
		}
	}
	c.put("oversized", make([]byte, 2048))
	if _, ok := c.get("oversized"); ok {
		t.Fatal("oversized cached")
	}
}
func TestPersistentTreesMatchNative(t *testing.T) {
	dsn := os.Getenv("BLAS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("BLAS_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	scope := fmt.Sprintf("paged-test-%d", time.Now().UnixNano())
	limits := Limits{PageBytes: 1024, CacheBytes: 4096, CacheEntries: 10, BitmapBytes: 4096}
	s := Store{Q: db, Scope: scope, Limits: limits}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		db.Exec(`DELETE FROM blas.index_pages WHERE scope=$1`, scope)
		db.Exec(`DELETE FROM blas.index_leaves WHERE scope=$1`, scope)
	}()
	const n = 21
	words := []string{"a", "b", "c", "d", "e", "f", "g", "h", string([]byte{0xff, 0x80, 0, 1})}
	native, _ := iimt.NewEmpty(4)
	for _, w := range words {
		native.AddKeyword(w)
	}
	b, _ := NewBuilder(s, "iimt", "iimt", 4, n)
	for j, w := range words {
		bm := bitmap.New(n)
		for i := uint64(0); i < n; i++ {
			if int(i)%len(words) == j {
				bm.Set(i)
			}
		}
		if err = b.AddBitmap(ctx, w, 0, bm); err != nil {
			t.Fatal(err)
		}
	}
	for i := uint64(0); i < n; i++ {
		native.Insert(i, []string{words[int(i)%len(words)]})
	}
	meta, err := b.Finish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root, _ := native.Root()
	if root != meta.Root {
		t.Fatal("IIMT root changed")
	}
	rn, _ := rimbt.NewEmpty(4)
	values := []int64{-7, 0, 1, 15, 20}
	for i := uint64(0); i < n; i++ {
		rn.Insert(i, values[int(i)%len(values)])
	}
	rb, _ := NewBuilder(s, "rimbt", "rimbt", 4, n)
	for _, v := range values {
		bm := bitmap.New(n)
		for i := uint64(0); i < n; i++ {
			if values[int(i)%len(values)] <= v {
				bm.Set(i)
			}
		}
		if err = rb.AddBitmap(ctx, "", v, bm); err != nil {
			t.Fatal(err)
		}
	}
	meta, err = rb.Finish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rr, _ := rn.Root()
	if rr != meta.Root {
		t.Fatal("RIMBT root changed")
	}
	var leaves []omt.Leaf
	ob, _ := NewBuilder(s, "omt", "omt", 4, n)
	for i := uint64(0); i < n; i++ {
		o := []byte(fmt.Sprintf("record %d", i))
		leaves = append(leaves, omt.Leaf{OID: i, Object: o})
		if err = ob.AddObject(ctx, i, o); err != nil {
			t.Fatal(err)
		}
	}
	meta, err = ob.Finish(ctx)
	if err != nil {
		t.Fatal(err)
	}
	on, _ := omt.Build(4, leaves)
	or, _ := on.Root()
	if or != meta.Root {
		t.Fatal("OMT root changed")
	}
	// Reopen through a new connection/cache, without retaining a persistent tree.
	reopened, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	cache, _ := NewCache(limits)
	readerStore := Store{Q: reopened, Scope: scope, Limits: limits, Cache: cache}
	ir, err := Open(ctx, readerStore, "iimt", &Budget{MaxBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range words {
		wantQR, want, wantOK := native.Query(w)
		gotQR, got, gotOK := ir.Query(w)
		if ir.Err() != nil || wantOK != gotOK || !reflect.DeepEqual(wantQR, gotQR) || !reflect.DeepEqual(want, got) {
			t.Fatal("IIMT proof changed", w, ir.Err())
		}
		if !iimt.Verify(root, got) {
			t.Fatal("invalid IIMT proof")
		}
	}
	wantAbsent, _ := native.ProveAbsent("missing")
	gotAbsent, ok := ir.ProveAbsent("missing")
	if !ok || ir.Err() != nil || !reflect.DeepEqual(wantAbsent, gotAbsent) || !iimt.VerifyAbsent(root, n, "missing", gotAbsent) {
		t.Fatal("absence proof changed", ir.Err())
	}
	ranges := [][2]int64{{-100, -20}, {-7, -7}, {0, 15}, {2, 19}, {21, 30}, {-100, 100}, {0, 0}}
	for _, q := range ranges {
		dr, e := Open(ctx, readerStore, "rimbt", &Budget{MaxBytes: 1 << 20})
		if e != nil {
			t.Fatal(e)
		}
		want, _ := rn.RangeQuery(q[0], q[1])
		got, e := dr.RangeQuery(q[0], q[1])
		if e != nil || !reflect.DeepEqual(want, got) || !rimbt.VerifyRangeResult(rr, n, q[0], q[1], got) {
			t.Fatal("range proof changed", q, e)
		}
	}
	for i := uint64(0); i < n; i++ {
		dr, e := Open(ctx, readerStore, "omt", &Budget{MaxBytes: 1 << 20})
		if e != nil {
			t.Fatal(e)
		}
		want, _ := on.Prove(i)
		got, ok := dr.Prove(i)
		if !ok || dr.Err() != nil || !reflect.DeepEqual(want, got) || !omt.Verify(or, got) {
			t.Fatal("OMT proof changed", i, dr.Err())
		}
	}
	stats := cache.Stats()
	if stats.CacheBytes > uint64(limits.CacheBytes) || stats.CacheEntries > uint64(limits.CacheEntries) || stats.CacheHits == 0 || stats.CacheMisses == 0 || stats.StorageReads == 0 {
		t.Fatal(stats)
	}
	small, err := Open(ctx, readerStore, "iimt", &Budget{MaxBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	small.Query("a")
	if small.Err() == nil {
		t.Fatal("proof budget ignored")
	}
}
