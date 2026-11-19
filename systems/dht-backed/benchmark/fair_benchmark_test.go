package benchmark

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"

	"dhtbacked/internal/cassandradht"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

var fairDHTResult verifier.Result
var fairDHTFixtureAttempted bool
var fairDHTFixture *fairDHTPersistentFixture
var fairDHTFixtureErr error

// BenchmarkFairDHTVerificationRequest uses the same testing.B timing
// machinery as every other fair series. A timed operation starts with the
// request, reads the requested records from PostgreSQL in one query, reads
// every proof record the batch needs from the Cassandra-backed DHT with a
// bounded pool of concurrent lookups, and ends after trusted-anchor
// verification. Each lookup pays ceil(log2 peers) routing reads (10 at the
// default 1,024 peers) before its value read.
func BenchmarkFairDHTVerificationRequest(b *testing.B) {
	b.StopTimer()
	if os.Getenv("FAIR_PERSISTENCE") != "1" {
		b.Fatal("FAIR_PERSISTENCE=1 is required; use the shared runner")
	}
	segmentLeaves := fairDHTSegmentLeaves(b)
	fixture := fairDHTFixtureForBenchmark(b, segmentLeaves)

	ctx := context.Background()
	store, err := storage.Open(ctx, os.Getenv("FAIR_DHT_POSTGRES_DSN"))
	if err != nil {
		b.Fatalf("open PostgreSQL store: %v", err)
	}
	b.Cleanup(func() { _ = store.Close(context.Background()) })
	proofStore := openFairDHTCassandra(b)
	b.Cleanup(proofStore.Close)

	proofs, err := cassandradht.New(ctx, proofStore, fixture.ca.PublicKey(), cassandradht.Config{
		Peers: fairDHTPeers(b), Concurrency: fairDHTConcurrency(b),
	})
	if err != nil {
		b.Fatalf("open Cassandra DHT: %v", err)
	}
	persistentVerifier := verifier.New(store, proofs, fixture.chain, fixture.config)

	// Fixture construction deliberately leaves no dense tree or record cache,
	// and a full collection releases transient encryption/COPY allocations
	// before any validation, warm-up, or measured operation starts.
	runtime.GC()
	debug.FreeOSMemory()

	runCase := func(name string, batchSize int, ids []string) {
		b.Run(name, func(b *testing.B) {
			b.StopTimer()
			// A complete benchmark suite can exceed the token's fixed one-hour
			// validity. Issue one for each benchmark execution so setup,
			// calibration, and earlier cases cannot age it out. Issuance remains
			// outside the measured request interval.
			token := fixture.ca.Issue("fair-auditor", fairDHTEpoch, fixture.config.LogTypes)
			runRequest := func() (verifier.Result, error) {
				return persistentVerifier.VerifyBatch(ctx, &token, ids)
			}
			proofs.ResetStats()
			validation, err := runRequest()
			if err != nil || !validation.Accepted {
				b.Fatalf("validation accepted=%v err=%v", validation.Accepted, err)
			}
			reads := proofs.Stats()
			warmup := fairDHTWarmupCount(b)
			for range warmup {
				result, err := runRequest()
				if err != nil || !result.Accepted {
					b.Fatalf("warm-up accepted=%v err=%v", result.Accepted, err)
				}
				fairDHTResult = result
			}
			b.ResetTimer()
			b.ReportMetric(float64(batchSize), "records/op")
			if os.Getenv("FAIR_QUERY_DIR") != "" {
				b.ReportMetric(9, "workload-version/op")
			} else if os.Getenv("FAIR_REGION0_RECORDS") != "" {
				b.ReportMetric(8, "workload-version/op")
			}
			b.ReportMetric(float64(validation.HashCount), "hashes/op")
			b.ReportMetric(float64(validation.Ancestors), "ancestors/op")
			b.ReportMetric(float64(reads.Lookups), "cassandra-dht-lookups/op")
			b.ReportMetric(float64(reads.RoutingReads), "dht-routing-reads/op")
			b.ReportMetric(float64(proofs.HopsPerLookup()), "dht-hops-per-lookup/op")
			b.ReportMetric(float64(proofs.Peers()), "dht-peers/op")
			b.ReportMetric(float64(proofs.Concurrency()), "dht-concurrency/op")
			b.ReportMetric(1, "postgres-batches/op")
			b.ReportMetric(1, "proof-generation/op")
			b.ReportMetric(float64(warmup), "warmup-ops")
			b.StartTimer()
			for range b.N {
				result, err := runRequest()
				if err != nil || !result.Accepted {
					b.Fatalf("request accepted=%v err=%v", result.Accepted, err)
				}
				fairDHTResult = result
			}
		})
	}
	// Native query sets: with FAIR_QUERY_DIR set, run the five clustered and five
	// scattered record sets per q from the query file instead of the fixed placements.
	if dir := os.Getenv("FAIR_QUERY_DIR"); dir != "" {
		maxQ, err := fairDHTMaxQ(os.Getenv("FAIR_DHT_MAX_Q"))
		if err != nil {
			b.Fatal(err)
		}
		cases, err := fairDHTNativeCases(dir, maxQ)
		if err != nil {
			b.Fatal(err)
		}
		for _, c := range cases {
			for _, set := range c.Sets {
				runCase(fmt.Sprintf("%s/q=%d/set=%d", c.Shape, c.Q, set.Set), c.Q, set.IDs)
			}
		}
		return
	}
	for _, placement := range fairDHTPlacements {
		for _, batchSize := range fairDHTRequestCounts {
			ids := fixture.batches[fairDHTCase{placement: placement, count: batchSize}]
			runCase(fmt.Sprintf("%s/q=%d", placement, batchSize), batchSize, ids)
		}
	}
}

