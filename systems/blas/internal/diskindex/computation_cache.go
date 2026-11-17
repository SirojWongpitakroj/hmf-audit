package diskindex

import (
	"container/list"
	"sync"
	"time"

	"blas/bhash"
)

// ComputationCache memoizes only H(exact bytes). It is never a page cache or
// authoritative working state: it can safely survive an aborted SQL transaction.
// Complete inputs, not their digests or node positions, are compared as keys.
type ComputationCache struct {
	mu                   sync.Mutex
	segment              string
	maxEntries, maxBytes int
	items                map[string]*list.Element
	lru                  list.List
	stats                ComputationStats
}

type computedHash struct {
	input string
	hash  bhash.Digest
	cost  int
}
type ComputationStats struct {
	Requests, Hits, Computations, HashedBytes, HashNanos, Resets uint64
	Entries, Bytes, PeakEntries, PeakBytes                       int
}

func NewComputationCache(entries, bytes int) *ComputationCache {
	return &ComputationCache{maxEntries: max(entries, 0), maxBytes: max(bytes, 0), items: make(map[string]*list.Element)}
}

// BeginSegment bounds retention to the current existing native segment. This
// changes no IDs, segment size, tree naming or durable state.
func (c *ComputationCache) BeginSegment(segment string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.segment == segment {
		return
	}
	c.segment = segment
	c.items = make(map[string]*list.Element)
	c.lru.Init()
	c.stats.Entries = 0
	c.stats.Bytes = 0
	c.stats.Resets++
}

func (c *ComputationCache) Stats() ComputationStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

func (c *ComputationCache) Hash(data []byte) bhash.Digest {
	c.mu.Lock()
	c.stats.Requests++
	if e, ok := c.items[string(data)]; ok {
		c.lru.MoveToFront(e)
		c.stats.Hits++
		h := e.Value.(computedHash).hash
		c.mu.Unlock()
		return h
	}
	c.mu.Unlock()
	started := time.Now()
	h := bhash.H(data)
	elapsed := time.Since(started)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stats.Computations++
	c.stats.HashedBytes += uint64(len(data))
	c.stats.HashNanos += uint64(elapsed)
	cost := len(data) + 192
	if c.maxEntries == 0 || cost > c.maxBytes {
		return h
	}
	if _, ok := c.items[string(data)]; ok {
		return h
	}
	for len(c.items) >= c.maxEntries || c.stats.Bytes > c.maxBytes-cost {
		e := c.lru.Back()
		v := e.Value.(computedHash)
		delete(c.items, v.input)
		c.lru.Remove(e)
		c.stats.Bytes -= v.cost
	}
	// Own the key; callers may reuse or mutate their input buffers after Hash.
	input := string(data)
	c.items[input] = c.lru.PushFront(computedHash{input, h, cost})
	c.stats.Entries = len(c.items)
	c.stats.Bytes += cost
	c.stats.PeakEntries = max(c.stats.PeakEntries, c.stats.Entries)
	c.stats.PeakBytes = max(c.stats.PeakBytes, c.stats.Bytes)
	return h
}

func (b *Builder) hash(data []byte) bhash.Digest {
	if b.Store.Computations != nil {
		return b.Store.Computations.Hash(data)
	}
	return bhash.H(data)
}
func (b *Builder) hashConcat(parts ...[]byte) bhash.Digest {
	if b.Store.Computations == nil {
		return bhash.HashConcat(parts...)
	}
	size := 0
	for _, p := range parts {
		size += len(p)
	}
	input := make([]byte, 0, size)
	for _, p := range parts {
		input = append(input, p...)
	}
	return b.Store.Computations.Hash(input)
}
