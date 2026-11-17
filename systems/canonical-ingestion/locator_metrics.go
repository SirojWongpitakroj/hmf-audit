package canonicalingestion

import "sync/atomic"

type PageCopyCounter struct{ Count, Bytes atomic.Uint64 }
type PageCopySample struct{ Count, Bytes uint64 }
type PageCopySites struct{ CacheRead, CacheWrite, DirtyRead, StorageRead PageCopyCounter }
type PageCopySitesSnapshot struct{ CacheRead, CacheWrite, DirtyRead, StorageRead PageCopySample }

func (c *PageCopyCounter) Observe(bytes uint64) { c.Count.Add(1); c.Bytes.Add(bytes) }
func (c *PageCopyCounter) snapshot() PageCopySample {
	return PageCopySample{c.Count.Load(), c.Bytes.Load()}
}
func (s *PageCopySites) Snapshot() PageCopySitesSnapshot {
	return PageCopySitesSnapshot{CacheRead: s.CacheRead.snapshot(), CacheWrite: s.CacheWrite.snapshot(), DirtyRead: s.DirtyRead.snapshot(), StorageRead: s.StorageRead.snapshot()}
}

// LocatorCacheSnapshot describes decoded locator RAM, not database size or RSS.
// Pages counts clean cached pages; dirty pages have their own independent bound.
type LocatorCacheSnapshot struct {
	Available                                                            bool
	Bytes, CleanBytes, DirtyBytes, MaxBytes, EvictionThresholdBytes      int64
	Pages, DirtyPages, MaxPages                                          int
	Hits, Misses, Evictions, EvictionsOldEpoch, Bypasses, FinalizedEpoch uint64
}
