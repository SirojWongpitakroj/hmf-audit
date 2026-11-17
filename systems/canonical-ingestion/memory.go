package canonicalingestion

import (
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"
)

type MemorySnapshot struct {
	TotalAllocBytes, GCCount, GCPauseNanos               uint64
	RSSAvailable                                         bool
	RSSBytes, HeapAllocBytes, HeapInuseBytes, GoSysBytes uint64
	HeapIdleBytes, HeapReleasedBytes, GoMemoryUsedBytes  uint64
	GoMemoryLimitBytes                                   int64
}

func ReadMemory() MemorySnapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	rss, ok := residentBytes()
	return MemorySnapshot{RSSAvailable: ok, RSSBytes: rss, HeapAllocBytes: m.HeapAlloc, HeapInuseBytes: m.HeapInuse, GoSysBytes: m.Sys, TotalAllocBytes: m.TotalAlloc, GCCount: uint64(m.NumGC), GCPauseNanos: m.PauseTotalNs,
		HeapIdleBytes: m.HeapIdle, HeapReleasedBytes: m.HeapReleased, GoMemoryUsedBytes: m.Sys - m.HeapReleased, GoMemoryLimitBytes: debug.SetMemoryLimit(-1)}
}

type MemoryMonitor struct {
	stop, done chan struct{}
	peak       atomic.Uint64
	heapPeak   atomic.Uint64
}

func StartMemoryMonitor(interval time.Duration) *MemoryMonitor {
	m := &MemoryMonitor{stop: make(chan struct{}), done: make(chan struct{})}
	rss, _ := residentBytes()
	m.peak.Store(rss)
	var initial runtime.MemStats
	runtime.ReadMemStats(&initial)
	m.heapPeak.Store(initial.HeapAlloc)
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				var heap runtime.MemStats
				runtime.ReadMemStats(&heap)
				if heap.HeapAlloc > m.heapPeak.Load() {
					m.heapPeak.Store(heap.HeapAlloc)
				}
				rss, ok := residentBytes()
				if ok && rss > m.peak.Load() {
					m.peak.Store(rss)
				}
			}
		}
	}()
	return m
}
func (m *MemoryMonitor) Stop() uint64 { close(m.stop); <-m.done; return m.peak.Load() }

func (m *MemoryMonitor) PeakHeap() uint64 { return m.heapPeak.Load() }