func openFairDHTCassandra(t testing.TB) *cassandradht.Cassandra {
	t.Helper()
	port, err := strconv.Atoi(os.Getenv("FAIR_CASSANDRA_PORT"))
	if err != nil {
		t.Fatalf("invalid FAIR_CASSANDRA_PORT %q", os.Getenv("FAIR_CASSANDRA_PORT"))
	}
	store, err := cassandradht.Open(os.Getenv("FAIR_CASSANDRA_HOST"), port)
	if err != nil {
		t.Fatalf("open Cassandra DHT store: %v", err)
	}
	return store
}

func fairDHTFixtureForBenchmark(t testing.TB, segmentLeaves int) *fairDHTPersistentFixture {
	t.Helper()
	datasetPath := os.Getenv("FAIR_DATASET_PATH")
	if datasetPath == "" {
		t.Fatal("FAIR_DATASET_PATH is unset; run the shared benchmark runner")
	}
	if !fairDHTFixtureAttempted {
		fairDHTFixtureAttempted = true
		ctx := context.Background()
		store, err := storage.Open(ctx, os.Getenv("FAIR_DHT_POSTGRES_DSN"))
		if err != nil {
			fairDHTFixtureErr = fmt.Errorf("open PostgreSQL fixture store: %w", err)
		} else {
			defer store.Close(context.Background())
			proofStore := openFairDHTCassandra(t)
			defer proofStore.Close()
			fairDHTFixture, fairDHTFixtureErr = buildFairDHTPersistentFixture(
				ctx, datasetPath, segmentLeaves, t.TempDir(), store, proofStore, fairDHTPeers(t),
			)
		}
	}
	if fairDHTFixtureErr != nil {
		t.Fatalf("prepare persistent DHT fixture: %v", fairDHTFixtureErr)
	}
	return fairDHTFixture
}

func fairDHTWarmupCount(t testing.TB) int {
	t.Helper()
	value := os.Getenv("FAIR_WARMUP")
	if value == "" {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		t.Fatalf("invalid FAIR_WARMUP %q", value)
	}
	return count
}

func fairDHTPositiveSetting(t testing.TB, name string, fallback int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		t.Fatalf("invalid %s %q", name, value)
	}
	return parsed
}

// fairDHTConcurrency is the bound on concurrent DHT lookups per request. It
// defaults to 8, the bound HMF-Audit and EVSEB use for their own lookups.
func fairDHTConcurrency(t testing.TB) int {
	return fairDHTPositiveSetting(t, "FAIR_DHT_CONCURRENCY", 8)
}

// fairDHTPeers is n, the number of peers in the modelled DHT; a lookup costs
// ceil(log2 n) routing hops, each a real Cassandra read. It defaults to 16
// logical nodes (4 hops), the same overlay EVSEB's Cassandra DHT models, and
// nothing is simulated. FAIR_DHT_PEERS=1 turns routing off.
func fairDHTPeers(t testing.TB) int {
	return fairDHTPositiveSetting(t, "FAIR_DHT_PEERS", 16)
}

func fairDHTSegmentLeaves(t testing.TB) int {
	t.Helper()
	value := os.Getenv("FAIR_HMF_SEGMENT_LEAVES")
	if value == "" {
		return 1 << 13
	}
	segmentLeaves, err := strconv.Atoi(value)
	if err != nil || segmentLeaves <= 0 {
		t.Fatalf("invalid FAIR_HMF_SEGMENT_LEAVES %q", value)
	}
	return segmentLeaves
}

func selectFairDHTBatch(pool []string, segmentLeaves int, placement string, count int) []string {
	positions, err := fairDHTBatchPositions(len(pool), segmentLeaves, placement, count)
	if err != nil {
		panic(err)
	}
	result := make([]string, len(positions))
	for index, position := range positions {
		result[index] = pool[position]
	}
	return result
}

func TestSelectFairDHTBatchUsesCommonRecordOrdinals(t *testing.T) {
	pool := make([]string, 33)
	for index := range pool {
		pool[index] = fmt.Sprintf("%d", index)
	}
	for _, test := range []struct {
		placement string
		last      string
	}{
		{"clustered-2", "15"},
		{"clustered-3", "23"},
		{"clustered-4", "31"},
	} {
		got := selectFairDHTBatch(pool, 8, test.placement, 16)
		if got[0] != "0" || got[len(got)-1] != test.last {
			t.Fatalf("%s = %v", test.placement, got)
		}
		if single := selectFairDHTBatch(pool, 8, test.placement, 1); len(single) != 1 || single[0] != "0" {
			t.Fatalf("%s q=1 = %v", test.placement, single)
		}
	}
	if got := selectFairDHTBatch(pool, 8, "scattered", 3); strings.Join(got, ",") != "0,16,32" {
		t.Fatalf("scattered = %v", got)
	}
}
