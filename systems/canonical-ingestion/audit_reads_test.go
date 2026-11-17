package canonicalingestion

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAuditReadCacheBoundsRetryAndConcurrentFill(t *testing.T) {
	ctx := context.Background()
	p := DefaultAuditReads()
	p.CacheEntries = 2
	p.CacheBytes = 8
	c := NewAuditReadCache[int, string](p)
	fail := true
	load := func() (string, int, error) {
		if fail {
			return "", 0, errors.New("injected read failure")
		}
		return "data", 4, nil
	}
	if _, _, e := c.Get(ctx, 1, load); e == nil {
		t.Fatal("read failure lost")
	}
	fail = false
	if _, hit, e := c.Get(ctx, 1, load); e != nil || hit {
		t.Fatal("failed read incorrectly cached", e)
	}
	for _, key := range []int{2, 3} {
		if _, _, e := c.Get(ctx, key, load); e != nil {
			t.Fatal(e)
		}
	}
	st := c.Stats()
	if st.Entries != 2 || st.Bytes != 8 || st.PeakBytes > 8 || st.Evictions != 1 {
		t.Fatal(st)
	}
	if _, hit, e := c.Get(ctx, 4, func() (string, int, error) { return "oversized", 9, nil }); e != nil || hit {
		t.Fatal(e)
	}
	if c.Stats().Bypasses != 1 {
		t.Fatal(c.Stats())
	}
	concurrent := NewAuditReadCache[int, string](p)
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, _, e := concurrent.Get(ctx, 1, func() (string, int, error) { calls.Add(1); return "data", 4, nil })
			if e != nil || v != "data" {
				t.Error(v, e)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 || concurrent.Stats().Hits != 15 {
		t.Fatal(calls.Load(), concurrent.Stats())
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, e := concurrent.Get(cancelled, 1, load); !errors.Is(e, context.Canceled) {
		t.Fatal("cancelled read accepted", e)
	}
}
