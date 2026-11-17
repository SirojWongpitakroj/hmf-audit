package integrity

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"

	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

const (
	fairEVBESDHTKeyspace = "evseb"
	fairEVBESDHTSelect   = "SELECT h_lid FROM dht_entries WHERE lid_hash = ?"
	fairEVBESDHTInsert   = "INSERT INTO dht_entries (lid_hash, h_lid) VALUES (?, ?)"
)

// fairEVBESCassandraDHT is a persistent, hash-partitioned implementation of
// EVSEB's logical DHT[H(LID)] = hLID mapping for the fair benchmark. Cassandra
// is not a peer-to-peer/Kademlia DHT, so no O(log(peers)) routing claim is made.
type fairEVBESCassandraDHT struct {
	session        *gocql.Session
	maxConcurrency int
}

type fairEVBESDHTEntry struct {
	key  string
	hash [fairEVBESDigest]byte
}

func newFairEVBESCassandraDHT(host, portText string,
	maxConcurrency int) (*fairEVBESCassandraDHT, error) {

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
	cluster := gocql.NewCluster(host)
	cluster.Port = port
	cluster.Keyspace = fairEVBESDHTKeyspace
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("connect EVSEB Cassandra DHT: %w", err)
	}
	return &fairEVBESCassandraDHT{
		session: session, maxConcurrency: maxConcurrency,
	}, nil
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

// Load performs one partition-key lookup per unique LID, bounded by the same
// explicit concurrency control used by the rest of the fair harness.
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
				var hash []byte
				err := dht.session.Query(fairEVBESDHTSelect, Key(lid)).ScanContext(ctx, &hash)
				if errors.Is(err, gocql.ErrNotFound) {
					results <- loadResult{lid: lid}
					continue
				}
				if err != nil {
					results <- loadResult{lid: lid, err: fmt.Errorf("load EVSEB DHT entry %s: %w", lid, err)}
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
