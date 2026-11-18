package integrity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const (
	fairEVBESDHTKeyspace = "evseb"
	fairEVBESDHTSelect   = "SELECT h_lid FROM dht_entries WHERE lid_hash = ?"
	fairEVBESDHTInsert   = "INSERT INTO dht_entries (lid_hash, h_lid) VALUES (?, ?)"

	fairEVBESDHTRouteCreate = "CREATE TABLE IF NOT EXISTS routing_table " +
		"(peer int, level int, next_peer int, PRIMARY KEY (peer, level))"
	fairEVBESDHTRouteSelect = "SELECT next_peer FROM routing_table WHERE peer = ? AND level = ?"
	fairEVBESDHTRouteInsert = "INSERT INTO routing_table (peer, level, next_peer) VALUES (?, ?, ?)"

	// fairEVBESDHTMaxPeers bounds the modelled overlay, as the DHT-backed system does.
	fairEVBESDHTMaxPeers = 65536
)

// fairEVBESCassandraDHT is a persistent implementation of EVSEB's logical
// DHT[H(LID)] = hLID mapping for the fair benchmark. Cassandra is not a
// peer-to-peer/Kademlia DHT, so the O(log n) cost of resolving a key among n
// peers is modelled the same way the DHT-backed system models it: a lookup
// first walks ceil(log2 n) routing hops, each a real read of a persisted
// routing_table row in Cassandra, and only then reads the value at the
// responsible peer. Nothing is simulated or delayed; all cost is measured
// Cassandra time. With one peer a lookup is a single partition-key read.
type fairEVBESCassandraDHT struct {
	session        *gocql.Session
	maxConcurrency int
	peers          int
	hops           int
	routingReads   atomic.Int64
}

// fairEVBESDHTHops is ceil(log2 peers), and zero for a single peer.
func fairEVBESDHTHops(peers int) int {
	if peers <= 1 {
		return 0
	}
	return bits.Len(uint(peers - 1))
}

// fairEVBESDHTPeerOf assigns a key to one of n peers: the first eight bytes of
// SHA-256 of the key read as a big-endian integer, reduced modulo n.
func fairEVBESDHTPeerOf(key string, peers int) int {
	h := sha256.Sum256([]byte(key))
	return int(binary.BigEndian.Uint64(h[:8]) % uint64(peers))
}

// fairEVBESDHTRouteNext is the routing-table entry (peer, level) must hold:
// the peer reached by flipping bit level of peer's identifier.
func fairEVBESDHTRouteNext(peer, level int) int { return peer ^ (1 << level) }

type fairEVBESDHTEntry struct {
	key  string
	hash [fairEVBESDigest]byte
}

func newFairEVBESCassandraDHT(host, portText string,
	maxConcurrency, peers int) (*fairEVBESCassandraDHT, error) {

	if host == "" {
		return nil, fmt.Errorf("FAIR_CASSANDRA_HOST is required for the EVSEB persistent DHT")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		return nil, fmt.Errorf("invalid FAIR_CASSANDRA_PORT %q", portText)
	}
	if maxConcurrency <= 0 {
		return nil, fmt.Errorf("EVSEB DHT concurrency must be positive")
	}
	if peers <= 0 || peers > fairEVBESDHTMaxPeers {
		return nil, fmt.Errorf("EVSEB DHT peer count must be between 1 and %d, got %d",
			fairEVBESDHTMaxPeers, peers)
	}
	cluster := gocql.NewCluster(host)
	cluster.Port = port
	cluster.Keyspace = fairEVBESDHTKeyspace
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connect EVSEB Cassandra DHT: %w", err)
	}
	return &fairEVBESCassandraDHT{
		session: session, maxConcurrency: maxConcurrency,
		peers: peers, hops: fairEVBESDHTHops(peers),
	}, nil
}

