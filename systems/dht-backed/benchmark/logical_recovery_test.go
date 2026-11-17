package benchmark

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	contract "canonicalingestion"
	"dhtbacked/auth"
	"dhtbacked/blockchain"
	"dhtbacked/dht"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/hashing"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

// fileProofFixture is a disk-backed KV test double, not an alternative production
// proof store. It lets local PostgreSQL tests exercise the logical-DHT recovery
// boundary without claiming a live Cassandra integration pass.
type fileProofFixture struct{ dir string }

func (f fileProofFixture) Proof(ctx context.Context, k hashing.Digest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := os.ReadFile(filepath.Join(f.dir, hex.EncodeToString(k[:])))
	if os.IsNotExist(err) {
		return nil, cassandradht.ErrNotFound
	}
	return b, err
}
func (f fileProofFixture) Route(ctx context.Context, peer, level int) (int, error) {
	b, err := os.ReadFile(filepath.Join(f.dir, fmt.Sprintf("route_%d_%d", peer, level)))
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(b))
}
func (f fileProofFixture) PutProof(ctx context.Context, k hashing.Digest, b []byte) error {
	path := filepath.Join(f.dir, hex.EncodeToString(k[:]))
	old, err := os.ReadFile(path)
	if err == nil {
		if !bytes.Equal(old, b) {
			return errors.New("conflicting immutable replay")
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(path, b, 0600)
}
func logicalOptions(t *testing.T, db *sql.DB, keys, work string) (*IncrementalDHT, cassandradht.Config) {
	t.Helper()
	ca, err := auth.LoadCAPublicKey(filepath.Join(keys, "ca.pk"))
	if err != nil {
		t.Fatal(err)
	}
	l := cassandradht.DefaultLimits()
	l.Workers = 1
	l.MaxRequests = 4096
	l.QueueBytes = 4096
	l.MaxValueBytes = 4096
	l.ProofBytes = 4 << 20
	l.AncestorMapBytes = 8 << 20
	l.CacheBytes = 2048
	l.CacheEntries = 4
	l.RecordFetchBatch = 3
	l.RecordBytes = 4 << 20
	cfg := cassandradht.Config{Peers: 4, Concurrency: 1, Overlay: "logical-recovery", CA: ca, Limits: &l}
	a, err := OpenIncrementalDHT(db, IncrementalOptions{RunID: "logical", KeyDir: keys, WorkDir: work, EpochCapacity: 512, MaxRecordBytes: 4096, Cassandra: &cfg})
	if err != nil {
		t.Fatal(err)
	}
	e := contract.DefaultExecution()
	e.CPUWorkers = 8
	a.Execution = &e
	return a, cfg
}

type fixtureProofStore interface {
	cassandradht.KV
	cassandradht.ProofWriter
}

func fixtureProof(t *testing.T, cfg cassandradht.Config, identity [32]byte, dir, realDeployment string) (fixtureProofStore, func()) {
	t.Helper()
	ctx := context.Background()
	if realDeployment != "" {
		raw, err := os.ReadFile(realDeployment)
		if err != nil {
			t.Fatal(err)
		}
		var deployment cassandradht.Deployment
		if err = json.Unmarshal(raw, &deployment); err != nil {
			t.Fatal(err)
		}
		store, err := cassandradht.OpenPersistent(ctx, deployment, true)
		if err != nil {
			t.Fatal(err)
		}
		if err = store.Bind(ctx, identity[:], true); err != nil {
			t.Fatal(err)
		}
		if err = store.EnsureRoutes(ctx, 4); err != nil {
			t.Fatal(err)
		}
		return store, store.Close
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for peer := 0; peer < 4; peer++ {
		for level := 0; level < 2; level++ {
			path := filepath.Join(dir, fmt.Sprintf("route_%d_%d", peer, level))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				if err = os.WriteFile(path, []byte(strconv.Itoa(cassandradht.RouteNext(peer, level))), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return fileProofFixture{dir}, func() {}
}

type failAfterProofFlush struct{ sink publicationSink }

func (f failAfterProofFlush) Put(k hashing.Digest, b []byte) error { return f.sink.Put(k, b) }
func (f failAfterProofFlush) Flush() error {
	if err := f.sink.Flush(); err != nil {
		return err
	}
	return errors.New("injected crash boundary after proof durability, before anchor")
}

type badProofFixture struct {
	cassandradht.KV
	key     hashing.Digest
	missing bool
}

func (f badProofFixture) Proof(ctx context.Context, k hashing.Digest) ([]byte, error) {
	if k == f.key {
		if f.missing {
			return nil, cassandradht.ErrNotFound
		}
		return []byte{255}, nil
	}
	return f.KV.Proof(ctx, k)
}

func TestLogicalRecoveryProcessHelper(t *testing.T) {
	if os.Getenv("DHT_LOGICAL_HELPER") != "1" {
		return
	}
	ctx := context.Background()
	db, err := contract.OpenPostgres(ctx, os.Getenv("DHT_LOGICAL_DB"), 8)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	a, cfg := logicalOptions(t, db, os.Getenv("DHT_LOGICAL_KEYS"), os.Getenv("DHT_LOGICAL_WORK"))
	source := contract.Store{DB: db}
	c := contract.Consumer{Store: source, ID: "dht/logical", StreamID: "logical", Fingerprint: "fixture-v1", ConfigHash: a.ConfigHash(), DeferCompletion: true}
	if err = c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	identity := a.ProofIdentity(c.StreamID, c.Fingerprint)
	proof, closeProof := fixtureProof(t, cfg, identity, os.Getenv("DHT_LOGICAL_PROOFS"), os.Getenv("DHT_LOGICAL_CASSANDRA"))
	defer closeProof()
	sink, err := cassandradht.NewPublisher(ctx, proof, *cfg.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PublishPending(ctx, sink); err != nil {
		t.Fatal(err)
	}
	p, err := c.Progress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for p.Committed < p.Target {
		p, err = c.Step(ctx, source, contract.Limits{MaxRecords: 8, MaxBatchBytes: 1 << 20, MaxRecordBytes: 4096}, a.Apply)
		if err != nil {
			t.Fatal(err)
		}
		if err = a.PublishPending(ctx, sink); err != nil {
			t.Fatal(err)
		}
	}
}
func TestLogicalOutboxRecovery(t *testing.T) { runLogicalOutbox(t, false) }
func TestRealCassandraOutboxRecovery(t *testing.T) {
	if os.Getenv("DHT_TEST_CASSANDRA_HOSTS") == "" {
		t.Skip("real Cassandra integration not configured")
	}
	runLogicalOutbox(t, true)
}

func runLogicalOutbox(t *testing.T, real bool) {
	dsn := os.Getenv("DHT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("DHT_TEST_POSTGRES_DSN unset")
	}
	ctx := context.Background()
	admin, err := contract.OpenPostgres(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("dht_logical_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(`DROP DATABASE "` + name + `" WITH(FORCE)`)
	u.Path = "/" + name
	db, err := contract.OpenPostgres(ctx, u.String(), 8)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	dir := t.TempDir()
	keys := filepath.Join(dir, "keys")
	work := filepath.Join(dir, "work")
	proofDir := filepath.Join(dir, "proofs")
	if err = PrepareDHTKeys(keys); err != nil {
		t.Fatal(err)
	}
	a, cfg := logicalOptions(t, db, keys, work)
	if err = a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	source := contract.Store{DB: db}
	if err = source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = source.Create(ctx, "logical", "fixture-v1"); err != nil {
		t.Fatal(err)
	}
	var entries []contract.Entry
	for i := uint64(1); i <= 553; i++ {
		ds := []string{"WADI", "HAI", "SWAT"}[i%3]
		origin := contract.SourceObservation{Dataset: ds, File: ds + "/raw/fixture.csv", Row: i, Column: 2}
		raw, _ := json.Marshal(struct {
			CanonicalObservation
			Source contract.SourceObservation `json:"source"`
		}{CanonicalObservation{Dataset: ds, Site: ds + "/site", Device: "sensor", Type: "sensor", Timestamp: time.Unix(int64(i), 0).UTC(), Value: fmt.Sprint(i), Label: "normal"}, origin})
		entries = append(entries, contract.Entry{Ordinal: i, SourceID: origin.ID(), Record: raw})
	}
	limits := contract.Limits{MaxRecords: 1024, MaxBatchBytes: 1 << 20, MaxRecordBytes: 4096}
	if err = source.Append(ctx, "logical", entries, limits); err != nil {
		t.Fatal(err)
	}
	c := contract.Consumer{Store: source, ID: "dht/logical", StreamID: "logical", Fingerprint: "fixture-v1", ConfigHash: a.ConfigHash(), DeferCompletion: true}
	if err = c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Begin(ctx, 1000000); err != nil {
		t.Fatal(err)
	}
	legacy := *a
	legacy.Options.Cassandra = nil
	legacy.Options.Peers.Overlay = cfg.Overlay
	oldConsumer := c
	oldConsumer.ConfigHash = legacy.ConfigHash()
	if oldConsumer.ConfigHash == c.ConfigHash || oldConsumer.Open(ctx) == nil {
		t.Fatal("backend change silently adopted an incompatible durable cursor")
	}
	// Only the small fixture overrides internal targets; production CLIs still
	// accept exclusively the five cumulative manual milestones.
	db.Exec(`UPDATE canonical_ingestion.consumers SET target=529 WHERE consumer_id='dht/logical'`)
	limits.MaxRecords = 512
	p, err := c.Step(ctx, source, limits, a.Apply)
	if err != nil || p.Committed != 512 {
		t.Fatal(p, err)
	}
	var cipherBefore []byte
	db.QueryRow(`SELECT c FROM dhtbacked.records ORDER BY record_id LIMIT 1`).Scan(&cipherBefore)
	deploymentFile := ""
	if real {
		deployment := cassandradht.Deployment{LogicalNodes: 4, Overlay: cfg.Overlay, Hosts: strings.Split(os.Getenv("DHT_TEST_CASSANDRA_HOSTS"), ","), Port: 9042, Keyspace: name, ReplicationFactor: 1, TimeoutSeconds: 10, Limits: *cfg.Limits}
		t.Cleanup(func() {
			// Only this fixture's generated keyspace is eligible for cleanup.
			cluster := gocql.NewCluster(deployment.Hosts...)
			cluster.Port = deployment.Port
			cluster.Timeout = 10 * time.Second
			cleanup, e := cluster.CreateSession()
			if e != nil {
				t.Error("test keyspace cleanup connection", e)
				return
			}
			defer cleanup.Close()
			if e = cleanup.Query("DROP KEYSPACE IF EXISTS " + name).ExecContext(context.Background()); e != nil {
				t.Error("test keyspace cleanup", e)
			}
		})
		raw, _ := json.Marshal(deployment)
		deploymentFile = filepath.Join(dir, "cassandra.json")
		if err = os.WriteFile(deploymentFile, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	identity := a.ProofIdentity(c.StreamID, c.Fingerprint)
	proof, closeProof := fixtureProof(t, cfg, identity, proofDir, deploymentFile)
	sink, err := cassandradht.NewPublisher(ctx, proof, *cfg.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.PublishPending(ctx, failAfterProofFlush{sink}); err == nil {
		t.Fatal("durability/anchor crash boundary did not fail")
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM dhtbacked.dht_epoch_anchors`).Scan(&count)
	if count != 0 {
		t.Fatal("anchor committed before publication completed")
	}
	closeProof()
	command := exec.Command(os.Args[0], "-test.run=^TestLogicalRecoveryProcessHelper$", "-test.count=1")
	command.Env = append(os.Environ(), "DHT_LOGICAL_HELPER=1", "DHT_LOGICAL_DB="+u.String(), "DHT_LOGICAL_KEYS="+keys, "DHT_LOGICAL_WORK="+work, "DHT_LOGICAL_PROOFS="+proofDir, "DHT_LOGICAL_CASSANDRA="+deploymentFile)
	if output, e := command.CombinedOutput(); e != nil {
		t.Fatalf("fresh process recovery: %v\n%s", e, output)
	}
	a, cfg = logicalOptions(t, db, keys, work)
	p, err = c.Progress(ctx)
	if err != nil || p.Committed != 529 {
		t.Fatal("restart did not resume cursor to exact target", p, err)
	}
	proof, closeProof = fixtureProof(t, cfg, identity, proofDir, deploymentFile)
	defer closeProof()
	sink, err = cassandradht.NewPublisher(ctx, proof, *cfg.Limits)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec(`UPDATE canonical_ingestion.consumers SET target=553 WHERE consumer_id='dht/logical'`)
	for p.Committed < 553 {
		p, err = c.Step(ctx, source, limits, a.Apply)
		if err != nil {
			t.Fatal(err)
		}
		if err = a.PublishPending(ctx, sink); err != nil {
			t.Fatal(err)
		}
	}
	tx, _ := db.BeginTx(ctx, nil)
	if err = a.PublicationReady(ctx, tx, p); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	tx.Rollback()
	var cipherAfter []byte
	db.QueryRow(`SELECT c FROM dhtbacked.records ORDER BY record_id LIMIT 1`).Scan(&cipherAfter)
	if !bytes.Equal(cipherBefore, cipherAfter) {
		t.Fatal("restart changed original ciphertext")
	}
	var ids, sources, natives int
	db.QueryRow(`SELECT count(*),count(DISTINCT source_id),count(DISTINCT native_id) FROM canonical_ingestion.mappings WHERE consumer_id='dht/logical'`).Scan(&ids, &sources, &natives)
	if ids != 553 || sources != 553 || natives != 553 {
		t.Fatal("duplicate/replayed canonical or native IDs", ids, sources, natives)
	}
	// Decrypt only inside this producer-side test to establish exact equality
	// with the canonical stream. Production verification never decrypts.
	key, _ := os.ReadFile(filepath.Join(keys, "aead.key"))
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	rows, err := db.Query(`SELECT m.ordinal,m.source_id,r.c,r.t,r.ad FROM canonical_ingestion.mappings m JOIN dhtbacked.records r ON r.record_id=m.native_id WHERE consumer_id='dht/logical' ORDER BY ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n uint64
		var id string
		var ciphertext, tag, ad []byte
		if err = rows.Scan(&n, &id, &ciphertext, &tag, &ad); err != nil {
			t.Fatal(err)
		}
		plain, e := gcm.Open(nil, ciphertext[:12], append(append([]byte(nil), ciphertext[12:]...), tag...), ad)
		if e != nil {
			t.Fatal(e)
		}
		var payload struct{ Message string }
		json.Unmarshal(plain, &payload)
		if payload.Message != string(entries[n-1].Record) || id != entries[n-1].SourceID {
			t.Fatal("canonical payload changed", n)
		}
	}
	rows.Close()
	client, err := cassandradht.New(ctx, proof, cfg.CA, cfg)
	if err != nil {
		t.Fatal(err)
	}
	request := contract.AuditRequest{Version: 1, StreamID: c.StreamID, Fingerprint: c.Fingerprint, ExpectedN: 553, Entries: entries}
	metrics, err := a.Audit(ctx, c, request, client, 1024)
	if err != nil || metrics.RequestedRecords != 553 || metrics.AncestorReuseHits == 0 || metrics.DHTLookupMode != "four-node-logical-cassandra" || metrics.LogicalRoutingSteps != 2*metrics.DHTLookups || metrics.CassandraProofReads == 0 || metrics.CassandraReadBytes == 0 {
		t.Fatal("logical audit/reuse/lookup metrics", metrics, err)
	}
	for owner, n := range metrics.DHTOwnerLookups {
		if n == 0 {
			t.Fatal("owner missing from lookup accounting", owner)
		}
	}
	if metrics.CacheBytes > 2048 || metrics.CacheEntries > 4 {
		t.Fatal("cache bound", metrics)
	}
	for _, missing := range []bool{false, true} {
		bad, _ := cassandradht.New(ctx, badProofFixture{proof, dht.KeyR(dht.TreeID(1, producer.LogTypes[0])), missing}, cfg.CA, cfg)
		if _, err = a.Audit(ctx, c, request, bad, 1024); err == nil {
			t.Fatal("corrupt/missing required evidence accepted", missing)
		}
	}
	// Dense native MK-Tree roots are the storage-refactor oracle.
	anchor := &blockchain.SQLAnchor{DB: db}
	for epoch := uint64(1); ; epoch++ {
		capacity, _, e := anchor.Geometry(ctx, epoch)
		if errors.Is(e, blockchain.ErrNotAnchored) || errors.Is(e, sql.ErrNoRows) {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		geometry := epochConfig(capacity)
		var roots []hashing.Digest
		for _, typ := range geometry.LogTypes {
			tree, e := mktree.NewTree(epoch, typ, capacity, geometry.Stride)
			if e != nil {
				t.Fatal(e)
			}
			rows, e := db.Query(`SELECT d FROM dhtbacked.records WHERE epoch=$1 AND log_type=$2 ORDER BY leaf_index`, int64(epoch), typ)
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				var raw []byte
				rows.Scan(&raw)
				var h hashing.Digest
				copy(h[:], raw)
				tree.Insert(h)
			}
			rows.Close()
			roots = append(roots, tree.Root())
		}
		root, e := anchor.GetRoot(ctx, epoch)
		if e != nil || root != mktree.RootOfRoots(roots) {
			t.Fatal("native root changed", epoch, e)
		}
	}
}
