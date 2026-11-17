package canonicalingestion

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"
)

// AuditFlags supplies generic measurement mechanics. Experiment-specific audit
// sizes, warmups and repetitions are supplied by each experiment's own runner.
type AuditFlags struct {
	ReadBatchRows, ReadCacheEntries, ReadCacheBytes  *int
	LegacyReads                                      *bool
	Progress                                         *bool
	ProgressLabel                                    *string
	Request, Output                                  *string
	Warmups, Runs, MaxRecords, MaxBytes, RecordBytes *int
	Interval                                         *time.Duration
	RSSLimit                                         *uint64
	CPUWorkers, IOWorkers, SQLPoolSize, MaxProcs     *int
	// CountAuthHashes runs the authentication-cost counting pass: targets are
	// verified one at a time and verification hash invocations are counted.
	CountAuthHashes *bool
}

func BindAuditFlags(f *flag.FlagSet) AuditFlags {
	return AuditFlags{
		ReadBatchRows:    f.Int("audit-read-batch-rows", 256, "BLAS transaction read chunk bound"),
		ReadCacheEntries: f.Int("audit-read-cache-entries", 64, "request-local immutable read cache entries"),
		ReadCacheBytes:   f.Int("audit-read-cache-bytes", 8<<20, "request-local immutable read cache bytes"),
		LegacyReads:      f.Bool("audit-legacy-reads", false, "reference read path for equivalence measurements"),
		Progress:         f.Bool("progress", true, "write concise per-measurement progress to stderr"),
		ProgressLabel:    f.String("progress-label", "", "optional experiment/milestone prefix for progress lines"),
		CPUWorkers:       f.Int("audit-cpu-workers", 2, "independent audit CPU jobs"),
		IOWorkers:        f.Int("audit-io-workers", 8, "independent audit I/O concurrency budget"),
		SQLPoolSize:      f.Int("audit-sql-pool-size", 6, "audit PostgreSQL connection limit"),
		MaxProcs:         f.Int("audit-max-procs", 2, "audit GOMAXPROCS"),
		CountAuthHashes:  f.Bool("count-auth-hashes", false, "authentication-cost pass: serial targets, count verification hash invocations"),
		Request:          f.String("request", "", "immutable canonical audit request JSON"), Output: f.String("output", "", "new report file; empty writes JSON to stdout"), Warmups: f.Int("warmups", 0, "unmeasured native warmups"), Runs: f.Int("runs", 1, "measured repetitions"), MaxRecords: f.Int("max-audit-records", 4096, "request record bound"), MaxBytes: f.Int("max-audit-bytes", 8<<20, "request byte bound"), RecordBytes: f.Int("record-bytes", 1<<20, "single canonical record byte bound"), Interval: f.Duration("memory-interval", 100*time.Millisecond, "RSS sampling interval"), RSSLimit: f.Uint64("rss-limit-bytes", 0, "optional client RSS validation limit; zero records RSS without declaring an RSS budget pass")}
}
func (f AuditFlags) WithReads(ctx context.Context) (context.Context, error) {
	o := AuditReadOptions{BatchRows: *f.ReadBatchRows, CacheEntries: *f.ReadCacheEntries, CacheBytes: *f.ReadCacheBytes, Legacy: *f.LegacyReads}
	if err := o.Validate(); err != nil {
		return ctx, err
	}
	return WithAuditReads(ctx, o), nil
}

// WithAuthCount applies the counting pass to ctx when requested.
func (f AuditFlags) WithAuthCount(ctx context.Context) context.Context {
	if *f.CountAuthHashes {
		return WithAuthHashCount(ctx)
	}
	return ctx
}
func (f AuditFlags) WithProgress(ctx context.Context) context.Context {
	if !*f.Progress {
		return ctx
	}
	return WithAuditProgress(ctx, os.Stderr, *f.ProgressLabel)
}
func (f AuditFlags) Execution() AuditExecution {
	return AuditExecution{*f.CPUWorkers, *f.IOWorkers, *f.SQLPoolSize, *f.MaxProcs}
}
func (f AuditFlags) Limits() Limits {
	return Limits{MaxRecords: *f.MaxRecords, MaxBatchBytes: *f.MaxBytes, MaxRecordBytes: *f.RecordBytes}
}
func (f AuditFlags) Finish(report AuditReport) error {
	for _, sample := range report.Samples {
		if report.ReadPolicy != nil && sample.Metrics.RequestCachePeakBytes > uint64(report.ReadPolicy.CacheBytes) {
			return fmt.Errorf("request-local read cache exceeds configured byte limit")
		}
		if !sample.MemoryAfter.RSSAvailable {
			return fmt.Errorf("RSS unavailable on this platform; cannot issue a memory validation report")
		}
		if *f.RSSLimit > 0 && sample.PeakRSSBytes > *f.RSSLimit {
			return fmt.Errorf("client RSS %d exceeds configured limit %d", sample.PeakRSSBytes, *f.RSSLimit)
		}
	}
	return WriteAuditReport(*f.Output, report)
}
