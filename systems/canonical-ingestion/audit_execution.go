package canonicalingestion

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

// AuditExecution grants execution resources only. It is deliberately separate
// from persisted native configuration and canonical/workload fingerprints.
type AuditExecution struct {
	CPUWorkers  int `yaml:"cpu_workers"`
	IOWorkers   int `yaml:"io_workers"`
	SQLPoolSize int `yaml:"sql_pool_size"`
	MaxProcs    int `yaml:"max_procs"`
}

func DefaultAuditExecution() AuditExecution { return AuditExecution{2, 8, 6, 2} }

func (e AuditExecution) Validate() error {
	if e.CPUWorkers < 1 || e.CPUWorkers > 64 || e.IOWorkers < 1 || e.IOWorkers > 64 || e.SQLPoolSize < 2 || e.SQLPoolSize > 64 || e.MaxProcs < 1 || e.MaxProcs > 64 {
		return fmt.Errorf("audit execution limits must each be between 1 and 64; SQL pool needs at least two connections for the dataset lease and reads")
	}
	return nil
}

type auditExecutionKey struct{}

type auditActivityKey struct{}

// AuditActivity is request-local instrumentation, independent of verification.
type AuditActivity struct{ active, peak atomic.Int32 }

func TrackAuditActivity(ctx context.Context) (context.Context, *AuditActivity) {
	a := new(AuditActivity)
	return context.WithValue(ctx, auditActivityKey{}, a), a
}

func (a *AuditActivity) Peak() int { return int(a.peak.Load()) }

// WithAuditExecution does not mutate native adapters or their fingerprints.
// Callers without an execution policy retain the serial reference path.
func WithAuditExecution(ctx context.Context, e AuditExecution) context.Context {
	return context.WithValue(ctx, auditExecutionKey{}, e)
}

func AuditExecutionFrom(ctx context.Context) AuditExecution {
	if e, ok := ctx.Value(auditExecutionKey{}).(AuditExecution); ok {
		return e
	}
	return AuditExecution{1, 1, 1, 1}
}

func HasAuditExecution(ctx context.Context) bool {
	_, ok := ctx.Value(auditExecutionKey{}).(AuditExecution)
	return ok
}

// SetRuntime is process-wide; command entry points call it once before audits.
func (e AuditExecution) SetRuntime() error {
	if err := e.Validate(); err != nil {
		return err
	}
	runtime.GOMAXPROCS(e.MaxProcs)
	return nil
}

// AuditParallel runs independent, indexed jobs with a fixed worker pool and no
// buffered job queue. Each job owns its mutable verifier/reader state. The
// caller reduces indexed outputs in native order only after all workers join.
// Error selection is in input order, never worker-completion order.
func AuditParallel(ctx context.Context, jobs, workers int, run func(int) error) error {
	if jobs < 0 || jobs > 4096 || workers < 1 || workers > 64 || run == nil {
		return fmt.Errorf("invalid bounded audit worker/task count")
	}
	if activity, ok := ctx.Value(auditActivityKey{}).(*AuditActivity); ok {
		original := run
		run = func(i int) error {
			n := activity.active.Add(1)
			defer activity.active.Add(-1)
			for p := activity.peak.Load(); n > p && !activity.peak.CompareAndSwap(p, n); p = activity.peak.Load() {
			}
			return original(i)
		}
	}
	errors := make([]error, jobs)
	if workers == 1 {
		for i := 0; i < jobs; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := run(i); err != nil {
				return err
			}
		}
		return ctx.Err()
	}
	queue := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, jobs) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range queue {
				if ctx.Err() == nil {
					errors[i] = run(i)
				}
			}
		}()
	}
dispatch:
	for i := 0; i < jobs; i++ {
		select {
		case queue <- i:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(queue)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

// AddIndependent adds work, not wall time. Component durations may overlap in
// concurrent audits; EndToEnd must always be measured by the outer request.
func (m *AuditMetrics) AddIndependent(o AuditMetrics) {
	m.ApplicationCacheHits += o.ApplicationCacheHits
	m.ApplicationCacheMisses += o.ApplicationCacheMisses
	m.PhysicalRowsRead += o.PhysicalRowsRead
	m.PhysicalReadBytes += o.PhysicalReadBytes
	m.TransactionQueries += o.TransactionQueries
	m.TransactionRowsLoaded += o.TransactionRowsLoaded
	m.TransactionLogicalVisits += o.TransactionLogicalVisits
	m.HistoryStateRowsRead += o.HistoryStateRowsRead
	m.HistoryRecordRowsRead += o.HistoryRecordRowsRead
	m.SignatureChecks += o.SignatureChecks
	m.LogicalHistoryRecords += o.LogicalHistoryRecords
	m.QueryResolution += o.QueryResolution
	m.ProofConstruction += o.ProofConstruction
	m.Retrieval += o.Retrieval
	m.Verification += o.Verification
	m.EvidenceBytes += o.EvidenceBytes
	m.BitmapBytes += o.BitmapBytes
	m.StorageReads += o.StorageReads
	m.StorageReadBytes += o.StorageReadBytes
	m.HistoryStates += o.HistoryStates
	m.HistoryBatches += o.HistoryBatches
	m.HistoryRecords += o.HistoryRecords
	m.HistoryBytes += o.HistoryBytes
	m.NativeQueries += o.NativeQueries
	m.RequestedRecords += o.RequestedRecords
	m.ReturnedRecords += o.ReturnedRecords
	m.AuthHashInvocations += o.AuthHashInvocations
}
