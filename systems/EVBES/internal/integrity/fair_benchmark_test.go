package integrity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"evseb/internal/blockchain"
	"evseb/internal/crypto"
	"evseb/internal/logstore"
	"github.com/jackc/pgx/v5"
)

type fairEVBESDatasetRecord struct {
	RawLog   string `json:"raw_log"`
	RegionID string `json:"region_id"`
	SourceID string `json:"source_id"`
	TenantID string `json:"tenant_id"`
}

var (
	fairEVBESVerified []string
	fairEVBESOK       bool
)

var fairEVBESPlacements = []string{"clustered-2", "scattered"}
var fairEVBESRequestCounts = []int{1, 16, 64, 256, 1024, 4096}

// fairEVBESDefaultDHTPeers is the modelled overlay size, the same 16 logical
// nodes (4 routing hops per lookup) the DHT-backed system uses. FAIR_DHT_PEERS
// overrides it for both systems; 1 turns routing off.
const fairEVBESDefaultDHTPeers = 16

var fairEVBESTamperCounts = []int{1, 2, 4, 8, 16, 32, 64, 128, 256}

const fairEVBESFixedBatchSize = 1 << 12

func BenchmarkFairEVBESDetectAndLocalizeCore(b *testing.B) {
	runFairEVBES(b, true)
}

func BenchmarkFairEVBESLocalizationAfterMismatch(b *testing.B) {
	runFairEVBES(b, true)
}

// BenchmarkFairEVBESTamperCountLocalization holds the requested batch at
// 2^12 and varies the number of persistently tampered requested logs.
func BenchmarkFairEVBESTamperCountLocalization(b *testing.B) {
	b.StopTimer()
	ctx := context.Background()
	database, dht, setup := sharedFairEVBES(b)
	segmentLeaves := fairEVBESSegmentLeaves(b)
	for _, placement := range fairEVBESPlacements {
		candidates := fairEVBESCandidates(
			setup.leafCount, segmentLeaves, placement, fairEVBESFixedBatchSize,
		)
		for _, tamperCount := range fairEVBESTamperCounts {
			b.Run(fmt.Sprintf("%s/q=%d/tampered=%d", placement,
				fairEVBESFixedBatchSize, tamperCount), func(b *testing.B) {
				b.StopTimer()
				tampered, err := fairEVBESTamperedCandidates(candidates, tamperCount)
				if err != nil {
					b.Fatal(err)
				}
				if err := replaceFairEVBESTampers(ctx, database, tampered); err != nil {
					b.Fatal(err)
				}
				runFairEVBESCase(b, ctx, database, dht, setup, candidates, tampered, 8)
			})
		}
	}
	if err := clearFairEVBESTamper(ctx, database); err != nil {
		b.Fatal(err)
	}
}

// BenchmarkFairEVBESVerificationRequest times the same native request over
// honest storage: the recomputed root matches the anchor, so every candidate
// is verified and no tracing runs.
func BenchmarkFairEVBESVerificationRequest(b *testing.B) {
	runFairEVBES(b, false)
}

func runFairEVBES(b *testing.B, tamper bool) {
	b.StopTimer()
	ctx := context.Background()
	database, dht, setup := sharedFairEVBES(b)
	segmentLeaves := fairEVBESSegmentLeaves(b)

	// Experiment 1 on the native query set: the five batches of every
	// (shape, q) in dht_evseb_queries.json. Localization keeps clustered-2.
	if dir := fairNativeQueriesDir(); dir != "" && !tamper {
		batches, err := loadFairNativeBatches(dir, "EVSEB", fairEVBESNativeRequestCounts, setup.leafCount)
		if err != nil {
			b.Fatal(err)
		}
		for _, batch := range batches {
			b.Run(fmt.Sprintf("%s/q=%d/set=%d", batch.Shape, batch.Q, batch.Set), func(b *testing.B) {
				b.StopTimer()
				candidates := make([]string, len(batch.Ordinals))
				for index, ordinal := range batch.Ordinals {
					candidates[index] = fairEVBESLID(ordinal)
				}
				runFairEVBESCase(b, ctx, database, dht, setup, candidates, nil, fairNativeWorkloadVersion)
			})
		}
		return
	}

	for _, placement := range fairEVBESPlacements {
		for _, batchSize := range fairEVBESRequestCounts {
			b.Run(fmt.Sprintf("%s/q=%d", placement, batchSize), func(b *testing.B) {
				b.StopTimer()
				candidates := fairEVBESCandidates(setup.leafCount, segmentLeaves, placement, batchSize)
				var tamperedLIDs []string
				if tamper {
					tamperedLIDs = []string{candidates[len(candidates)/2]}
					if err := replaceFairEVBESTampers(ctx, database, tamperedLIDs); err != nil {
						b.Fatal(err)
					}
				}
				runFairEVBESCase(b, ctx, database, dht, setup, candidates, tamperedLIDs, 8)
			})
		}
	}
	if err := clearFairEVBESTamper(ctx, database); err != nil {
		b.Fatal(err)
	}
}

