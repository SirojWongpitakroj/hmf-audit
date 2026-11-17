package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

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
	execution := contract.DefaultExecution()
	execution.Flags(flag.CommandLine)
	follow := flag.Bool("follow", false, "consume live durable prefixes while MVA continues ingestion")
	managed := flag.Bool("managed", false, "stop when supervisor stdin closes")
	poll := flag.Duration("poll-interval", 500*time.Millisecond, "published prefix polling interval")
	envFile := flag.String("env-file", "", "optional local deployment environment profile")
	mode := flag.String("mode", "", "one cumulative target: 1M, 10M, 50M, 100M, 250M")
	runID := flag.String("run", "", "persistent BLAS run identity")
	stream := flag.String("stream", "canonical-v1", "published canonical stream ID")
	entries := flag.Int("entries-per-file", 4096, "native entries per immutable file")
	files := flag.Int("files-per-block", 256, "native files per immutable block")
	fanout := flag.Int("fanout", 4, "native tree fanout")
	segment := flag.Uint64("segment-size", 1024, "native Optimization-1 segment size; not a RAM limit")
	aggregate := flag.Bool("aggregation", false, "construct native Optimization-2 commitments")
	generatorMode := flag.String("generator-mode", "device-v1", "device-v1 requires a BLAS-only rebuild; empty selects legacy single-generator state")
	maxGenerators := flag.Int("max-generators", 512, "maximum durable active generator cursors; limit never causes file closure")
	compactZeros := flag.Bool("compact-zero-bitmaps", true, "reconstruct explicit zero bitmap markers; logical leaves and hashes retained")
	lowRisk := flag.Bool("low-risk-optimizations", true, "bounded transaction-object batching, current-file input reuse and immutable encoding reuse")
	segmentReuse := flag.Bool("segment-computation-reuse", true, "reuse exact hash inputs within the active native segment; persistence unchanged")
	keywordPrefetch := flag.Bool("keyword-prefetch", true, "look up a batch's generator keywords in two queries; false restores per-keyword lookups (stored state is identical)")
	groupIndexWrites := flag.Bool("group-index-writes", true, "combine compatible index trees/files in existing bounded batches inside one SQL transaction")
	batchRecords := flag.Int("batch-records", 1000, "canonical transaction row limit")
	batchBytes := flag.Int("batch-bytes", 8<<20, "canonical transaction byte limit")
	objectBytes := flag.Int("object-bytes", 4<<20, "active object/metadata byte limit")
	bitmapBytes := flag.Int("bitmap-bytes", 32<<20, "single active bitmap byte limit")
	queryBytes := flag.Int("query-bytes", 256<<20, "per-query byte budget")
	pageBytes := flag.Int("page-bytes", 64<<10, "persistent page byte limit")
	cacheBytes := flag.Int("cache-bytes", 64<<20, "maximum query cache bytes including entry accounting")
	cacheEntries := flag.Int("cache-entries", 4096, "maximum query cache entries; zero disables")
	flag.Parse()
	if err := execution.ConfigureRuntime(); err != nil {
		return err
	}
	if err := contract.LoadEnv(*envFile); err != nil {
		return err
	}
	if _, err := contract.Target(*mode); err != nil {
		return err
	}
	ctx, cancel := contract.ManagedContext(*managed)
	defer cancel()
	metrics := &contract.IngestMetrics{}
	ctx = contract.WithIngestMetrics(ctx, metrics)
	sourceDB, db, closeDatabases, err := contract.OpenIngestionDatabases(ctx, execution.SQLPoolSize)
	if err != nil {
		return fmt.Errorf("canonical stream connection: %w", err)
	}
	defer closeDatabases()
	stopMetrics := metrics.Reporter(ctx, os.Stderr, execution, map[string]*sql.DB{"source": sourceDB, "native": db})
	defer stopMetrics()
	source := contract.Store{DB: sourceDB}
	if err = source.Migrate(ctx); err != nil {
		return err
	}
	var manifest contract.Manifest
	if *follow {
		manifest, err = source.WaitManifest(ctx, *stream, *poll)
	} else {
		manifest, err = source.Manifest(ctx, *stream)
	}
	if err != nil {
		return err
	}
	a := durable.Adapter{DB: db, Options: durable.Options{RunID: *runID, EntriesPerFile: *entries, FilesPerBlock: *files, Fanout: *fanout, SegmentSize: *segment, Aggregation: *aggregate, Index: diskindex.Limits{PageBytes: *pageBytes, CacheBytes: *cacheBytes, CacheEntries: *cacheEntries, BitmapBytes: *bitmapBytes}, ObjectBytes: *objectBytes, QueryBytes: *queryBytes}}
	store := contract.Store{DB: db}
	a.Options.GeneratorMode, a.Options.MaxGenerators = *generatorMode, *maxGenerators
	a.CompactZeroBitmaps = *compactZeros
	a.Execution, a.Metrics = &execution, metrics
	a.DisableLowRiskOptimizations = !*lowRisk
	a.DisableCompatibleIndexBatching = !*groupIndexWrites
	a.DisableKeywordPrefetch = !*keywordPrefetch
	if *segmentReuse {
		a.SegmentComputations = diskindex.NewComputationCache(execution.CacheMaxEntries, execution.CacheMaxBytes)
	}
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	c := contract.Consumer{Store: store, ID: "blas/" + *runID, StreamID: *stream, Fingerprint: manifest.Fingerprint, ConfigHash: a.Options.ConfigHash()}
	c.Execution, c.Metrics = &execution, metrics
	lease, err := c.Acquire(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	if err = a.Migrate(ctx); err != nil {
		return err
	}
	if err = c.Open(ctx); err != nil {
		return err
	}
	var p contract.Progress
	if *follow {
		target, _ := contract.Target(*mode)
		p, err = c.FollowMilestones(ctx, source, target, contract.Limits{MaxRecords: *batchRecords, MaxBatchBytes: *batchBytes, MaxRecordBytes: *objectBytes}, a.Apply, *poll, nil, nil)
	} else {
		p, err = c.Run(ctx, source, *mode, contract.Limits{MaxRecords: *batchRecords, MaxBatchBytes: *batchBytes, MaxRecordBytes: *objectBytes}, a.Apply)
	}
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		Consumer          string
		Committed, Target uint64
		Stopped           bool
	}{c.ID, p.Committed, p.Target, true})
}
