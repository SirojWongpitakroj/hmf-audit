package canonicalingestion

import (
	"testing"
	"time"
)

func TestPublicationMetricSnapshotAndAppendPhases(t *testing.T) {
	m := &AccessMetrics{}
	p := &m.PublicationWork
	p.SetupNanos.Add(1)
	p.ManifestReadNanos.Add(2)
	p.JournalReadNanos.Add(3)
	p.SourceReadNanos.Add(4)
	p.EncodeValidateNanos.Add(5)
	p.PrepareNanos.Add(12)
	p.WriterWaitNanos.Add(6)
	p.PrepareBlockedNanos.Add(7)
	p.Prepared(3, 100)
	p.Prepared(2, 80)
	p.ObserveAppend(AppendResult{Timings: AppendTimings{Validation: 8, BeginLock: 9, BuildRows: 10, Insert: 11, Update: 12, Commit: 13}}, 100*time.Nanosecond)
	s := m.Snapshot().PublicationWork
	want := PublicationSnapshot{SetupNanos: 1, ManifestReadNanos: 2, JournalReadNanos: 3, SourceReadNanos: 4, EncodeValidateNanos: 5, PrepareNanos: 12, WriterWaitNanos: 6, PrepareBlockedNanos: 7, PreparedRecords: 5, PreparedBatches: 2, PeakPreparedBatchBytes: 100, AppendNanos: 100, AppendValidationNanos: 8, AppendBeginLockNanos: 9, AppendBuildRowsNanos: 10, InsertNanos: 11, UpdateNanos: 12, CommitNanos: 13}
	if s != want {
		t.Fatal(s, want)
	}
}
