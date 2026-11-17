package canonicalingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"runtime"
	"sync/atomic"
	"time"
)

type OperationMetrics struct {
	Count, Failures, Nanos atomic.Uint64
	buckets                [32]atomic.Uint64
}

func (m *OperationMetrics) Observe(d time.Duration, err error) {
	if m == nil {
		return
	}
	m.Count.Add(1)
	m.Nanos.Add(uint64(max(d, 0)))
	micros := uint64(max(d.Microseconds(), 1))
	bucket := 0
	for micros > 1 && bucket < 31 {
		micros >>= 1
		bucket++
	}
	m.buckets[bucket].Add(1)
	if err != nil {
		m.Failures.Add(1)
	}
}

type OperationSnapshot struct {
	Count, Failures                  uint64
	MeanMilliseconds                 float64
	P50Milliseconds                  float64
	P95Milliseconds, P99Milliseconds float64
}

func (m *OperationMetrics) percentile(q float64) float64 {
	target := uint64(float64(m.Count.Load())*q + 0.999999)
	if target == 0 {
		return 0
	}
	var n uint64
	for i := range m.buckets {
		n += m.buckets[i].Load()
		if n >= target {
			return float64(uint64(1)<<uint(i+1)) / 1000
		}
	}
	return 0
}

func (m *OperationMetrics) snapshot() OperationSnapshot {
	n := m.Count.Load()
	return OperationSnapshot{Count: n, Failures: m.Failures.Load(), MeanMilliseconds: float64(m.Nanos.Load()) / float64(max(n, 1)) / 1e6, P50Milliseconds: m.percentile(.50), P95Milliseconds: m.percentile(.95), P99Milliseconds: m.percentile(.99)}
}

// IngestMetrics has fixed storage, irrespective of dataset/history size.
type IngestMetrics struct {
	// Configure before attaching to any worker. Detailed timers are opt-in.
	ProfileLocator                                                                                       bool
	Access                                                                                               AccessMetrics
	CacheObserver                                                                                        func() (int, int)
	LocatorCacheObserver                                                                                 func() LocatorCacheSnapshot
	SQLTraced                                                                                            atomic.Bool
	Records, Batches, BatchBytes, SQLBatches, Retries, FailedBatches, Recoveries                         atomic.Uint64
	AcceptedRecords                                                                                      atomic.Uint64
	BatchAdmittedRecords                                                                                 atomic.Uint64
	AdmissionBatch                                                                                       OperationMetrics
	SQL, Admission, Cassandra                                                                            OperationMetrics
	Input, Prepared, Reorder, ReservedBytes                                                              atomic.Int64
	PeakInput, PeakPrepared, PeakReorder, PeakReservedBytes                                              atomic.Int64
	InFlight, PeakInFlight                                                                               atomic.Int64
	PendingBatchBytes, PeakPendingBatchBytes, CacheEntries, CacheBytes, PeakCacheEntries, PeakCacheBytes atomic.Int64
	StorageQueueRecords, StorageQueueBytes, PeakStorageQueueRecords, PeakStorageQueueBytes               atomic.Int64
}

func ReadProcessCPUSeconds() (float64, bool) { return processCPUSeconds() }
func (m *IngestMetrics) TrackPendingBatchBytes(n int64) {
	if m == nil {
		return
	}
	m.PendingBatchBytes.Store(n)
	peak(&m.PeakPendingBatchBytes, n)
}
func (m *IngestMetrics) TrackStorageQueue(records, bytes int64) {
	if m == nil {
		return
	}
	m.StorageQueueRecords.Store(records)
	m.StorageQueueBytes.Store(bytes)
	peak(&m.PeakStorageQueueRecords, records)
	peak(&m.PeakStorageQueueBytes, bytes)
}
func (m *IngestMetrics) TrackCache(entries, bytes int64) {
	if m == nil {
		return
	}
	m.CacheEntries.Store(entries)
	m.CacheBytes.Store(bytes)
	peak(&m.PeakCacheEntries, entries)
	peak(&m.PeakCacheBytes, bytes)
}