func runFairEVBESCase(b *testing.B, ctx context.Context, database *pgx.Conn,
	dht *fairEVBESCassandraDHT, setup *fairEVBESSetup, candidates []string,
	tamperedLIDs []string, workloadVersion int) {
	b.Helper()
	tamperedRecords := len(tamperedLIDs)
	wantVerified := len(candidates) - tamperedRecords
	wantIntegrity := tamperedRecords == 0
	run := func() ([]string, bool, error) {
		stored, _, err := loadFairEVBESStoredTree(ctx, database, dht, candidates)
		if err != nil {
			return nil, false, err
		}
		if !bytes.Equal(stored.Root, setup.anchor) {
			return nil, false, ErrStoredTreeMismatch
		}
		prepared, err := loadFairEVBESPrepared(ctx, database, candidates, stored)
		if err != nil {
			return nil, false, err
		}
		_, computed, _ := RecomputeMerkleRootPrepared(prepared, stored)
		return LocalizeComputed(prepared, computed, setup.anchor)
	}
	verified, ok, err := run()
	if err != nil || ok != wantIntegrity || len(verified) != wantVerified {
		b.Fatalf("unexpected persisted result: verified=%d integrity=%v err=%v", len(verified), ok, err)
	}
	if err := validateFairEVBESVerifiedCandidates(candidates, tamperedLIDs, verified); err != nil {
		b.Fatal(err)
	}
	warmup := fairEVBESWarmupCount(b)
	for range warmup {
		verified, ok, err := run()
		if err != nil || ok != wantIntegrity || len(verified) != wantVerified {
			b.Fatalf("unexpected warm-up result: verified=%d integrity=%v err=%v",
				len(verified), ok, err)
		}
		fairEVBESVerified, fairEVBESOK = verified, ok
	}
	_, storedNodes, err := loadFairEVBESStoredTree(ctx, database, dht, candidates)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	b.ReportMetric(float64(len(candidates)), "records/op")
	if os.Getenv("FAIR_REGION0_RECORDS") != "" || workloadVersion == fairNativeWorkloadVersion {
		b.ReportMetric(float64(workloadVersion), "workload-version/op")
	}
	b.ReportMetric(float64(len(candidates)), "candidate-leaves/op")
	b.ReportMetric(float64(setup.treeLevels), "tree-levels/op")
	b.ReportMetric(float64(storedNodes), "stored-tree-nodes/op")
	b.ReportMetric(float64(tamperedRecords), "tampered-records/op")
	b.ReportMetric(3, "postgres-reads/op")
	b.ReportMetric(float64(len(candidates)), "cassandra-dht-lookups/op")
	b.ReportMetric(float64(len(candidates)*dht.hops), "dht-routing-reads/op")
	b.ReportMetric(float64(dht.hops), "dht-hops-per-lookup/op")
	b.ReportMetric(float64(dht.peers), "dht-peers/op")
	b.ReportMetric(float64(dht.maxConcurrency), "dht-concurrency/op")
	b.ReportMetric(1, "proof-generation/op")
	b.ReportMetric(float64(warmup), "warmup-ops")
	b.StartTimer()
	for range b.N {
		verified, ok, err := run()
		if err != nil || ok != wantIntegrity || len(verified) != wantVerified {
			b.Fatalf("unexpected result: verified=%d integrity=%v err=%v", len(verified), ok, err)
		}
		fairEVBESVerified, fairEVBESOK = verified, ok
	}
}

func validateFairEVBESVerifiedCandidates(candidates, tampered, verified []string) error {
	tamperedSet := make(map[string]struct{}, len(tampered))
	for _, lid := range tampered {
		tamperedSet[lid] = struct{}{}
	}
	verifiedSet := make(map[string]struct{}, len(verified))
	for _, lid := range verified {
		if _, duplicate := verifiedSet[lid]; duplicate {
			return fmt.Errorf("EVSEB returned duplicate verified candidate %q", lid)
		}
		verifiedSet[lid] = struct{}{}
	}
	for _, lid := range candidates {
		_, wasTampered := tamperedSet[lid]
		_, wasVerified := verifiedSet[lid]
		if wasTampered == wasVerified {
			return fmt.Errorf("EVSEB candidate %q: tampered=%v verified=%v",
				lid, wasTampered, wasVerified)
		}
	}
	return nil
}

