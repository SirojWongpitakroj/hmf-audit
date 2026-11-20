package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"blas/alis"
	"blas/chain"
	"blas/iomf"
	"blas/query"
	"blas/verify"
	"github.com/jackc/pgx/v5"
)

// fairBLASConcurrency is the number of concurrent PostgreSQL readers for the off-chain
// files; HMF-Audit, DHT-backed and EVSEB also run their request I/O 8 wide.
const fairBLASConcurrency = 8

type fairBLASScenario struct {
	placement string
	batchSize int
	request   query.Q
	// set and expectedTS are used by native query sets (FAIR_NATIVE_QUERIES_DIR): the set
	// number and the timestamps of the records the query must return.
	set        int
	expectedTS []int64
}

// name is the sub-benchmark name: placement/q=N, plus /set=K for native query sets.
func (scenario fairBLASScenario) name() string {
	if scenario.set > 0 {
		return fmt.Sprintf("%s/q=%d/set=%d", scenario.placement, scenario.batchSize, scenario.set)
	}
	return fmt.Sprintf("%s/q=%d", scenario.placement, scenario.batchSize)
}

type fairBLASRequestResult struct {
	result        verify.Result
	response      verify.Response
	postgresReads int
	loadedBytes   int
	chainReads    int64 // PostgreSQL round trips of the full node's Stage 1/2 (zero for an in-memory chain)
	chainBytes    int64 // payload bytes it read
}

// BenchmarkFairBLASVerificationRequest measures the complete request after it
// arrives: full-node Stage 1-2 proof generation over the on-chain indexes,
// SP retrieval of the off-chain log files from PostgreSQL with Stage 3 proof
// generation, and final auditor verification of all three stages.
func BenchmarkFairBLASVerificationRequest(b *testing.B) {
	b.StopTimer()
	ctx := context.Background()
	state := sharedFairBLAS(b)
	fullNode, registry, auditor := state.fullNode, state.registry, state.auditor
	options := verify.Options{UseSegmentScan: true, UseAggregation: true}

	scenarios := make([]fairBLASScenario, 0, 2*len(fairBLASBatchSizes))
	native := fairNativeQueriesDir() != ""
	if native {
		var err error
		if scenarios, err = fairBLASNativeScenarios(fairNativeQueriesDir(), state.vocabulary); err != nil {
			b.Fatal(err)
		}
	}
	for _, placement := range fairBLASPlacements {
		if native {
			break
		}
		for _, batchSize := range fairBLASBatchSizes {
			expression := iomf.Keyword(fairBLASBatchKeyword(placement, batchSize))
			scenarios = append(scenarios, fairBLASScenario{
				placement: placement,
				batchSize: batchSize,
				request:   query.Q{LG: 0, FK: expression, EK: expression},
			})
		}
	}

	for _, scenario := range scenarios {
		scenario := scenario
		b.Run(scenario.name(), func(b *testing.B) {
			b.StopTimer()
			probe, err := executeFairBLASRequest(ctx, state.workers, fullNode, registry, auditor, scenario, options)
			if err != nil {
				b.Fatal(err)
			}
			validateFairBLASRequestResult(b, scenario, probe)

			warmup := fairBLASWarmupCount(b)
			for range warmup {
				outcome, err := executeFairBLASRequest(ctx, state.workers, fullNode, registry, auditor, scenario, options)
				if err != nil {
					b.Fatal(err)
				}
				validateFairBLASRequestResult(b, scenario, outcome)
				fairBLASResult = outcome.result
			}

			b.ResetTimer()
			b.ReportMetric(float64(scenario.batchSize), "records/op")
			if native {
				b.ReportMetric(fairNativeWorkloadVersion, "workload-version/op")
			} else if os.Getenv("FAIR_REGION0_RECORDS") != "" {
				b.ReportMetric(8, "workload-version/op")
			}
			b.ReportMetric(float64(state.datasetRecords), "dataset-records")
			b.ReportMetric(float64(state.blocks), "blocks")
			b.ReportMetric(float64(len(probe.response.Files)), "response-files/op")
			b.ReportMetric(float64(VOSize(
				probe.response.Stage1, probe.response.Stage2,
				probe.response.Files, probe.response.Bindings,
			)), "evidence-bytes/op")
			b.ReportMetric(float64(probe.loadedBytes), "cloud-bytes/op")
			b.ReportMetric(float64(state.cloudFiles), "persisted-cloud-files")
			b.ReportMetric(float64(state.cloudBytes), "persisted-cloud-bytes")
			b.ReportMetric(float64(state.chainStoredBytes), "stored-chain-bytes")
			b.ReportMetric(float64(probe.postgresReads), "postgres-reads/op")
			b.ReportMetric(float64(probe.chainReads), "chain-reads/op")
			b.ReportMetric(float64(probe.chainBytes), "chain-bytes/op")
			b.ReportMetric(1, "proof-generation/op")
			b.ReportMetric(float64(warmup), "warmup-ops")
			b.StartTimer()
			for range b.N {
				outcome, err := executeFairBLASRequest(ctx, state.workers, fullNode, registry, auditor, scenario, options)
				if err != nil {
					b.Fatal(err)
				}
				validateFairBLASRequestResult(b, scenario, outcome)
				fairBLASResult = outcome.result
			}
		})
	}
}

