package cassandradht

import (
	"context"
	"testing"
)

func TestColdAuditCacheAlwaysRoutesAndFetchesAgain(t *testing.T) {
	d, kv, g, requests := boundedFixture(t)
	for run := range 35 {
		if err := d.ClearAuditCache(context.Background()); err != nil {
			t.Fatal(err)
		}
		before := d.Stats()
		if before.CacheBytes != 0 || before.CacheEntries != 0 {
			t.Fatal("warm start", run)
		}
		for range 2 {
			if _, err := d.GetMany(&g.token, requests[:2]); err != nil {
				t.Fatal(err)
			}
		}
		after := d.Stats()
		if after.ProofReads-before.ProofReads != 2 || after.CacheHits-before.CacheHits != 2 || after.CacheMisses-before.CacheMisses != 2 || after.RoutingReads-before.RoutingReads != 8 {
			t.Fatal("cross-run reuse or routing bypass", before, after)
		}
		if err := d.ClearAuditCache(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if kv.reads.Load() != 70 || kv.routes.Load() != 280 {
		t.Fatal("unexpected storage operations", kv.reads.Load(), kv.routes.Load())
	}
}
