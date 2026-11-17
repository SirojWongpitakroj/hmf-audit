package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
	"dhtbacked/dht"
	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/hashing"
	"dhtbacked/producer"
)

// The proof payload fixture stays unchanged. Only the explicit routing model
// supplies a larger, deterministic table; this is not a live Cassandra test.
type modeledFixtureRoutes struct {
	cassandradht.KV
	nodes int
}

func (f modeledFixtureRoutes) Route(ctx context.Context, peer, level int) (int, error) {
	if peer < 0 || peer >= f.nodes || level < 0 || level >= cassandradht.Hops(f.nodes) {
		return 0, fmt.Errorf("invalid modeled route")
	}
	return cassandradht.RouteNext(peer, level), ctx.Err()
}

func TestAuditRoutingModelReusesExistingRecordsProofsAndIdentity(t *testing.T) {
	db, dsn := testfixture.Database(t, "DHT_TEST_POSTGRES_DSN")
	db.SetMaxOpenConns(6)
	ctx := context.Background()
	keys, work, proofDir := t.TempDir(), t.TempDir(), t.TempDir()
	if err := PrepareDHTKeys(keys); err != nil {
		t.Fatal(err)
	}
	a, native := logicalOptions(t, db, keys, work)
	a.Options.EpochCapacity = 32
	a.Execution = nil
	// Pin the original four-node fingerprint encoding, not merely a comparison
	// against the optimized function itself.
	var pub []byte
	for _, key := range a.keys {
		pub = append(pub, key.Public...)
	}
	oldEncoding, _ := json.Marshal(struct {
		Version                 int
		Backend, RunID, Overlay string
		LogicalNodes            int
		Capacity                uint64
		KeyHash                 hashing.Digest
		Public, CA              []byte
	}{2, "logical-cassandra-v1", a.Options.RunID, native.Overlay, 4, a.Options.EpochCapacity, a.keyHash, pub, a.ca})
	oldHash := fmt.Sprintf("%x", sha256.Sum256(oldEncoding))
	if a.ConfigHash() != oldHash {
		t.Fatal("existing native fingerprint changed")
	}
	if err := a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	source := contract.Store{DB: db}
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.Create(ctx, "fixture", "same-source"); err != nil {
		t.Fatal(err)
	}
	entries := testfixture.Entries(1, 96)
	limits := contract.Limits{MaxRecords: 96, MaxBatchBytes: 1 << 20, MaxRecordBytes: 4096}
	if err := source.Append(ctx, "fixture", entries, limits); err != nil {
		t.Fatal(err)
	}
	c := contract.Consumer{Store: source, ID: "dht/logical", StreamID: "fixture", Fingerprint: "same-source", ConfigHash: oldHash, DeferCompletion: true}
	if err := c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Begin(ctx, 1000000); err != nil {
		t.Fatal(err)
	}
	// Only the local fixture target is shortened; no production stage is run.
	if _, err := db.Exec(`UPDATE canonical_ingestion.consumers SET target=96 WHERE consumer_id=$1`, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Step(ctx, source, limits, a.Apply); err != nil {
		t.Fatal(err)
	}
	nativeIdentity := a.ProofIdentity(c.StreamID, c.Fingerprint)
	proof, closeProof := fixtureProof(t, native, nativeIdentity, proofDir, "")
	defer closeProof()
	sink, err := cassandradht.NewPublisher(ctx, proof, *native.Limits)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.PublishPending(ctx, sink); err != nil {
		t.Fatal(err)
	}
	client, err := cassandradht.New(ctx, proof, native.CA, native)
	if err != nil {
		t.Fatal(err)
	}
	request := contract.AuditRequest{Version: 1, StreamID: c.StreamID, Fingerprint: c.Fingerprint, ExpectedN: 96, Entries: entries[:24]}
	baseline, err := a.Audit(ctx, c, request, client, 96)
	if err != nil {
		t.Fatal(err)
	}
	coldCtx := contract.WithAuditCacheReset(ctx, client.ClearAuditCache)
	for range 2 {
		m, e := contract.AuditWithColdCaches(coldCtx, func(ctx context.Context) (contract.AuditMetrics, error) {
			if client.Stats().CacheEntries != 0 {
				t.Fatal("warm proof cache start")
			}
			return a.Audit(ctx, c, request, client, 96)
		})
		if e != nil || m.RequestedRecords != baseline.RequestedRecords || m.EvidenceBytes != baseline.EvidenceBytes || m.CassandraProofReads == 0 || m.ProofCacheMisses == 0 || m.LogicalRoutingSteps != m.DHTLookups*uint64(client.HopsPerLookup()) || client.Stats().CacheEntries != 0 {
			t.Fatal("cold DHT audit changed native verification or bypassed retrieval", m, e)
		}
	}
	snapshot := func() string {
		return testfixture.Snapshot(t, db, `SELECT * FROM dhtbacked.records ORDER BY record_id`, `SELECT * FROM dhtbacked.dht_ingestion_epochs ORDER BY epoch`, `SELECT * FROM dhtbacked.dht_epoch_anchors ORDER BY epoch`, `SELECT * FROM canonical_ingestion.consumers ORDER BY consumer_id`, `SELECT * FROM canonical_ingestion.mappings ORDER BY ordinal`, `SELECT * FROM canonical_ingestion.streams ORDER BY stream_id`)
	}
	proofSnapshot := func() string {
		rows, err := os.ReadDir(proofDir)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.New()
		for _, row := range rows {
			b, err := os.ReadFile(filepath.Join(proofDir, row.Name()))
			if err != nil {
				t.Fatal(err)
			}
			h.Write([]byte(row.Name()))
			h.Write(b)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	before, proofBefore := snapshot(), proofSnapshot()
	modelIdentity, err := cassandradht.AuditRoutingIdentity(nativeIdentity[:], 1024)
	if err != nil {
		t.Fatal(err)
	}
	modelConfig := native
	modelConfig.Peers = 1024
	modelConfig.RoutingModelIdentity = hex.EncodeToString(modelIdentity[:])
	a.AuditLogicalNodes = 1024
	a.AuditRoutingModelIdentity = modelConfig.RoutingModelIdentity
	logical := func(m contract.AuditMetrics) []uint64 {
		return []uint64{m.RequestedRecords, m.ReturnedRecords, m.NativeQueries, m.AncestorGroups, m.AncestorPathRecomputations, m.AncestorReuseHits, m.ProofBytes, m.EvidenceBytes}
	}
	for range 2 {
		modeled, err := cassandradht.New(ctx, modeledFixtureRoutes{proof, 1024}, native.CA, modelConfig)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.Audit(ctx, c, request, modeled, 96)
		if err != nil || !reflect.DeepEqual(logical(got), logical(baseline)) {
			t.Fatal("routing model changed logical audit", err)
		}
		if err := contract.ValidateLogicalDHTMetrics(got, 1024); err != nil {
			t.Fatal(err)
		}
		if a.ConfigHash() != oldHash || a.ProofIdentity(c.StreamID, c.Fingerprint) != nativeIdentity || snapshot() != before || proofSnapshot() != proofBefore {
			t.Fatal("audit routing rewrote native state/identity/proofs")
		}
		// Reopen with empty caches and the original durable ingestion config.
		reopened, err := contract.OpenPostgres(ctx, dsn, 6)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		a, err = OpenIncrementalDHT(reopened, a.Options)
		if err != nil {
			t.Fatal(err)
		}
		a.AuditLogicalNodes = 1024
		a.AuditRoutingModelIdentity = modelConfig.RoutingModelIdentity
		c.Store.DB = reopened
	}
	if _, err := a.Audit(ctx, c, request, client, 96); err == nil {
		t.Fatal("four-node client silently used for 1024-node experiment")
	}
	for _, missing := range []bool{false, true} {
		bad := badProofFixture{modeledFixtureRoutes{proof, 1024}, dht.KeyR(dht.TreeID(1, producer.LogTypes[0])), missing}
		badClient, err := cassandradht.New(ctx, bad, native.CA, modelConfig)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Audit(ctx, c, request, badClient, 96); err == nil {
			t.Fatal("invalid proof accepted under larger routing model")
		}
	}
	// Changing the native deployment itself remains incompatible; only the
	// explicitly separate audit model can reuse the four-node provenance.
	otherOptions := a.Options
	otherConfig := native
	otherConfig.Peers = 1024
	otherOptions.Cassandra = &otherConfig
	other, err := OpenIncrementalDHT(a.DB, otherOptions)
	if err != nil {
		t.Fatal(err)
	}
	otherConsumer := c
	otherConsumer.ConfigHash = other.ConfigHash()
	if other.ConfigHash() == oldHash {
		t.Fatal("native topology omitted from fingerprint")
	}
	if _, err := otherConsumer.Progress(ctx); err == nil {
		t.Fatal("native topology change silently adopted old cursor")
	}
}