func (m *IngestMetrics) ObserveSQL(d time.Duration, err error) {
	if m != nil && !m.SQLTraced.Load() {
		m.SQL.Observe(d, err)
	}
}

func peak(p *atomic.Int64, n int64) {
	for old := p.Load(); n > old && !p.CompareAndSwap(old, n); old = p.Load() {
	}
}
func (m *IngestMetrics) Pipeline(input, prepared, reorder, bytes int64) {
	m.Input.Store(input)
	m.Prepared.Store(prepared)
	m.Reorder.Store(reorder)
	m.ReservedBytes.Store(bytes)
	peak(&m.PeakInput, input)
	peak(&m.PeakPrepared, prepared)
	peak(&m.PeakReorder, reorder)
	peak(&m.PeakReservedBytes, bytes)
}
func (m *IngestMetrics) Committed(records, bytes int) {
	if m == nil {
		return
	}
	m.Records.Add(uint64(records))
	m.BatchBytes.Add(uint64(bytes))
	m.Batches.Add(1)
}
func (m *IngestMetrics) StorageStarted() { n := m.InFlight.Add(1); peak(&m.PeakInFlight, n) }
func (m *IngestMetrics) StorageFinished(d time.Duration, err error) {
	m.InFlight.Add(-1)
	m.Cassandra.Observe(d, err)
}

