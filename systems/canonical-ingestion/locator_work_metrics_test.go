package canonicalingestion

import "testing"

func TestLocatorPhaseCountersAreBoundedAndForwarded(t *testing.T) {
	m := &IngestMetrics{ProfileLocator: true}
	p := &m.Access.LocatorWork
	p.LookupNanos.Add(100)
	p.CopyNanos.Add(20)
	p.HashNanos.Add(30)
	p.SizeNanos.Add(10)
	p.DirtyNanos.Add(50)
	p.CacheNanos.Add(5)
	p.DecodeNanos.Add(6)
	p.SerializeNanos.Add(7)
	want := LocatorWorkSnapshot{100, 20, 30, 10, 50, 5, 6, 7}
	if got := m.Access.Snapshot().LocatorWork; got != want {
		t.Fatalf("lost locator phases: %+v", got)
	}
}
