package canonicalingestion

import "sync/atomic"

// Optional inclusive wall-clock phase counters. These overlap (lookup includes
// cache/decode/copy; dirty insertion includes hashing), so never sum them.
type LocatorWorkMetrics struct {
	LookupNanos, CopyNanos, HashNanos, SizeNanos, DirtyNanos, CacheNanos, DecodeNanos, SerializeNanos atomic.Uint64
}

type LocatorWorkSnapshot struct {
	LookupNanos, CopyNanos, HashNanos, SizeNanos, DirtyNanos, CacheNanos, DecodeNanos, SerializeNanos uint64
}

func (m *LocatorWorkMetrics) Snapshot() LocatorWorkSnapshot {
	return LocatorWorkSnapshot{m.LookupNanos.Load(), m.CopyNanos.Load(), m.HashNanos.Load(), m.SizeNanos.Load(), m.DirtyNanos.Load(), m.CacheNanos.Load(), m.DecodeNanos.Load(), m.SerializeNanos.Load()}
}