// EnsureRoutes (re)creates the persisted routing table: one row per
// (peer, level), each holding the peer one bit-flip away at that level. It is
// setup work, outside every measured request.
func (dht *fairEVBESCassandraDHT) EnsureRoutes(ctx context.Context) error {
	if dht == nil || dht.session == nil {
		return fmt.Errorf("EVSEB Cassandra DHT is not initialized")
	}
	if err := dht.session.Query(fairEVBESDHTRouteCreate).ExecContext(ctx); err != nil {
		return fmt.Errorf("create EVSEB DHT routing table: %w", err)
	}
	if err := dht.session.Query("TRUNCATE routing_table").ExecContext(ctx); err != nil {
		return fmt.Errorf("reset EVSEB DHT routing table: %w", err)
	}
	if dht.hops == 0 {
		return nil
	}
	jobs := make(chan int, dht.peers)
	for peer := range dht.peers {
		jobs <- peer
	}
	close(jobs)
	var (
		wait     sync.WaitGroup
		errMu    sync.Mutex
		firstErr error
	)
	workers := min(dht.maxConcurrency, dht.peers)
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for peer := range jobs {
				// One partition (peer) per batch, so the batch stays on one replica set.
				batch := dht.session.Batch(gocql.UnloggedBatch)
				for level := range dht.hops {
					batch.Query(fairEVBESDHTRouteInsert, peer, level, fairEVBESDHTRouteNext(peer, level))
				}
				if err := batch.ExecContext(ctx); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("write EVSEB DHT routes for peer %d: %w", peer, err)
					}
					errMu.Unlock()
				}
			}
		}()
	}
	wait.Wait()
	return firstErr
}

// fairEVBESDHTWalk performs the routing walk for key over readRoute, returning
// the number of routing rows read. It starts at peer 0 and corrects one bit of
// the peer identifier per hop, most significant first.
func fairEVBESDHTWalk(ctx context.Context, key string, peers, hops int,
	readRoute func(ctx context.Context, peer, level int) (int, error)) (int, error) {

	target := fairEVBESDHTPeerOf(key, peers)
	cur, reads := 0, 0
	for level := hops - 1; level >= 0; level-- {
		next, err := readRoute(ctx, cur, level)
		if err != nil {
			return reads, fmt.Errorf("EVSEB DHT route (%d,%d): %w", cur, level, err)
		}
		reads++
		if next != fairEVBESDHTRouteNext(cur, level) {
			return reads, fmt.Errorf("EVSEB DHT conflicting persisted route (%d,%d)", cur, level)
		}
		if (cur^target)>>level&1 == 1 {
			cur = next
		}
	}
	if cur != target {
		return reads, fmt.Errorf("EVSEB DHT routing reached peer %d, want %d", cur, target)
	}
	return reads, nil
}

// RoutingReads is the number of routing-table rows read since the DHT opened.
func (dht *fairEVBESCassandraDHT) RoutingReads() int64 { return dht.routingReads.Load() }

// lookup resolves one LID. The walk starts at peer 0 and corrects one bit of the
// peer identifier per hop, most significant first, so it is always exactly
// hops routing reads long wherever the key lives. It fails if the persisted
// routing table does not lead to the responsible peer.
func (dht *fairEVBESCassandraDHT) lookup(ctx context.Context, lid string) ([]byte, bool, error) {
	key := Key(lid)
	reads, err := fairEVBESDHTWalk(ctx, key, dht.peers, dht.hops,
		func(ctx context.Context, peer, level int) (int, error) {
			var next int
			err := dht.session.Query(fairEVBESDHTRouteSelect, peer, level).ScanContext(ctx, &next)
			return next, err
		})
	dht.routingReads.Add(int64(reads))
	if err != nil {
		return nil, false, err
	}
	var hash []byte
	err = dht.session.Query(fairEVBESDHTSelect, key).ScanContext(ctx, &hash)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load EVSEB DHT entry %s: %w", lid, err)
	}
	return hash, true, nil
}

func (dht *fairEVBESCassandraDHT) Close() {
	if dht != nil && dht.session != nil {
		dht.session.Close()
	}
}

func (dht *fairEVBESCassandraDHT) Truncate(ctx context.Context) error {
	if dht == nil || dht.session == nil {
		return fmt.Errorf("EVSEB Cassandra DHT is not initialized")
	}
	return dht.session.Query("TRUNCATE dht_entries").ExecContext(ctx)
}

