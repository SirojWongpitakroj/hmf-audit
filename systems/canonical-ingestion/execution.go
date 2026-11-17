package canonicalingestion

import (
	"errors"
	"flag"
	"runtime"
	"runtime/debug"
	"time"
)

// Execution contains scheduling budgets only. It must never enter a native
// configuration fingerprint or change a file, batch, segment or epoch boundary.
type Execution struct {
	StorageAccessOptimizations                        bool // Execution-only; never fingerprinted.
	CPUWorkers, IOWorkers, SQLPoolSize, MaxProcs      int
	BatchMaxRecords, BatchMaxBytes                    int
	InputQueueRecords, PreparedQueueRecords           int
	QueueMaxBytes, ReorderMaxRecords, ReorderMaxBytes int
	CacheMaxEntries, CacheMaxBytes                    int
	MetricsInterval                                   time.Duration
	HeapSoftLimitBytes                                int64
	RSSTargetBytes                                    uint64
}

func DefaultExecution() Execution {
	return Execution{StorageAccessOptimizations: true, CPUWorkers: min(16, runtime.GOMAXPROCS(0)), IOWorkers: 32,
		SQLPoolSize: 16, BatchMaxRecords: 1000, BatchMaxBytes: 8 << 20,
		InputQueueRecords: 128, PreparedQueueRecords: 128, QueueMaxBytes: 128 << 20,
		ReorderMaxRecords: 1000, ReorderMaxBytes: 64 << 20,
		CacheMaxEntries: 4096, CacheMaxBytes: 8 << 20, MetricsInterval: 10 * time.Second}
}

func (e *Execution) Flags(f *flag.FlagSet) {
	f.BoolVar(&e.StorageAccessOptimizations, "storage-access-optimizations", e.StorageAccessOptimizations, "bounded acknowledged-state caches and consolidated ingestion reads; false retains prior access path")
	f.IntVar(&e.CPUWorkers, "cpu-workers", e.CPUWorkers, "bounded preparation workers (1 selects serial preparation)")
	f.IntVar(&e.IOWorkers, "io-workers", e.IOWorkers, "independent storage concurrency, at most 64")
	f.IntVar(&e.SQLPoolSize, "sql-pool-size", e.SQLPoolSize, "connections per SQL database, including control leases")
	f.IntVar(&e.MaxProcs, "max-procs", e.MaxProcs, "Go CPU parallelism; 0 preserves Go's available-CPU setting")
	f.IntVar(&e.BatchMaxRecords, "batch-max-records", e.BatchMaxRecords, "maximum persistence/preparation batch records")
	f.IntVar(&e.BatchMaxBytes, "batch-max-bytes", e.BatchMaxBytes, "maximum persistence/preparation batch bytes")
	f.IntVar(&e.InputQueueRecords, "input-queue-records", e.InputQueueRecords, "input channel capacity")
	f.IntVar(&e.PreparedQueueRecords, "prepared-queue-records", e.PreparedQueueRecords, "prepared channel capacity")
	f.IntVar(&e.QueueMaxBytes, "queue-max-bytes", e.QueueMaxBytes, "combined in-flight preparation byte budget")
	f.IntVar(&e.ReorderMaxRecords, "reorder-max-records", e.ReorderMaxRecords, "maximum dispatched but unconsumed records")
	f.IntVar(&e.ReorderMaxBytes, "reorder-max-bytes", e.ReorderMaxBytes, "maximum dispatched but unconsumed reserved bytes")
	f.IntVar(&e.CacheMaxEntries, "execution-cache-entries", e.CacheMaxEntries, "maximum execution cache entries")
	f.IntVar(&e.CacheMaxBytes, "execution-cache-bytes", e.CacheMaxBytes, "maximum execution cache bytes")
	f.DurationVar(&e.MetricsInterval, "metrics-interval", e.MetricsInterval, "ingestion telemetry interval; 0 disables periodic output")
	f.Int64Var(&e.HeapSoftLimitBytes, "heap-soft-limit-bytes", e.HeapSoftLimitBytes, "optional Go runtime soft memory limit; 0 preserves the runtime setting")
	f.Uint64Var(&e.RSSTargetBytes, "rss-target-bytes", e.RSSTargetBytes, "observational RSS target; does not replace queue/cache limits")
}

func (e Execution) Validate() error {
	if e.CPUWorkers < 1 || e.CPUWorkers > 256 || e.IOWorkers < 1 || e.IOWorkers > 64 || e.SQLPoolSize < 3 || e.SQLPoolSize > 256 || e.MaxProcs < 0 || e.MaxProcs > 256 ||
		e.BatchMaxRecords < 1 || e.BatchMaxRecords > 10000 || e.BatchMaxBytes < 1 || e.InputQueueRecords < 1 || e.PreparedQueueRecords < 1 || e.QueueMaxBytes < 1 || e.ReorderMaxRecords < 1 || e.ReorderMaxBytes < 1 || e.CacheMaxEntries < 0 || e.CacheMaxBytes < 0 || e.MetricsInterval < 0 || e.HeapSoftLimitBytes < 0 {
		return errors.New("invalid explicit ingestion execution limits")
	}
	return nil
}

func (e Execution) ConfigureRuntime() error {
	if err := e.Validate(); err != nil {
		return err
	}
	if e.MaxProcs > 0 {
		runtime.GOMAXPROCS(e.MaxProcs)
	}
	if e.HeapSoftLimitBytes > 0 {
		debug.SetMemoryLimit(e.HeapSoftLimitBytes)
	}
	return nil
}

// EffectiveLimits preserves legacy flags as additional bounds.
func (e Execution) StorageQueueBytes() int { return min(e.BatchMaxBytes, e.QueueMaxBytes/4) }
func (e Execution) PreparationBytes() int {
	return min(e.ReorderMaxBytes, e.QueueMaxBytes-e.StorageQueueBytes())
}
func (e Execution) EffectiveLimits(l Limits) Limits {
	l.MaxRecords = min(l.MaxRecords, e.BatchMaxRecords)
	// Leave room for decoded fields, immutable encryption tasks, encoded native
	// records and SQL parameters in addition to the source payload itself.
	capacity := e.PreparationBytes()
	l.MaxRecords = min(l.MaxRecords, capacity/8192)
	l.MaxBatchBytes = min(l.MaxBatchBytes, e.BatchMaxBytes, (capacity-l.MaxRecords*4096)/8)
	return l
}