type fairEVBESSetup struct {
	leafCount  int
	anchor     []byte
	treeLevels int
}

// TestMain closes the shared fixture's connection once every benchmark in the
// process has finished. A per-benchmark cleanup cannot do it: the fixture
// outlives the benchmark that happened to seed it.
func TestMain(m *testing.M) {
	code := m.Run()
	if database := fairEVBESShared.database; database != nil {
		_ = database.Close(context.Background())
	}
	if dht := fairEVBESShared.dht; dht != nil {
		dht.Close()
	}
	os.Exit(code)
}

// fairEVBESShared holds the persisted EVSEB fixture for the whole test
// process. Encrypting, folding and anchoring the dataset is setup, not part
// of any measured request, so it runs once and is shared by both benchmark
// functions and by every -count repetition. tampered lists the LIDs whose
// stored ciphertext is currently flipped.
var fairEVBESShared struct {
	once     sync.Once
	database *pgx.Conn
	dht      *fairEVBESCassandraDHT
	setup    *fairEVBESSetup
	err      error
	tampered []string
}

func sharedFairEVBES(b *testing.B) (*pgx.Conn, *fairEVBESCassandraDHT, *fairEVBESSetup) {
	b.Helper()
	if os.Getenv("FAIR_PERSISTENCE") != "1" {
		b.Fatal("FAIR_PERSISTENCE=1 is required; use the shared runner")
	}
	fairEVBESShared.once.Do(func() {
		// Setup only: seeding is not timed, so it may use every core. The
		// timed requests run again under the process's GOMAXPROCS (1).
		defer raiseFairSetupProcs()()
		ctx := context.Background()
		database, err := pgx.Connect(ctx, os.Getenv("FAIR_POSTGRES_DSN"))
		if err != nil {
			fairEVBESShared.err = fmt.Errorf("connect PostgreSQL: %w", err)
			return
		}
		fairEVBESShared.database = database
		dht, err := newFairEVBESCassandraDHT(
			os.Getenv("FAIR_CASSANDRA_HOST"),
			os.Getenv("FAIR_CASSANDRA_PORT"),
			fairEVBESPositiveSetting(b, "FAIR_EVSEB_DHT_CONCURRENCY", 8),
			fairEVBESPositiveSetting(b, "FAIR_DHT_PEERS", fairEVBESDefaultDHTPeers),
		)
		if err != nil {
			fairEVBESShared.err = err
			return
		}
		fairEVBESShared.dht = dht
		if err := dht.Truncate(ctx); err != nil {
			fairEVBESShared.err = fmt.Errorf("reset EVSEB Cassandra DHT: %w", err)
			return
		}
		if err := dht.EnsureRoutes(ctx); err != nil {
			fairEVBESShared.err = fmt.Errorf("seed EVSEB DHT routing table: %w", err)
			return
		}
		if _, err := database.Exec(ctx, "TRUNCATE TABLE evseb.encrypted_logs, "+
			"evseb.stored_tree_positions, evseb.stored_tree_nodes, evseb.stored_tree_state"); err != nil {
			fairEVBESShared.err = fmt.Errorf("reset EVSEB fixtures: %w", err)
			return
		}
		setup, err := seedFairEVBES(ctx, database, dht, b)
		if err != nil {
			fairEVBESShared.err = err
			return
		}
		fairEVBESShared.setup = setup
	})
	if fairEVBESShared.err != nil {
		b.Fatal(fairEVBESShared.err)
	}
	return fairEVBESShared.database, fairEVBESShared.dht, fairEVBESShared.setup
}

// clearFairEVBESTamper puts the flipped ciphertext back, so the next
// benchmark function in this process starts from honest storage.
func clearFairEVBESTamper(ctx context.Context, database *pgx.Conn) error {
	for index := len(fairEVBESShared.tampered) - 1; index >= 0; index-- {
		if err := toggleFairEVBESCiphertext(ctx, database, fairEVBESShared.tampered[index]); err != nil {
			fairEVBESShared.tampered = fairEVBESShared.tampered[:index+1]
			return err
		}
	}
	fairEVBESShared.tampered = nil
	return nil
}