// Reporter emits aggregate SQL/storage timings only where instrumented; counts
// accompany every latency, so an unobserved backend is never presented as fast.
func (m *IngestMetrics) Reporter(ctx context.Context, out io.Writer, e Execution, databases map[string]*sql.DB) func() {
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		start, previous := time.Now(), time.Now()
		var prior uint64
		var priorAccepted uint64
		priorCPU, cpuOK := processCPUSeconds()
		emit := func(final bool) {
			now := time.Now()
			if m.CacheObserver != nil {
				entries, bytes := m.CacheObserver()
				m.TrackCache(int64(entries), int64(bytes))
			}
			n := m.Records.Load()
			var locator *LocatorCacheSnapshot
			if m.LocatorCacheObserver != nil {
				snapshot := m.LocatorCacheObserver()
				locator = &snapshot
			}
			batches := m.Batches.Load()
			mem := ReadMemory()
			cpu, ok := processCPUSeconds()
			var percent *float64
			if cpuOK && ok {
				v := (cpu - priorCPU) / max(now.Sub(previous).Seconds(), 1e-9) * 100
				percent = &v
			}
			pools := make(map[string]sql.DBStats, len(databases))
			for name, db := range databases {
				if db != nil {
					pools[name] = db.Stats()
				}
			}
			_ = json.NewEncoder(out).Encode(struct {
				Type                                                                                                 string `json:"type"`
				Final                                                                                                bool   `json:"final"`
				Records                                                                                              uint64 `json:"records_committed_this_process"`
				AcceptedRecords                                                                                      uint64 `json:"records_staged_this_process"`
				RecordsPerSecond, RollingRecordsPerSecond, BatchesPerSecond                                          float64
				AcceptedRecordsPerSecond, RollingAcceptedRecordsPerSecond                                            float64
				AverageBatchRecords, AverageBatchBytes                                                               float64
				CPUPercentOneCore                                                                                    *float64
				Memory                                                                                               MemorySnapshot
				LocatorCache                                                                                         *LocatorCacheSnapshot `json:",omitempty"`
				RSSAboveTarget                                                                                       bool
				PendingBatchBytes, PeakPendingBatchBytes, CacheEntries, CacheBytes, PeakCacheEntries, PeakCacheBytes int64
				InputQueue, PreparedQueue, ReorderRecords, ReservedBytes                                             int64
				PeakInputQueue, PeakPreparedQueue, PeakReorderRecords, PeakReservedBytes                             int64
				StorageQueueRecords, StorageQueueBytes, PeakStorageQueueRecords, PeakStorageQueueBytes               int64
				StorageInFlight, PeakStorageInFlight                                                                 int64
				PostgreSQL, Admission, AdmissionBatch, Cassandra                                                     OperationSnapshot
				BatchAdmittedRecords                                                                                 uint64
				SQLBatchStatements, Retries, FailedBatches, Recoveries                                               uint64
				CPUWorkers, IOWorkers, Goroutines, MaxProcs                                                          int
				Databases                                                                                            map[string]sql.DBStats
				StorageAccess                                                                                        AccessSnapshot
			}{
				Type: "ingestion_metrics", Final: final, Records: n, AcceptedRecords: m.AcceptedRecords.Load(),
				StorageAccess:                   m.Access.Snapshot(),
				LocatorCache:                    locator,
				RecordsPerSecond:                float64(n) / max(now.Sub(start).Seconds(), 1e-9),
				RollingRecordsPerSecond:         float64(n-prior) / max(now.Sub(previous).Seconds(), 1e-9),
				BatchesPerSecond:                float64(batches) / max(now.Sub(start).Seconds(), 1e-9),
				AcceptedRecordsPerSecond:        float64(m.AcceptedRecords.Load()) / max(now.Sub(start).Seconds(), 1e-9),
				RollingAcceptedRecordsPerSecond: float64(m.AcceptedRecords.Load()-priorAccepted) / max(now.Sub(previous).Seconds(), 1e-9),
				AverageBatchRecords:             float64(n) / float64(max(batches, 1)), AverageBatchBytes: float64(m.BatchBytes.Load()) / float64(max(batches, 1)),
				CPUPercentOneCore: percent, Memory: mem, RSSAboveTarget: e.RSSTargetBytes > 0 && mem.RSSAvailable && mem.RSSBytes > e.RSSTargetBytes,
				PendingBatchBytes: m.PendingBatchBytes.Load(), PeakPendingBatchBytes: m.PeakPendingBatchBytes.Load(),
				CacheEntries: m.CacheEntries.Load(), CacheBytes: m.CacheBytes.Load(), PeakCacheEntries: m.PeakCacheEntries.Load(), PeakCacheBytes: m.PeakCacheBytes.Load(),
				InputQueue: m.Input.Load(), PreparedQueue: m.Prepared.Load(), ReorderRecords: m.Reorder.Load(), ReservedBytes: m.ReservedBytes.Load(),
				PeakInputQueue: m.PeakInput.Load(), PeakPreparedQueue: m.PeakPrepared.Load(), PeakReorderRecords: m.PeakReorder.Load(), PeakReservedBytes: m.PeakReservedBytes.Load(),
				StorageQueueRecords: m.StorageQueueRecords.Load(), StorageQueueBytes: m.StorageQueueBytes.Load(), PeakStorageQueueRecords: m.PeakStorageQueueRecords.Load(), PeakStorageQueueBytes: m.PeakStorageQueueBytes.Load(),
				StorageInFlight: m.InFlight.Load(), PeakStorageInFlight: m.PeakInFlight.Load(), PostgreSQL: m.SQL.snapshot(), Admission: m.Admission.snapshot(), AdmissionBatch: m.AdmissionBatch.snapshot(), BatchAdmittedRecords: m.BatchAdmittedRecords.Load(), Cassandra: m.Cassandra.snapshot(),
				SQLBatchStatements: m.SQLBatches.Load(), Retries: m.Retries.Load(), FailedBatches: m.FailedBatches.Load(), Recoveries: m.Recoveries.Load(),
				CPUWorkers: e.CPUWorkers, IOWorkers: e.IOWorkers, Goroutines: runtime.NumGoroutine(), MaxProcs: runtime.GOMAXPROCS(0), Databases: pools,
			})
			prior, previous, priorCPU, cpuOK = n, now, cpu, ok
			priorAccepted = m.AcceptedRecords.Load()
		}
		if e.MetricsInterval == 0 {
			<-stop
			emit(true)
			return
		}
		ticker := time.NewTicker(e.MetricsInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				emit(true)
				return
			case <-ctx.Done():
				emit(true)
				return
			case <-ticker.C:
				emit(false)
			}
		}
	}()
	return func() { close(stop); <-done }
}
