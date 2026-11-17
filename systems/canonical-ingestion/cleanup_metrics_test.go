package canonicalingestion

import "testing"

func TestCleanupTelemetrySnapshot(t *testing.T) {
	var m AccessMetrics
	m.CleanupCalls.Add(1)
	m.CleanupSQLCalls.Add(2)
	m.CleanupPageRowsDeleted.Add(7)
	m.CleanupLeafRowsDeleted.Add(3)
	m.CleanupRowsDeleted.Add(10)
	m.CleanupTreesRequested.Add(6)
	m.CleanupNanos.Add(40)
	m.CleanupPagesNanos.Add(20)
	m.CleanupLeavesNanos.Add(15)
	m.CleanupSegment.Store(2)
	m.CleanupGeneration.Store(998)
	s := m.Snapshot()
	if s.CleanupCalls != 1 || s.CleanupSQLCalls != 2 || s.CleanupPageRowsDeleted != 7 || s.CleanupLeafRowsDeleted != 3 || s.CleanupRowsDeleted != 10 || s.CleanupTreesRequested != 6 || s.CleanupNanos != 40 || s.CleanupPagesNanos != 20 || s.CleanupLeavesNanos != 15 || s.CleanupSegment != 2 || s.CleanupGeneration != 998 {
		t.Fatal("cleanup telemetry missing from native/supervisor snapshot", s)
	}
}
