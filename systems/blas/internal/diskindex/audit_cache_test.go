package diskindex

import "testing"

func TestAuditCacheClearDropsPagesPreservesCounters(t *testing.T) {
	c, err := NewCache(Limits{PageBytes: 128, CacheBytes: 4096, CacheEntries: 8, BitmapBytes: 128})
	if err != nil {
		t.Fatal(err)
	}
	c.PutValue("tree/page", []byte("authenticated page"))
	c.GetValue("tree/page")
	before := c.Stats()
	c.Clear()
	after := c.Stats()
	if after.CacheBytes != 0 || after.CacheEntries != 0 || before.CacheHits != after.CacheHits {
		t.Fatal("cache reset changed counters", after)
	}
	if _, hit := c.GetValue("tree/page"); hit {
		t.Fatal("page survived reset")
	}
}
