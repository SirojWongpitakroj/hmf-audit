package cassandradht

import (
	"container/list"
	"errors"
	"sync"

	"dhtbacked/internal/hashing"
)

// Limits bound working memory independently of persistent log volume.
type Limits struct {
	Workers, MaxRequests, QueueBytes                        int
	MaxValueBytes, ProofBytes, AncestorMapBytes             int
	CacheBytes, CacheEntries, RecordFetchBatch, RecordBytes int
}

func DefaultLimits() Limits {
	return Limits{Workers: 8, MaxRequests: 32768, QueueBytes: 65536, MaxValueBytes: 65536, ProofBytes: 8 << 20, AncestorMapBytes: 32 << 20, CacheBytes: 0, CacheEntries: 0, RecordFetchBatch: 256, RecordBytes: 8 << 20}
}
func (l Limits) Validate() error {
	if l.Workers < 1 || l.Workers > 1024 || l.MaxRequests < 1 || l.QueueBytes < 32 || l.MaxValueBytes < 1 || l.ProofBytes < l.MaxValueBytes || l.AncestorMapBytes < l.ProofBytes || l.CacheBytes < 0 || l.CacheEntries < 0 || l.RecordFetchBatch < 1 || l.RecordBytes < 1 {
		return errors.New("cassandradht: explicit positive worker/request/queue/value/proof/record bounds required")
	}
	return nil
}

type cacheEntry struct {
	key   hashing.Digest
	value []byte
}
type boundedCache struct {
	mu                  sync.Mutex
	l                   Limits
	items               map[hashing.Digest]*list.Element
	order               *list.List
	bytes, hits, misses uint64
}

func newBoundedCache(l Limits) *boundedCache {
	return &boundedCache{l: l, items: map[hashing.Digest]*list.Element{}, order: list.New()}
}
func (c *boundedCache) get(k hashing.Digest) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.items[k]; e != nil {
		c.hits++
		c.order.MoveToFront(e)
		return append([]byte(nil), e.Value.(cacheEntry).value...), true
	}
	c.misses++
	return nil, false
}
func (c *boundedCache) put(k hashing.Digest, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cost := uint64(len(b) + 256)
	if c.l.CacheEntries == 0 || cost > uint64(c.l.CacheBytes) {
		return
	}
	if c.items[k] != nil {
		return
	}
	for len(c.items) >= c.l.CacheEntries || c.bytes+cost > uint64(c.l.CacheBytes) {
		e := c.order.Back()
		v := e.Value.(cacheEntry)
		delete(c.items, v.key)
		c.bytes -= uint64(len(v.value) + 256)
		c.order.Remove(e)
	}
	c.items[k] = c.order.PushFront(cacheEntry{k, append([]byte(nil), b...)})
	c.bytes += cost
}
func (c *boundedCache) snapshot() (bytes, entries, hits, misses uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes, uint64(len(c.items)), c.hits, c.misses
}