// Load resolves every unique LID through the modelled overlay (hops routing
// reads, then the value read), bounded by the same explicit concurrency control
// used by the rest of the fair harness.
func (dht *fairEVBESCassandraDHT) Load(ctx context.Context,
	lids []string) (map[string][]byte, error) {

	if dht == nil || dht.session == nil {
		return nil, fmt.Errorf("EVSEB Cassandra DHT is not initialized")
	}
	unique := make([]string, 0, len(lids))
	seen := make(map[string]struct{}, len(lids))
	for _, lid := range lids {
		if _, exists := seen[lid]; exists {
			continue
		}
		seen[lid] = struct{}{}
		unique = append(unique, lid)
	}
	if len(unique) == 0 {
		return map[string][]byte{}, nil
	}

	type loadResult struct {
		lid   string
		hash  []byte
		found bool
		err   error
	}
	jobs := make(chan string, len(unique))
	results := make(chan loadResult, len(unique))
	for _, lid := range unique {
		jobs <- lid
	}
	close(jobs)

	workers := min(dht.maxConcurrency, len(unique))
	var wait sync.WaitGroup
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for lid := range jobs {
				hash, found, err := dht.lookup(ctx, lid)
				if err != nil {
					results <- loadResult{lid: lid, err: err}
					continue
				}
				if !found {
					results <- loadResult{lid: lid}
					continue
				}
				if len(hash) != fairEVBESDigest {
					results <- loadResult{lid: lid, err: fmt.Errorf(
						"EVSEB DHT entry %s has %d-byte hLID, want %d", lid, len(hash), fairEVBESDigest)}
					continue
				}
				results <- loadResult{lid: lid, hash: bytes.Clone(hash), found: true}
			}
		}()
	}
	wait.Wait()
	close(results)

	loaded := make(map[string][]byte, len(unique))
	var firstError error
	for result := range results {
		if result.err != nil && firstError == nil {
			firstError = result.err
		}
		if result.found {
			loaded[result.lid] = result.hash
		}
	}
	if firstError != nil {
		return nil, firstError
	}
	return loaded, nil
}

func (dht *fairEVBESCassandraDHT) putBatch(ctx context.Context,
	entries []fairEVBESDHTEntry) error {

	if len(entries) == 0 {
		return nil
	}
	batch := dht.session.Batch(gocql.UnloggedBatch)
	for _, entry := range entries {
		batch.Query(fairEVBESDHTInsert, entry.key, entry.hash[:])
	}
	if err := batch.ExecContext(ctx); err != nil {
		return fmt.Errorf("write %d EVSEB DHT entries: %w", len(entries), err)
	}
	return nil
}

// fairEVBESDHTSeeder bounds setup-time writes while the dataset continues to
// stream into PostgreSQL. Setup is outside every measured request.
type fairEVBESDHTSeeder struct {
	ctx       context.Context
	dht       *fairEVBESCassandraDHT
	batches   chan []fairEVBESDHTEntry
	wait      sync.WaitGroup
	closeOnce sync.Once
	errorMu   sync.Mutex
	firstErr  error
}

func newFairEVBESDHTSeeder(ctx context.Context,
	dht *fairEVBESCassandraDHT) *fairEVBESDHTSeeder {

	seeder := &fairEVBESDHTSeeder{
		ctx: ctx, dht: dht,
		batches: make(chan []fairEVBESDHTEntry, dht.maxConcurrency),
	}
	seeder.wait.Add(dht.maxConcurrency)
	for range dht.maxConcurrency {
		go func() {
			defer seeder.wait.Done()
			for entries := range seeder.batches {
				if seeder.Err() != nil {
					continue
				}
				if err := dht.putBatch(ctx, entries); err != nil {
					seeder.setError(err)
				}
			}
		}()
	}
	return seeder
}

func (seeder *fairEVBESDHTSeeder) Submit(entries []fairEVBESDHTEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if err := seeder.Err(); err != nil {
		return err
	}
	batch := append([]fairEVBESDHTEntry(nil), entries...)
	select {
	case seeder.batches <- batch:
		return nil
	case <-seeder.ctx.Done():
		return seeder.ctx.Err()
	}
}

func (seeder *fairEVBESDHTSeeder) Close() error {
	seeder.closeOnce.Do(func() {
		close(seeder.batches)
		seeder.wait.Wait()
	})
	return seeder.Err()
}

func (seeder *fairEVBESDHTSeeder) setError(err error) {
	seeder.errorMu.Lock()
	defer seeder.errorMu.Unlock()
	if seeder.firstErr == nil {
		seeder.firstErr = err
	}
}

func (seeder *fairEVBESDHTSeeder) Err() error {
	seeder.errorMu.Lock()
	defer seeder.errorMu.Unlock()
	return seeder.firstErr
}