func validateFairBLASRequestResult(b testing.TB, scenario fairBLASScenario,
	outcome fairBLASRequestResult) {
	b.Helper()
	if !outcome.result.Accept || len(outcome.result.Entries) != scenario.batchSize {
		b.Fatalf("BLAS %s: accepted=%v entries=%d, want %d",
			scenario.name(), outcome.result.Accept, len(outcome.result.Entries), scenario.batchSize)
	}
	if scenario.expectedTS != nil {
		got := make([]int64, len(outcome.result.Entries))
		for index, entry := range outcome.result.Entries {
			got[index] = entry.TS
		}
		if err := fairBLASSameTimestamps(got, scenario.expectedTS); err != nil {
			b.Fatalf("BLAS %s: %v", scenario.name(), err)
		}
	}
}

func executeFairBLASRequest(ctx context.Context, workers []*pgx.Conn,
	fullNode *query.FullNode, registry map[uint64][]string, auditor *verify.Auditor,
	scenario fairBLASScenario, options verify.Options) (fairBLASRequestResult, error) {
	provider := query.NewConcurrentPostgresSP(ctx, workers)
	fullNode.BeginRequest() // a storage-backed full node starts every request without anything cached
	response, ok := auditor.PrepareResponse(
		fullNode, provider, registry, scenario.request, options,
	)
	if !ok {
		return fairBLASRequestResult{}, fmt.Errorf(
			"BLAS proof generation failed for %s q=%d",
			scenario.placement, scenario.batchSize,
		)
	}
	reads, loadedBytes := provider.Stats()
	chainReads, chainBytes := fullNode.ChainStats()
	return fairBLASRequestResult{
		chainReads:    chainReads,
		chainBytes:    chainBytes,
		result:        auditor.VerifyResponse(registry, scenario.request, response, options),
		response:      response,
		postgresReads: reads,
		loadedBytes:   loadedBytes,
	}, nil
}

