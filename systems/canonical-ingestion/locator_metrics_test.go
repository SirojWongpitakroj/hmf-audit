package canonicalingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"runtime/debug"
	"testing"
)

func TestLocatorTelemetryIsSeparateFromPlacementCache(t *testing.T) {
	m := &IngestMetrics{}
	m.CacheObserver = func() (int, int) { return 2, 32854 }
	want := LocatorCacheSnapshot{Available: true, Bytes: 150000, CleanBytes: 100000, DirtyBytes: 50000, Pages: 8, DirtyPages: 2, MaxPages: 16, MaxBytes: 200000, EvictionThresholdBytes: 150000, Hits: 51, Misses: 3, Evictions: 9, EvictionsOldEpoch: 7}
	m.LocatorCacheObserver = func() LocatorCacheSnapshot { return want }
	m.Access.PageCopySites.CacheRead.Observe(2048)
	m.Access.PageCopySites.DirtyRead.Observe(4096)
	var output bytes.Buffer
	e := DefaultExecution()
	e.MetricsInterval = 0
	stop := m.Reporter(context.Background(), &output, e, nil)
	stop()
	var got struct {
		CacheBytes    int
		LocatorCache  *LocatorCacheSnapshot
		Memory        MemorySnapshot
		StorageAccess AccessSnapshot
	}
	if err := json.Unmarshal(output.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.LocatorCache == nil || *got.LocatorCache != want || got.CacheBytes != 32854 {
		t.Fatalf("locator and placement telemetry mixed: %+v", got)
	}
	if got.Memory.HeapReleasedBytes > got.Memory.HeapIdleBytes || got.Memory.HeapIdleBytes+got.Memory.HeapInuseBytes == 0 || got.Memory.GoMemoryUsedBytes+got.Memory.HeapReleasedBytes != got.Memory.GoSysBytes {
		t.Fatal("inconsistent runtime memory telemetry", got.Memory)
	}
	if got.StorageAccess.PageCopySites.CacheRead.Bytes != 2048 || got.StorageAccess.PageCopySites.DirtyRead.Count != 1 {
		t.Fatal("copy site telemetry missing")
	}
}

func TestRuntimeSoftMemoryLimitAndPreservation(t *testing.T) {
	before := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(before) })
	e := DefaultExecution()
	e.HeapSoftLimitBytes = 512 << 20
	if err := e.ConfigureRuntime(); err != nil {
		t.Fatal(err)
	}
	if got := ReadMemory(); got.GoMemoryLimitBytes != 512<<20 {
		t.Fatal("configured memory guardrail not reported", got.GoMemoryLimitBytes)
	}
	e.HeapSoftLimitBytes = 0
	if err := e.ConfigureRuntime(); err != nil {
		t.Fatal(err)
	}
	if debug.SetMemoryLimit(-1) != 512<<20 {
		t.Fatal("zero override changed existing runtime memory limit")
	}
	e.HeapSoftLimitBytes = -1
	if e.ConfigureRuntime() == nil {
		t.Fatal("negative runtime limit accepted")
	}
}