// verifyFairEVBESDHTReferences authenticates the Cassandra values through the
// loaded stored-tree paths before live cloud ciphertexts replace those leaves.
func verifyFairEVBESDHTReferences(stored *MerkleTree,
	references map[string][]byte) error {

	if stored == nil || stored.height() == 0 {
		return ErrStoredTreeMismatch
	}
	computed := &ComputedTree{
		Stored: stored,
		Over:   make([]map[int][]byte, stored.height()),
	}
	for level := range computed.Over {
		computed.Over[level] = make(map[int][]byte)
	}
	for lid, hash := range references {
		index, anchored := stored.Index(lid)
		if !anchored {
			continue
		}
		if len(hash) != fairEVBESDigest {
			return fmt.Errorf("EVSEB DHT entry %s has invalid hLID length", lid)
		}
		computed.Over[0][index] = bytes.Clone(hash)
	}
	if len(computed.Over[0]) == 0 {
		return nil
	}
	for level := 1; level < stored.height(); level++ {
		for _, index := range sortedKeys(computed.Over[level-1]) {
			parent := index / Arity
			if computed.Over[level][parent] == nil {
				computed.Over[level][parent] = hashConcatGroup(computed.group(level-1, parent))
			}
		}
	}
	computed.Root = computed.node(stored.height()-1, 0)
	if !bytes.Equal(computed.Root, stored.Root) {
		return fmt.Errorf("%w: Cassandra DHT leaves do not authenticate to the anchored tree",
			ErrStoredTreeMismatch)
	}
	return nil
}

func TestFairEVBESDHTReferencesAuthenticateToRoot(t *testing.T) {
	lids := []string{"000", "001", "002", "003", "004", "005", "006", "007"}
	leaves := make([][]byte, len(lids))
	for index, lid := range lids {
		leaves[index] = HashLog([]byte("ciphertext-" + lid))
	}
	tree := Build(lids, leaves)
	references := map[string][]byte{
		lids[1]: bytes.Clone(leaves[1]),
		lids[6]: bytes.Clone(leaves[6]),
	}
	if err := verifyFairEVBESDHTReferences(tree, references); err != nil {
		t.Fatalf("valid DHT references failed: %v", err)
	}
	references[lids[6]][0] ^= 0xff
	if err := verifyFairEVBESDHTReferences(tree, references); !errors.Is(err, ErrStoredTreeMismatch) {
		t.Fatalf("corrupt DHT reference error = %v, want ErrStoredTreeMismatch", err)
	}
}

func TestFairEVBESDHTRoutingIsTenHopsAt1024Peers(t *testing.T) {
	if got := fairEVBESDHTHops(1024); got != 10 {
		t.Fatalf("hops(1024) = %d, want 10", got)
	}
	for peers, want := range map[int]int{1: 0, 2: 1, 4: 2, 1000: 10, 1024: 10, 1025: 11} {
		if got := fairEVBESDHTHops(peers); got != want {
			t.Fatalf("hops(%d) = %d, want %d", peers, got, want)
		}
	}
	identity := func(_ context.Context, peer, level int) (int, error) {
		return fairEVBESDHTRouteNext(peer, level), nil
	}
	owners := make(map[int]int)
	for i := range 2000 {
		key := Key(fmt.Sprintf("%08d", i))
		reads, err := fairEVBESDHTWalk(context.Background(), key, 1024, 10, identity)
		if err != nil || reads != 10 {
			t.Fatalf("walk reads=%d err=%v, want exactly 10 reads", reads, err)
		}
		owners[fairEVBESDHTPeerOf(key, 1024)]++
	}
	if len(owners) < 500 {
		t.Fatalf("keys reached only %d of 1024 peers", len(owners))
	}
}

func TestFairEVBESDHTRoutingSinglePeerHasNoReads(t *testing.T) {
	reads, err := fairEVBESDHTWalk(context.Background(), Key("x"), 1, 0,
		func(context.Context, int, int) (int, error) {
			t.Fatal("route read with one peer")
			return 0, nil
		})
	if err != nil || reads != 0 {
		t.Fatalf("reads=%d err=%v", reads, err)
	}
}

func TestFairEVBESDHTRoutingRejectsConflictingRoute(t *testing.T) {
	wrong := func(_ context.Context, peer, level int) (int, error) {
		return fairEVBESDHTRouteNext(peer, level) ^ 1, nil
	}
	if _, err := fairEVBESDHTWalk(context.Background(), Key("x"), 1024, 10, wrong); err == nil {
		t.Fatal("conflicting persisted route accepted")
	}
	failing := func(context.Context, int, int) (int, error) { return 0, errors.New("down") }
	if _, err := fairEVBESDHTWalk(context.Background(), Key("x"), 1024, 10, failing); err == nil {
		t.Fatal("route read error swallowed")
	}
}