func seedFairEVBES(ctx context.Context, database *pgx.Conn, dht *fairEVBESCassandraDHT,
	b testing.TB) (*fairEVBESSetup, error) {
	b.Helper()
	path := os.Getenv("FAIR_DATASET_PATH")
	if path == "" {
		return nil, fmt.Errorf("FAIR_DATASET_PATH is required")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open fair dataset: %w", err)
	}
	defer file.Close()

	keys, err := crypto.GenerateKeys()
	if err != nil {
		return nil, fmt.Errorf("generate EVSEB keys: %w", err)
	}
	var assigner logstore.LIDAssigner
	leaves := make([]byte, 0, 1<<20) // level-0 digests, flat: 32 bytes a leaf
	count := 0
	region := -1
	logRows := make([][]any, 0, fairEVBESSeedFlushRows)
	positionRows := make([][]any, 0, fairEVBESSeedFlushRows)
	dhtRows := make([]fairEVBESDHTEntry, 0, fairEVBESSeedFlushRows)
	dhtSeeder := newFairEVBESDHTSeeder(ctx, dht)
	dhtSeederOpen := true
	defer func() {
		if dhtSeederOpen {
			_ = dhtSeeder.Close()
		}
	}()
	flush := func() error {
		if len(logRows) == 0 {
			return nil
		}
		if _, err := database.CopyFrom(ctx, pgx.Identifier{"evseb", "encrypted_logs"},
			[]string{"lid", "ct_log", "ct_metadata"}, pgx.CopyFromRows(logRows)); err != nil {
			return fmt.Errorf("persist EVSEB logs: %w", err)
		}
		if _, err := database.CopyFrom(ctx, pgx.Identifier{"evseb", "stored_tree_positions"},
			[]string{"lid", "ordinal"}, pgx.CopyFromRows(positionRows)); err != nil {
			return fmt.Errorf("persist EVSEB stored-tree positions: %w", err)
		}
		if err := dhtSeeder.Submit(dhtRows); err != nil {
			return fmt.Errorf("persist EVSEB Cassandra DHT entries: %w", err)
		}
		logRows = logRows[:0]
		positionRows = positionRows[:0]
		dhtRows = dhtRows[:0]
		return nil
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		ordinal := count
		var input fairEVBESDatasetRecord
		if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
			return nil, fmt.Errorf("decode fair dataset record %d: %w", ordinal, err)
		}
		// The shared dataset is grouped by region: R0, R1, ... in order.
		if input.RegionID != fmt.Sprintf("R%d", region) {
			if input.RegionID != fmt.Sprintf("R%d", region+1) {
				return nil, fmt.Errorf("record %d has region %q; records must be grouped R0, R1, ... in order",
					ordinal, input.RegionID)
			}
			region++
		}
		if input.RawLog == "" || input.SourceID == "" || input.TenantID == "" {
			return nil, fmt.Errorf("record %d has an empty required field", ordinal)
		}
		log := logstore.Log{
			LID: assigner.Next(), Content: input.RawLog,
			Ti: fairEVBESTimestamp(input.RawLog, ordinal),
			Si: input.SourceID, LType: input.RegionID,
		}
		encrypted, err := logstore.EncryptLog(log, keys.Kenc)
		if err != nil {
			return nil, fmt.Errorf("encrypt fair record %d: %w", ordinal, err)
		}
		if log.LID != fairEVBESLID(ordinal) {
			return nil, fmt.Errorf("record %d got LID %q, want %q", ordinal, log.LID, fairEVBESLID(ordinal))
		}
		hLID := HashLog(encrypted.CTLog)
		leaves = append(leaves, hLID...)
		count++
		logRows = append(logRows, []any{log.LID, encrypted.CTLog, encrypted.CTMetadata})
		positionRows = append(positionRows, []any{log.LID, int32(ordinal)})
		var digest [fairEVBESDigest]byte
		copy(digest[:], hLID)
		dhtRows = append(dhtRows, fairEVBESDHTEntry{key: Key(log.LID), hash: digest})
		if len(logRows) == cap(logRows) {
			if err := flush(); err != nil {
				return nil, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan fair dataset: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("fair dataset is empty")
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if err := dhtSeeder.Close(); err != nil {
		return nil, fmt.Errorf("persist EVSEB Cassandra DHT entries: %w", err)
	}
	dhtSeederOpen = false
	// Q9: stored_tree is built once when R is anchored and kept off-chain.
	root, treeLevels, err := buildAndPersistFairEVBESTree(ctx, database, leaves)
	if err != nil {
		return nil, err
	}
	leaves = nil
	chain := blockchain.NewMemory(keys.Kauth, keys.Kindex, time.Now)
	if err := chain.UpdateMerkleRoot(root); err != nil {
		return nil, err
	}
	if _, err := database.Exec(ctx,
		"INSERT INTO evseb.stored_tree_state (tree_id, leaf_count, root) VALUES (1, $1, $2)",
		int32(count), root); err != nil {
		return nil, fmt.Errorf("persist EVSEB stored-tree state: %w", err)
	}
	anchor, err := chain.VerifyIntegrity()
	if err != nil {
		return nil, err
	}
	if err := saveCloudDemoEVSEBTrust(anchor, count); err != nil {
		return nil, fmt.Errorf("save dashboard EVSEB trusted root: %w", err)
	}
	return &fairEVBESSetup{
		leafCount: count, anchor: bytes.Clone(anchor), treeLevels: treeLevels,
	}, nil
}

const fairEVBESDigest = 32

// buildAndPersistFairEVBESTree folds the level-0 digests into the quaternary
// stored_tree exactly as Build does and persists each level as it is
// computed, so setup holds one level rather than the whole tree. leaves and
// every level are flat runs of 32-byte digests. It returns the root and the
// number of levels.
func buildAndPersistFairEVBESTree(ctx context.Context, database *pgx.Conn,
	leaves []byte) ([]byte, int, error) {
	level := 0
	for cur := leaves; ; level++ {
		if err := persistFairEVBESLevel(ctx, database, level, cur); err != nil {
			return nil, 0, err
		}
		if len(cur) <= fairEVBESDigest {
			return bytes.Clone(cur), level + 1, nil
		}
		cur = foldFairEVBESLevel(cur)
	}
}

// foldFairEVBESLevel hashes each group of four consecutive digests, padding a
// short final group by repeating its last child (B14).
func foldFairEVBESLevel(cur []byte) []byte {
	n := len(cur) / fairEVBESDigest
	next := make([]byte, ((n+Arity-1)/Arity)*fairEVBESDigest)
	var g [Arity * fairEVBESDigest]byte
	for parent := 0; parent*fairEVBESDigest < len(next); parent++ {
		for child := range Arity {
			i := min(parent*Arity+child, n-1)
			copy(g[child*fairEVBESDigest:], cur[i*fairEVBESDigest:(i+1)*fairEVBESDigest])
		}
		h := crypto.H(g[:])
		copy(next[parent*fairEVBESDigest:], h[:])
	}
	return next
}

func persistFairEVBESLevel(ctx context.Context, database *pgx.Conn,
	level int, hashes []byte) error {
	index := 0
	_, err := database.CopyFrom(ctx, pgx.Identifier{"evseb", "stored_tree_nodes"},
		[]string{"level", "node_index", "node_hash"},
		pgx.CopyFromFunc(func() ([]any, error) {
			if index*fairEVBESDigest >= len(hashes) {
				return nil, nil
			}
			row := []any{int32(level), int32(index),
				hashes[index*fairEVBESDigest : (index+1)*fairEVBESDigest]}
			index++
			return row, nil
		}))
	if err != nil {
		return fmt.Errorf("persist EVSEB stored-tree level %d: %w", level, err)
	}
	return nil
}

func replaceFairEVBESTampers(ctx context.Context, database *pgx.Conn,
	tamperedLIDs []string) error {
	if err := clearFairEVBESTamper(ctx, database); err != nil {
		return err
	}
	flipped := make([]string, 0, len(tamperedLIDs))
	for _, lid := range tamperedLIDs {
		if err := toggleFairEVBESCiphertext(ctx, database, lid); err != nil {
			for index := len(flipped) - 1; index >= 0; index-- {
				_ = toggleFairEVBESCiphertext(ctx, database, flipped[index])
			}
			return err
		}
		flipped = append(flipped, lid)
	}
	fairEVBESShared.tampered = append([]string(nil), flipped...)
	return nil
}

func fairEVBESTamperedCandidates(candidates []string, tamperCount int) ([]string, error) {
	if tamperCount <= 0 || tamperCount > len(candidates) {
		return nil, fmt.Errorf("tamper count %d must be between 1 and batch size %d",
			tamperCount, len(candidates))
	}
	result := make([]string, tamperCount)
	for index := range tamperCount {
		offset := (2*index + 1) * len(candidates) / (2 * tamperCount)
		result[index] = candidates[offset]
	}
	return result, nil
}

func toggleFairEVBESCiphertext(ctx context.Context, database *pgx.Conn, lid string) error {
	var ciphertext []byte
	if err := database.QueryRow(ctx,
		"SELECT ct_log FROM evseb.encrypted_logs WHERE lid = $1", lid).Scan(&ciphertext); err != nil {
		return err
	}
	if len(ciphertext) == 0 {
		return fmt.Errorf("EVSEB ciphertext %s is empty", lid)
	}
	ciphertext[len(ciphertext)-1] ^= 0xff
	_, err := database.Exec(ctx,
		"UPDATE evseb.encrypted_logs SET ct_log = $1 WHERE lid = $2", ciphertext, lid)
	return err
}

// loadFairEVBESStoredTree reads the part of the persisted stored_tree that one
// request needs: its size and root, the candidates' leaf positions, and every
// sibling group on the candidates' quaternary proof paths. The returned tree
// holds only those nodes, so its memory follows the request rather than the
// dataset. Nodes outside those paths read as nil; Alg. 4.2 only compares them
// against themselves, so their values never affect the outcome. It returns
// the tree and the number of stored nodes read.
func loadFairEVBESStoredTree(ctx context.Context, database *pgx.Conn,
	dht *fairEVBESCassandraDHT, candidates []string) (*MerkleTree, int, error) {
	type dhtLoadResult struct {
		hashes map[string][]byte
		err    error
	}
	dhtContext, cancelDHT := context.WithCancel(ctx)
	defer cancelDHT()
	dhtResult := make(chan dhtLoadResult, 1)
	go func() {
		hashes, err := dht.Load(dhtContext, candidates)
		dhtResult <- dhtLoadResult{hashes: hashes, err: err}
	}()

	rows, err := database.Query(ctx, `SELECT s.leaf_count, s.root, p.lid, p.ordinal
		FROM evseb.stored_tree_state s
		LEFT JOIN evseb.stored_tree_positions p ON p.lid = ANY($1)
		WHERE s.tree_id = 1`, candidates)
	if err != nil {
		return nil, 0, err
	}
	leafCount := -1
	var root []byte
	positions := make(map[string]int, len(candidates))
	for rows.Next() {
		var lid *string
		var ordinal *int32
		var count int32
		if err := rows.Scan(&count, &root, &lid, &ordinal); err != nil {
			rows.Close()
			return nil, 0, err
		}
		leafCount = int(count)
		if lid != nil && ordinal != nil {
			positions[*lid] = int(*ordinal)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	if leafCount <= 0 {
		return nil, 0, fmt.Errorf("PostgreSQL has no EVSEB stored tree")
	}

	widths := []int{leafCount}
	for width := leafCount; width > 1; {
		width = (width + Arity - 1) / Arity
		widths = append(widths, width)
	}
	lids := make(map[int]string, len(positions))
	for lid, index := range positions {
		lids[index] = lid
	}

	top := len(widths) - 1
	wanted := map[[2]int]struct{}{{top, 0}: {}}
	candidateLeaves := make(map[int]struct{}, len(positions))
	for _, leaf := range positions {
		candidateLeaves[leaf] = struct{}{}
		index := leaf
		for level := 0; level < top; level++ {
			parent := index / Arity
			for child := parent * Arity; child < min((parent+1)*Arity, widths[level]); child++ {
				wanted[[2]int{level, child}] = struct{}{}
			}
			index = parent
		}
	}
	levels := make([]int32, 0, len(wanted))
	indexes := make([]int32, 0, len(wanted))
	for node := range wanted {
		if node[0] == 0 {
			if _, fromDHT := candidateLeaves[node[1]]; fromDHT {
				continue
			}
		}
		levels = append(levels, int32(node[0]))
		indexes = append(indexes, int32(node[1]))
	}
	nodes := make(map[[2]int][]byte, len(wanted))
	if len(levels) > 0 {
		nodeRows, err := database.Query(ctx, `SELECT level, node_index, node_hash
			FROM evseb.stored_tree_nodes
			WHERE (level, node_index) IN (SELECT * FROM unnest($1::integer[], $2::integer[]))`,
			levels, indexes)
		if err != nil {
			return nil, 0, err
		}
		for nodeRows.Next() {
			var level, index int32
			var hash []byte
			if err := nodeRows.Scan(&level, &index, &hash); err != nil {
				nodeRows.Close()
				return nil, 0, err
			}
			nodes[[2]int{int(level), int(index)}] = hash
		}
		if err := nodeRows.Err(); err != nil {
			nodeRows.Close()
			return nil, 0, err
		}
		nodeRows.Close()
	}
	storedNodesLoaded := len(nodes)
	if storedNodesLoaded != len(levels) {
		return nil, 0, fmt.Errorf("PostgreSQL returned %d of %d EVSEB authentication nodes",
			storedNodesLoaded, len(levels))
	}
	references := <-dhtResult
	if references.err != nil {
		return nil, 0, references.err
	}
	for lid, index := range positions {
		hash, ok := references.hashes[lid]
		if !ok {
			return nil, 0, fmt.Errorf("Cassandra EVSEB DHT has no hLID for %s", lid)
		}
		nodes[[2]int{0, index}] = bytes.Clone(hash)
	}
	if len(nodes) != len(wanted) {
		return nil, 0, fmt.Errorf("persistent EVSEB proof has %d of %d required nodes", len(nodes), len(wanted))
	}
	tree := NewPartialTree(widths, nodes, lids, positions, nodes[[2]int{top, 0}])
	if !bytes.Equal(tree.Root, root) {
		return nil, 0, ErrStoredTreeMismatch
	}
	if err := verifyFairEVBESDHTReferences(tree, references.hashes); err != nil {
		return nil, 0, err
	}
	return tree, storedNodesLoaded, nil
}

func loadFairEVBESPrepared(ctx context.Context, database *pgx.Conn,
	candidates []string, stored *MerkleTree) (*PreparedVerification, error) {
	rows, err := database.Query(ctx,
		"SELECT lid, ct_log FROM evseb.encrypted_logs WHERE lid = ANY($1)", candidates)
	if err != nil {
		return nil, err
	}
	ciphertexts := make(map[string][]byte, len(candidates))
	for rows.Next() {
		var lid string
		var ciphertext []byte
		if err := rows.Scan(&lid, &ciphertext); err != nil {
			rows.Close()
			return nil, err
		}
		ciphertexts[lid] = ciphertext
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	prepared := &PreparedVerification{candidates: append([]string(nil), candidates...)}
	for _, lid := range candidates {
		index, ok := stored.Index(lid)
		if !ok {
			prepared.unknown = append(prepared.unknown, lid)
			continue
		}
		ciphertext, found := ciphertexts[lid]
		prepared.leaves = append(prepared.leaves, preparedLeaf{
			index: index, ctLog: bytes.Clone(ciphertext), found: found,
		})
	}
	return prepared, nil
}

func fairEVBESWarmupCount(b testing.TB) int {
	b.Helper()
	value := os.Getenv("FAIR_WARMUP")
	if value == "" {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		b.Fatalf("invalid FAIR_WARMUP %q", value)
	}
	return count
}

func fairEVBESPositiveSetting(t testing.TB, name string, fallback int) int {
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

func fairEVBESTimestamp(rawLog string, index int) int64 {
	fields := strings.Fields(rawLog)
	if len(fields) >= 3 {
		value := fields[1] + " " + fields[2]
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed.Unix()
			}
		}
	}
	return time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(index) * time.Millisecond).Unix()
}

// fairEVBESLID is the LID logstore.LIDAssigner gives the record at ordinal.
// Deriving it keeps setup from holding one string per record.
func fairEVBESLID(ordinal int) string {
	return fmt.Sprintf("%012d", ordinal)
}

func fairEVBESSegmentLeaves(t testing.TB) int {
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

func fairEVBESClusteredSegments(placement string) (int, bool) {
	switch placement {
	case "clustered-2":
		return 2, true
	case "clustered-3":
		return 3, true
	case "clustered-4":
		return 4, true
	default:
		return 0, false
	}
}

func fairEVBESCandidates(leafCount, segmentLeaves int, placement string, count int) []string {
	if regionText := os.Getenv("FAIR_REGION0_RECORDS"); regionText != "" {
		region0, err := strconv.Atoi(regionText)
		if err != nil || region0 <= 0 || region0 > leafCount {
			panic(fmt.Sprintf("invalid FAIR_REGION0_RECORDS %q", regionText))
		}
		region1Text := os.Getenv("FAIR_REGION1_RECORDS")
		region1, err := strconv.Atoi(region1Text)
		if err != nil || region1 <= 0 || region0+region1 > leafCount {
			panic(fmt.Sprintf("invalid FAIR_REGION1_RECORDS %q", region1Text))
		}
		start, width := 0, 2*segmentLeaves
		switch placement {
		case "clustered-2":
		case "scattered":
			start, width = region0, leafCount-region0
		default:
			panic("unknown fair EVSEB placement: " + placement)
		}
		if count <= 0 || count > width || 2*segmentLeaves > region0 {
			panic(fmt.Sprintf("invalid %s q=%d for R0=%d", placement, count, region0))
		}
		bitCount := bits.Len(uint(width - 1))
		result := make([]string, 0, count)
		for index := uint64(0); len(result) < count; index++ {
			offset := int(bits.Reverse64(index) >> (64 - bitCount))
			if offset < width {
				result = append(result, fairEVBESLID(start+offset))
			}
		}
		return result
	}
	clusteredSegments, isClustered := fairEVBESClusteredSegments(placement)
	clusteredWindow := clusteredSegments * segmentLeaves
	if count <= 0 || count > leafCount {
		panic(fmt.Sprintf("invalid fair EVSEB count %d for %d leaves", count, leafCount))
	}
	if isClustered && count > 1 {
		if count < 16 || clusteredWindow > leafCount || count > clusteredWindow {
			panic(fmt.Sprintf("invalid %s q=%d, segment leaves=%d, total=%d",
				placement, count, segmentLeaves, leafCount))
		}
	}
	result := make([]string, 0, count)
	for index := range count {
		ordinal := index
		switch {
		case isClustered && count > 1:
			ordinal = index * (clusteredWindow - 1) / (count - 1)
		case isClustered:
		case placement == "scattered" && count > 1:
			ordinal = index * (leafCount - 1) / (count - 1)
		case placement == "scattered":
		default:
			panic("unknown fair EVSEB placement: " + placement)
		}
		result = append(result, fairEVBESLID(ordinal))
	}
	return result
}

func TestFairEVBESCandidatesUseCommonRecordOrdinals(t *testing.T) {
	for _, test := range []struct {
		placement string
		last      int
	}{
		{"clustered-2", 15},
		{"clustered-3", 23},
		{"clustered-4", 31},
	} {
		clustered := fairEVBESCandidates(33, 8, test.placement, 16)
		if clustered[0] != fairEVBESLID(0) || clustered[15] != fairEVBESLID(test.last) {
			t.Fatalf("%s = %v", test.placement, clustered)
		}
		if single := fairEVBESCandidates(33, 8, test.placement, 1); len(single) != 1 || single[0] != fairEVBESLID(0) {
			t.Fatalf("%s q=1 = %v", test.placement, single)
		}
	}
	scattered := fairEVBESCandidates(33, 8, "scattered", 3)
	if scattered[0] != fairEVBESLID(0) || scattered[1] != fairEVBESLID(16) ||
		scattered[2] != fairEVBESLID(32) {
		t.Fatalf("scattered = %v", scattered)
	}
}

func TestFairEVBESVerificationSelectionMatchesLogicalALLWorkload(t *testing.T) {
	t.Setenv("FAIR_REGION0_RECORDS", "32768")
	t.Setenv("FAIR_REGION1_RECORDS", "32768")
	for _, placement := range fairEVBESPlacements {
		for _, count := range []int{1, 16, 256, 16384} {
			candidates := fairEVBESCandidates(98304, 8192, placement, count)
			if len(candidates) != count {
				t.Fatalf("%s/q=%d returned %d candidates", placement, count, len(candidates))
			}
			if count >= 16 {
				wantPrefix := []int{0, 8192, 4096, 12288}
				if placement == "scattered" {
					wantPrefix = []int{32768, 65536, 49152, 81920}
				}
				for i, want := range wantPrefix {
					if candidates[i] != fairEVBESLID(want) {
						t.Fatalf("%s/q=%d candidate %d = %s, want ordinal %d", placement, count, i, candidates[i], want)
					}
				}
			}
			if placement == "clustered-2" && candidates[0] != fairEVBESLID(0) ||
				placement == "scattered" && candidates[0] != fairEVBESLID(32768) {
				t.Fatalf("%s/q=%d begins at unexpected LID %s", placement, count, candidates[0])
			}
		}
	}
}

func TestFairEVBESLIDMatchesAssigner(t *testing.T) {
	var assigner logstore.LIDAssigner
	for ordinal := range 5 {
		if got := assigner.Next(); got != fairEVBESLID(ordinal) {
			t.Fatalf("assigner gave %q for ordinal %d, want %q", got, ordinal, fairEVBESLID(ordinal))
		}
	}
}

// TestFairEVBESFlatFoldMatchesBuild pins the setup-only flat fold to Build
// over sizes that exercise padded groups.
func TestFairEVBESFlatFoldMatchesBuild(t *testing.T) {
	for _, size := range []int{1, 2, 4, 5, 16, 17, 63, 64, 65} {
		lids := make([]string, size)
		leaves := make([][]byte, size)
		flat := make([]byte, 0, size*fairEVBESDigest)
		for index := range size {
			h := crypto.H([]byte(fmt.Sprintf("leaf-%d", index)))
			lids[index] = fairEVBESLID(index)
			leaves[index] = h[:]
			flat = append(flat, h[:]...)
		}
		want := Build(lids, leaves)
		for level, hashes := range want.Levels {
			if !bytes.Equal(flat, bytes.Join(hashes, nil)) {
				t.Fatalf("size %d: level %d differs from Build", size, level)
			}
			if len(flat) > fairEVBESDigest {
				flat = foldFairEVBESLevel(flat)
			} else if level != len(want.Levels)-1 {
				t.Fatalf("size %d: flat fold ended at level %d of %d", size, level, len(want.Levels))
			}
		}
	}
}
