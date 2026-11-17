package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/signal"

	"blas/internal/diskindex"
	"blas/internal/durable"
	contract "canonicalingestion"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	env := flag.String("env-file", "", "deployment environment profile")
	runID := flag.String("run", "", "persistent BLAS run identity")
	entries := flag.Int("entries-per-file", 4096, "ingestion geometry")
	files := flag.Int("files-per-block", 256, "ingestion geometry")
	fanout := flag.Int("fanout", 4, "ingestion geometry")
	segment := flag.Uint64("segment-size", 1024, "ingestion geometry")
	aggregate := flag.Bool("aggregation", false, "ingestion Optimization-2 setting")
	generatorMode := flag.String("generator-mode", "device-v1", "must match the ingestion generator mode; empty selects legacy")
	scan := flag.Bool("segment-scan", true, "native Optimization-1 backward scan")
	cacheBytes := flag.Int("cache-bytes", 64<<20, "bounded LRU bytes")
	cacheEntries := flag.Int("cache-entries", 4096, "bounded LRU entries")
	pageBytes := flag.Int("page-bytes", 64<<10, "persistent page limit")
	bitmapBytes := flag.Int("bitmap-bytes", 32<<20, "single bitmap workspace limit")
	objectBytes := flag.Int("object-bytes", 4<<20, "object/metadata workspace limit")
	queryBytes := flag.Int("query-bytes", 256<<20, "single native query workspace limit")
	audit := contract.BindAuditFlags(flag.CommandLine)
	flag.Parse()
	execution := audit.Execution()
	if err := execution.SetRuntime(); err != nil {
		return err
	}
	if err := contract.LoadEnv(*env); err != nil {
		return err
	}
	request, err := contract.ReadAuditRequest(*audit.Request, audit.Limits())
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx = contract.WithAuditExecution(ctx, execution)
	ctx = audit.WithProgress(ctx)
	ctx = audit.WithAuthCount(ctx)
	ctx, err = audit.WithReads(ctx)
	if err != nil {
		return err
	}
	db, err := contract.OpenEnvPostgres(ctx, "BASELINES_POSTGRES", "", execution.SQLPoolSize)
	if err != nil {
		return err
	}
	defer db.Close()
	a := durable.Adapter{DB: db, Options: durable.Options{RunID: *runID, EntriesPerFile: *entries, FilesPerBlock: *files, Fanout: *fanout, SegmentSize: *segment, Aggregation: *aggregate, Index: diskindex.Limits{PageBytes: *pageBytes, CacheBytes: *cacheBytes, CacheEntries: *cacheEntries, BitmapBytes: *bitmapBytes}, ObjectBytes: *objectBytes, QueryBytes: *queryBytes}}
	a.Options.GeneratorMode = *generatorMode
	if err = a.Options.Validate(); err != nil {
		return err
	}
	c := contract.Consumer{Store: contract.Store{DB: db}, ID: "blas/" + *runID, StreamID: request.StreamID, Fingerprint: request.Fingerprint, ConfigHash: a.Options.ConfigHash()}
	cache, err := diskindex.NewCache(a.Options.Index)
	if err != nil {
		return err
	}
	ctx = contract.WithAuditCacheReset(ctx, func(context.Context) error { cache.Clear(); return nil })
	report, err := contract.RunAudits(ctx, c, request, audit.Limits(), "BLAS", *audit.Warmups, *audit.Runs, *audit.Interval, func(ctx context.Context, r contract.AuditRequest) (contract.AuditMetrics, error) {
		return a.Audit(ctx, c, r, cache, *scan)
	})
	if err != nil {
		return err
	}
	report.Settings, _ = json.Marshal(a.Options)
	reads := contract.AuditReadsFrom(ctx)
	report.ReadPolicy = &reads
	report.Notes = append(report.Notes, "The fixed logical observations are resolved through native attribute/time queries; all legitimate extra matches are proved and counted.", "QueryResolution includes stage-1/2 processing and overlaps proof construction/retrieval; stage times must not be added to derive end-to-end.", "Independent native point queries use bounded workers, private readers/verifiers and one shared bounded page cache; historical structures are never preloaded.")
	return audit.Finish(report)
}