func encodeFairBLASValue(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := gob.NewEncoder(&output).Encode(value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

// fairBLASState is the persisted BLAS fixture. BLAS Sec. III: raw logs and
// the entry-level index are off-chain with the SP, so they are read from
// PostgreSQL on every request. The block- and transaction-level indexes are
// on-chain and stay with the full node, as every system's blockchain state
// stays outside the persistence layer. The registry is the on-chain keyword
// set file (the build keeps it beside the chain rather than registering it on
// the Chain value).
type fairBLASState struct {
	fileRoots      map[query.FileRef]alis.Transaction
	database       *pgx.Conn
	workers        []*pgx.Conn // fairBLASConcurrency readers; workers[0] is database
	fullNode       *query.FullNode
	auditor        *verify.Auditor
	registry       map[uint64][]string
	vocabulary     []string
	blocks         int
	datasetRecords int
	cloudFiles     int
	cloudBytes     int

	chainStoredBytes int64 // payload bytes of the stored segments and blocks (zero with the in-memory chain)
}

// TestMain closes the shared fixture's connection once every benchmark in the
// process has finished. A per-benchmark cleanup cannot do it: the fixture
// outlives the benchmark that happened to seed it.
func TestMain(m *testing.M) {
	code := m.Run()
	if state := fairBLASShared.state; state != nil {
		for _, extra := range state.workers[1:] {
			_ = extra.Close(context.Background())
		}
		_ = state.database.Close(context.Background())
	}
	os.Exit(code)
}

// fairBLASShared holds that fixture for the whole test process: building the
// chain and writing the entry files is setup, not part of any measured
// request, so it runs once and is shared by every -count repetition.
var fairBLASShared struct {
	once  sync.Once
	state *fairBLASState
	err   error
}

func sharedFairBLAS(b *testing.B) *fairBLASState {
	b.Helper()
	if os.Getenv("FAIR_PERSISTENCE") != "1" {
		b.Fatal("FAIR_PERSISTENCE=1 is required; use the shared runner")
	}
	fairBLASShared.once.Do(func() {
		fairBLASShared.state, fairBLASShared.err = seedFairBLAS(context.Background())
	})
	if fairBLASShared.err != nil {
		b.Fatal(fairBLASShared.err)
	}
	return fairBLASShared.state
}

func seedFairBLAS(ctx context.Context) (*fairBLASState, error) {
	return loadFairBLAS(ctx, true)
}

func loadFairBLAS(ctx context.Context, persist bool) (*fairBLASState, error) {
	// Setup only: seeding is not timed, so it may use every core. The timed
	// requests run again under the process's GOMAXPROCS (1).
	defer raiseFairSetupProcs()()
	connection, err := pgx.ParseConfig(os.Getenv("FAIR_POSTGRES_DSN"))
	if err != nil {
		return nil, fmt.Errorf("invalid BLAS PostgreSQL configuration")
	}
	if !persist {
		connection.RuntimeParams["default_transaction_read_only"] = "on"
	}
	database, err := pgx.ConnectConfig(ctx, connection)
	if err != nil {
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	cfg := Config{
		NumGenerators: 1, EntriesPerFile: 128, FilesPerBlock: 8,
		Fanout: 4, SegmentSize: 8, Seed: 1, UseAggregation: true,
	}
	vocabulary, total, err := fairBLASVocabulary()
	if err != nil {
		return nil, err
	}
	writer := database
	if !persist {
		writer = nil
	}
	var batchKeywords map[int][]string
	if persist && fairNativeQueriesDir() == "" {
		batchKeywords = fairBLASBatchKeywords(total)
	}
	if !persist {
		batchKeywords, err = cloudBLASBatchKeywords(ctx, database, cfg, vocabulary, total)
		if err != nil {
			_ = database.Close(ctx)
			return nil, err
		}
	}
	build, err := buildAndPersistFairBLAS(ctx, writer, cfg, vocabulary, total, batchKeywords)
	if err != nil {
		return nil, fmt.Errorf("build BLAS reference chain: %w", err)
	}
	headers := make([]chain.BlockHeader, len(build.chain.Blocks()))
	var fileRoots map[query.FileRef]alis.Transaction
	if !persist {
		fileRoots = make(map[query.FileRef]alis.Transaction, build.files)
	}
	for index, block := range build.chain.Blocks() {
		headers[index] = block.Header
		if !persist {
			for _, transaction := range block.Transactions {
				fileRoots[query.FileRef{LID: transaction.LID, FID: transaction.FID}] = transaction
			}
		}
	}
	if _, ok := build.registry[0]; !ok {
		return nil, fmt.Errorf("shared dataset did not produce BLAS log generator 0")
	}
	// The full node's block-level and transaction-level indexes live in PostgreSQL, not in the process:
	// at the scale the paper targets they do not fit in memory, and a request reads only the segments and
	// blocks it needs. Saving them is setup. FAIR_BLAS_CHAIN_IN_MEMORY=1 keeps the in-memory chain instead.
	// Only the seeding run stores the chain; the read-only run (persist == false, the cloud demo) reuses
	// the reference chain it just rebuilt.
	storeChain := persist && os.Getenv("FAIR_BLAS_CHAIN_IN_MEMORY") != "1"
	var chainBytes int64
	if storeChain {
		started := time.Now()
		chainBytes, err = query.SaveChain(ctx, database, build.chain)
		fmt.Fprintf(os.Stderr, "fixture-timing: BLAS: store the block- and transaction-level indexes in PostgreSQL: %s\n",
			time.Since(started).Round(time.Millisecond))
		if err != nil {
			_ = database.Close(ctx)
			return nil, fmt.Errorf("store BLAS chain: %w", err)
		}
	}
	// Same request concurrency as HMF-Audit, DHT-backed and EVSEB: 8 readers, opened once
	// here so connection setup is not part of any measured request.
	workers, err := query.OpenWorkerConns(ctx, database, fairBLASConcurrency)
	if err != nil {
		return nil, fmt.Errorf("open BLAS reader connections: %w", err)
	}
	fullNode := query.NewFullNode(build.chain)
	if storeChain {
		view, err := query.OpenPostgresChain(ctx, workers)
		if err != nil {
			return nil, fmt.Errorf("open stored BLAS chain: %w", err)
		}
		fullNode = query.NewFullNodeFromView(view)
		build.chain = nil // from here on only the database holds the indexes
	}
	runtime.GC()
	return &fairBLASState{
		chainStoredBytes: chainBytes,
		fileRoots:        fileRoots, database: database, workers: workers, fullNode: fullNode,
		auditor: verify.NewAuditor(headers, cfg.SegmentSize), registry: build.registry,
		vocabulary: vocabulary,
		blocks:     len(headers), datasetRecords: total,
		cloudFiles: build.files, cloudBytes: build.bytes,
	}, nil
}

type fairBLASBuild struct {
	chain    *chain.Chain
	registry map[uint64][]string
	files    int
	bytes    int
}

// buildAndPersistFairBLAS streams the shared dataset into BLAS: entries are
// packed into ALIS entry files, each file is COPYed into PostgreSQL as it is
// closed and then dropped, and its metadata goes on-chain a block at a time.
// Setup therefore holds the chain, not the dataset or the entry files.
// A nil database rebuilds only the reference chain for read-only verification.
func buildAndPersistFairBLAS(ctx context.Context, database *pgx.Conn, cfg Config,
	vocabulary []string, total int, batchKeywords map[int][]string) (*fairBLASBuild, error) {
	file, err := os.Open(os.Getenv("FAIR_DATASET_PATH"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)

	logChain, err := chain.NewChainWithOptions(cfg.Fanout, cfg.SegmentSize, cfg.UseAggregation)
	if err != nil {
		return nil, err
	}
	build := &fairBLASBuild{chain: logChain, registry: map[uint64][]string{}}
	generator := alis.NewLogGenerator(0)
	var pending []alis.FileMetadata
	var fid uint64
	ordinal, region := 0, -1

	// addScannedEntry adds the line the scanner currently holds.
	addScannedEntry := func(builder *alis.EntryFileBuilder) error {
		var input fairBLASDatasetRecord
		if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
			return fmt.Errorf("decode shared dataset record %d: %w", ordinal, err)
		}
		if err := validateFairBLASRecord(input, ordinal, &region); err != nil {
			return err
		}
		keywords := append(fairBLASKeywords(vocabulary, input.RawLog), batchKeywords[ordinal]...)
		if _, err := builder.AddEntry(fairBLASTimestamp(input.RawLog, ordinal),
			keywords, []byte(input.RawLog)); err != nil {
			return err
		}
		ordinal++
		return nil
	}
	// nextFile builds and encodes one entry file, or returns a nil row when
	// the dataset is exhausted.
	nextFile := func() ([]any, error) {
		if !scanner.Scan() {
			return nil, scanner.Err()
		}
		builder, err := alis.NewEntryFileBuilder(generator, fid, cfg.Fanout)
		if err != nil {
			return nil, err
		}
		for entries := 0; ; {
			if err := addScannedEntry(builder); err != nil {
				return nil, err
			}
			if entries++; entries == cfg.EntriesPerFile || !scanner.Scan() {
				break
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
		entryFile, meta, err := builder.Close()
		if err != nil {
			return nil, err
		}
		var payload []byte
		if database != nil {
			payload, err = encodeFairBLASValue(entryFile)
			if err != nil {
				return nil, fmt.Errorf("encode file %d: %w", fid, err)
			}
		}
		pending = append(pending, meta)
		if len(pending) == cfg.FilesPerBlock {
			if _, err := logChain.AddBlock(pending); err != nil {
				return nil, err
			}
			pending = nil
		}
		build.files++
		build.bytes += len(payload)
		row := []any{int64(0), int64(fid), payload}
		fid++
		return row, nil
	}

	if database == nil {
		for {
			row, err := nextFile()
			if err != nil {
				return nil, err
			}
			if row == nil {
				break
			}
		}
	} else {
		if _, err := database.Exec(ctx, "TRUNCATE TABLE blas.entry_files"); err != nil {
			return nil, err
		}
		inserted, err := database.CopyFrom(ctx, pgx.Identifier{"blas", "entry_files"},
			[]string{"lid", "fid", "payload"}, pgx.CopyFromFunc(nextFile))
		if err != nil {
			return nil, err
		}
		if inserted != int64(build.files) {
			return nil, fmt.Errorf("persisted %d BLAS files, want %d", inserted, build.files)
		}
	}
	if len(pending) > 0 {
		if _, err := logChain.AddBlock(pending); err != nil {
			return nil, err
		}
	}
	if ordinal != total {
		return nil, fmt.Errorf("streamed %d BLAS records, want %d", ordinal, total)
	}
	build.registry[0] = generator.KeywordUniverse()
	return build, nil
}
