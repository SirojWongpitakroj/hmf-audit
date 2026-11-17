package canonicalingestion

import (
	"container/list"
	"context"
	"fmt"
	"sync"
)

// AuditReadOptions affect execution only, never native dataset fingerprints.
type AuditReadOptions struct {
	BatchRows    int  `yaml:"batch_rows"`
	CacheEntries int  `yaml:"cache_entries"`
	CacheBytes   int  `yaml:"cache_bytes"`
	Legacy       bool `yaml:"legacy"`
}

func DefaultAuditReads() AuditReadOptions {
	return AuditReadOptions{BatchRows: 256, CacheEntries: 64, CacheBytes: 8 << 20}
}
func (o AuditReadOptions) Validate() error {
	if o.BatchRows < 1 || o.BatchRows > 1000 || o.CacheEntries < 0 || o.CacheEntries > 4096 || o.CacheBytes < 0 || o.CacheBytes > 256<<20 {
		return fmt.Errorf("invalid bounded audit read policy")
	}
	return nil
}

type auditReadsKey struct{}

func WithAuditReads(ctx context.Context, o AuditReadOptions) context.Context {
	return context.WithValue(ctx, auditReadsKey{}, o)
}
func AuditReadsFrom(ctx context.Context) AuditReadOptions {
	if o, ok := ctx.Value(auditReadsKey{}).(AuditReadOptions); ok {
		return o
	}
	return DefaultAuditReads()
}

type ReadCacheStats struct{ Hits, Misses, Bytes, Entries, PeakBytes, Evictions, Bypasses uint64 }
type readEntry[K comparable, V any] struct {
	key   K
	value V
	cost  uint64
}

// AuditReadCache is request-local. Values must be immutable after loading.
// A single cache-fill owner prevents duplicate concurrent SQL reads. No SQL
// transaction is shared: the loader runs on its calling reader/transaction.
type AuditReadCache[K comparable, V any] struct {
	mu     sync.Mutex
	items  map[K]*list.Element
	lru    *list.List
	limits AuditReadOptions
	stats  ReadCacheStats
}

func NewAuditReadCache[K comparable, V any](o AuditReadOptions) *AuditReadCache[K, V] {
	return &AuditReadCache[K, V]{items: make(map[K]*list.Element), lru: list.New(), limits: o}
}
func (c *AuditReadCache[K, V]) Get(ctx context.Context, key K, load func() (V, int, error)) (value V, hit bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err = ctx.Err(); err != nil {
		return
	}
	if item := c.items[key]; item != nil {
		c.lru.MoveToFront(item)
		c.stats.Hits++
		return item.Value.(readEntry[K, V]).value, true, nil
	}
	c.stats.Misses++
	value, cost, err := load()
	if err != nil {
		return value, false, err
	}
	if cost < 0 {
		return value, false, fmt.Errorf("negative audit cache size")
	}
	if c.limits.CacheEntries == 0 || cost > c.limits.CacheBytes {
		c.stats.Bypasses++
		return value, false, nil
	}
	for c.lru.Len() > 0 && (c.lru.Len() >= c.limits.CacheEntries || c.stats.Bytes+uint64(cost) > uint64(c.limits.CacheBytes)) {
		old := c.lru.Back()
		entry := old.Value.(readEntry[K, V])
		delete(c.items, entry.key)
		c.stats.Bytes -= entry.cost
		c.lru.Remove(old)
		c.stats.Evictions++
	}
	c.items[key] = c.lru.PushFront(readEntry[K, V]{key, value, uint64(cost)})
	c.stats.Bytes += uint64(cost)
	c.stats.Entries = uint64(c.lru.Len())
	c.stats.PeakBytes = max(c.stats.PeakBytes, c.stats.Bytes)
	return value, false, nil
}
func (c *AuditReadCache[K, V]) Stats() ReadCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}
