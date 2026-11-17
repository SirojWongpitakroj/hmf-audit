package canonicalingestion

import (
	"sync/atomic"
	"time"
)

// PublicationMetrics are cumulative client elapsed phases, including failed and
// speculative preparation. Preparation overlaps append: do not sum their times
// to infer wall time. No payloads or per-record samples are retained here.
type PublicationMetrics struct {
	JournalReadNanos, SourceReadNanos, EncodeValidateNanos            atomic.Uint64
	PrepareNanos, PrepareBlockedNanos, WriterWaitNanos                atomic.Uint64
	SetupNanos, ManifestReadNanos, AppendNanos                        atomic.Uint64
	AppendValidationNanos, AppendBeginLockNanos, AppendBuildRowsNanos atomic.Uint64
	InsertNanos, UpdateNanos, CommitNanos                             atomic.Uint64
	PreparedRecords, PreparedBatches                                  atomic.Uint64
	PeakPreparedBatchBytes                                            atomic.Int64
}

type PublicationSnapshot struct {
	JournalReadNanos, SourceReadNanos, EncodeValidateNanos            uint64
	PrepareNanos, PrepareBlockedNanos, WriterWaitNanos                uint64
	SetupNanos, ManifestReadNanos, AppendNanos                        uint64
	AppendValidationNanos, AppendBeginLockNanos, AppendBuildRowsNanos uint64
	InsertNanos, UpdateNanos, CommitNanos                             uint64
	PreparedRecords, PreparedBatches                                  uint64
	PeakPreparedBatchBytes                                            int64
}

func (m *PublicationMetrics) Snapshot() PublicationSnapshot {
	return PublicationSnapshot{
		JournalReadNanos: m.JournalReadNanos.Load(), SourceReadNanos: m.SourceReadNanos.Load(), EncodeValidateNanos: m.EncodeValidateNanos.Load(),
		PrepareNanos: m.PrepareNanos.Load(), PrepareBlockedNanos: m.PrepareBlockedNanos.Load(), WriterWaitNanos: m.WriterWaitNanos.Load(),
		SetupNanos: m.SetupNanos.Load(), ManifestReadNanos: m.ManifestReadNanos.Load(), AppendNanos: m.AppendNanos.Load(),
		AppendValidationNanos: m.AppendValidationNanos.Load(), AppendBeginLockNanos: m.AppendBeginLockNanos.Load(), AppendBuildRowsNanos: m.AppendBuildRowsNanos.Load(),
		InsertNanos: m.InsertNanos.Load(), UpdateNanos: m.UpdateNanos.Load(), CommitNanos: m.CommitNanos.Load(),
		PreparedRecords: m.PreparedRecords.Load(), PreparedBatches: m.PreparedBatches.Load(), PeakPreparedBatchBytes: m.PeakPreparedBatchBytes.Load(),
	}
}

func (m *PublicationMetrics) Prepared(records, bytes int) {
	m.PreparedRecords.Add(uint64(records))
	m.PreparedBatches.Add(1)
	peak(&m.PeakPreparedBatchBytes, int64(bytes))
}

func (m *PublicationMetrics) ObserveAppend(result AppendResult, elapsed time.Duration) {
	m.AppendNanos.Add(uint64(elapsed))
	t := result.Timings
	m.AppendValidationNanos.Add(uint64(t.Validation))
	m.AppendBeginLockNanos.Add(uint64(t.BeginLock))
	m.AppendBuildRowsNanos.Add(uint64(t.BuildRows))
	m.InsertNanos.Add(uint64(t.Insert))
	m.UpdateNanos.Add(uint64(t.Update))
	m.CommitNanos.Add(uint64(t.Commit))
}
