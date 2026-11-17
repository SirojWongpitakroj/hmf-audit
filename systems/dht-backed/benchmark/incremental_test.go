package benchmark

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	contract "canonicalingestion"
	"dhtbacked/auth"
	"dhtbacked/blockchain"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/internal/peerdht"
	"dhtbacked/mktree"
	"dhtbacked/producer"
	"dhtbacked/storage"
	"dhtbacked/verifier"
)

func TestIncrementalDHTOutboxAndIdentity(t *testing.T) {
	dsn := os.Getenv("DHT_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("DHT_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	base, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	var names, dsns []string
	for i := 0; i < 5; i++ {
		name := fmt.Sprintf("dht_ingest_test_%d_%d", time.Now().UnixNano(), i)
		if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
		u := *base
		u.Path = "/" + name
		dsns = append(dsns, u.String())
	}
	defer func() {
		for _, name := range names {
			admin.Exec(`DROP DATABASE "` + name + `" WITH (FORCE)`)
		}
	}()
	db, err := contract.OpenPostgres(ctx, dsns[4], 8)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	keyDir := filepath.Join(t.TempDir(), "keys")
	if err = PrepareDHTKeys(keyDir); err != nil {
		t.Fatal(err)
	}
	priv, pub, err := auth.LoadOrCreateCAKeys(filepath.Join(keyDir, "ca.sk"), filepath.Join(keyDir, "ca.pk"))
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := auth.NewCA(priv, clock.System{})
	var servers []*httptest.Server
	var endpoints []string
	for i := 0; i < 4; i++ {
		server := httptest.NewUnstartedServer(nil)
		servers = append(servers, server)
		endpoints = append(endpoints, "http://"+server.Listener.Addr().String())
	}
	defer func() {
		for _, server := range servers {
			server.Close()
		}
	}()
	limits := peerdht.Limits{Workers: 2, MaxRequests: 1000, QueueBytes: 4096, MaxValueBytes: 4096, MaxRequestBytes: 16384, ProofBytes: 1 << 20, AncestorMapBytes: 2 << 20, CacheBytes: 2048, CacheEntries: 4, RecordFetchBatch: 3, RecordBytes: 1 << 20}
	cfg := peerdht.Config{Node: 0, Overlay: "outbox-test", Peers: endpoints, CA: pub, WriteSecret: "isolated-test-only", Limits: limits, Timeout: 2 * time.Second}
	for i, server := range servers {
		localDB, e := contract.OpenPostgres(ctx, dsns[i], 4)
		if e != nil {
			t.Fatal(e)
		}
		defer localDB.Close()
		peerCfg := cfg
		peerCfg.Node = i
		peer, e := peerdht.NewPeer(ctx, peerCfg, &peerdht.LocalStore{DB: localDB, Node: i, Overlay: cfg.Overlay, MaxValueBytes: 4096})
		if e != nil {
			t.Fatal(e)
		}
		server.Config.Handler = peer
		server.Start()
	}
	options := IncrementalOptions{RunID: "test", KeyDir: keyDir, WorkDir: t.TempDir(), EpochCapacity: 8, MaxRecordBytes: 4096, Peers: cfg}
	a, err := OpenIncrementalDHT(db, options)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	source := contract.Store{DB: db}
	if err = source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err = source.Create(ctx, "test", "fixture-v1"); err != nil {
		t.Fatal(err)
	}
	var entries []contract.Entry
	for i := 1; i <= 43; i++ {
		ds := []string{"WADI", "HAI", "SWAT"}[i%3]
		sourceID := contract.SourceObservation{Dataset: ds, File: ds + "/raw/test.csv", Row: uint64(i), Column: 2}
		b, _ := json.Marshal(struct {
			CanonicalObservation
			Source contract.SourceObservation `json:"source"`
		}{CanonicalObservation{Dataset: ds, Site: ds + "/site", Device: fmt.Sprintf("sensor%d", i%5), Type: "sensor", Timestamp: time.Unix(int64(i), 0).UTC(), Value: fmt.Sprint(i), Label: "normal"}, sourceID})
		entries = append(entries, contract.Entry{Ordinal: uint64(i), SourceID: sourceID.ID(), Record: b})
	}
	batch := contract.Limits{MaxRecords: 100, MaxBatchBytes: 1 << 20, MaxRecordBytes: 4096}
	if err = source.Append(ctx, "test", entries, batch); err != nil {
		t.Fatal(err)
	}
	c := contract.Consumer{Store: source, ID: "dht/test", StreamID: "test", Fingerprint: "fixture-v1", ConfigHash: a.ConfigHash(), DeferCompletion: true}
	if err = c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Begin(ctx, 1000000); err != nil {
		t.Fatal(err)
	}
	// Small internal targets exercise identical mechanics without a million
	// fixture records. Production Begin accepts only the five manual milestones.
	db.Exec(`UPDATE canonical_ingestion.consumers SET target=17 WHERE consumer_id='dht/test'`)
	batch.MaxRecords = 8
	p, err := c.Step(ctx, source, batch, a.Apply)
	if err != nil || p.Committed != 8 {
		t.Fatal(p, err)
	}
	var encryptedBefore []byte
	if err = db.QueryRow(`SELECT c FROM dhtbacked.records ORDER BY record_id LIMIT 1`).Scan(&encryptedBefore); err != nil {
		t.Fatal(err)
	}
	bad := cfg
	bad.Peers = append([]string(nil), cfg.Peers...)
	bad.Peers[0] = "http://127.0.0.1:1"
	unavailable, _ := peerdht.NewClient(ctx, bad)
	if err = a.PublishPending(ctx, peerdht.NewPublisher(unavailable)); err == nil {
		t.Fatal("unavailable peer did not block authentication publication")
	}
	unavailable.Close()
	var anchored int
	db.QueryRow(`SELECT count(*) FROM dhtbacked.dht_epoch_anchors`).Scan(&anchored)
	if anchored != 0 {
		t.Fatal("anchor published before peer durability")
	}
	// Reopen the ingestion process state and replay the durable publication plan.
	a, err = OpenIncrementalDHT(db, options)
	if err != nil {
		t.Fatal(err)
	}
	client, err := peerdht.NewClient(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err = a.PublishPending(ctx, peerdht.NewPublisher(client)); err != nil {
		t.Fatal(err)
	}
	for _, target := range []uint64{17, 43} {
		db.Exec(`UPDATE canonical_ingestion.consumers SET target=$1 WHERE consumer_id='dht/test'`, int64(target))
		for p.Committed < target {
			p, err = c.Step(ctx, source, batch, a.Apply)
			if err != nil {
				t.Fatal(err)
			}
			if err = a.PublishPending(ctx, peerdht.NewPublisher(client)); err != nil {
				t.Fatal(err)
			}
		}
		tx, _ := db.BeginTx(ctx, nil)
		if err = a.PublicationReady(ctx, tx, p); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
		tx.Rollback()
	}
	var encryptedAfter []byte
	db.QueryRow(`SELECT c FROM dhtbacked.records ORDER BY record_id LIMIT 1`).Scan(&encryptedAfter)
	if !bytes.Equal(encryptedBefore, encryptedAfter) {
		t.Fatal("restart re-encrypted an existing native record")
	}
	// Decrypt only in this producer-side test to prove every ordinal maps to the
	// exact canonical payload; the native auditor never decrypts.
	key, _ := os.ReadFile(filepath.Join(keyDir, "aead.key"))
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	rows, err := db.Query(`SELECT m.ordinal,m.source_id,r.c,r.t,r.ad FROM canonical_ingestion.mappings m JOIN dhtbacked.records r ON r.record_id=m.native_id WHERE consumer_id='dht/test' ORDER BY ordinal`)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for rows.Next() {
		var ordinal uint64
		var sourceID string
		var cBytes, tag, ad []byte
		if err = rows.Scan(&ordinal, &sourceID, &cBytes, &tag, &ad); err != nil {
			t.Fatal(err)
		}
		sealed := append(append([]byte(nil), cBytes[12:]...), tag...)
		plain, e := gcm.Open(nil, cBytes[:12], sealed, ad)
		if e != nil {
			t.Fatal(e)
		}
		var message struct {
			Message string `json:"message"`
		}
		if e = json.Unmarshal(plain, &message); e != nil || message.Message != string(entries[ordinal-1].Record) || sourceID != entries[ordinal-1].SourceID {
			t.Fatal("logical records differ", ordinal, e)
		}
		count++
	}
	rows.Close()
	if count != 43 {
		t.Fatal("mapping count", count)
	}
	metrics, e := a.Audit(ctx, c, contract.AuditRequest{Version: 1, StreamID: "test", Fingerprint: "fixture-v1", ExpectedN: 43, Entries: entries[:6]}, client, 100)
	if e != nil || metrics.RequestedRecords != 6 || metrics.EvidenceBytes == 0 || metrics.StorageReads == 0 || metrics.DHTLookups == 0 {
		t.Fatal("complete measured DHT audit", metrics, e)
	}
	// Verify the exact published roots against the dense native MK-Tree oracle.
	anchor := &blockchain.SQLAnchor{DB: db}
	recordStore := &storage.SQLStore{DB: db, MaxRecords: 100, BatchRows: 3, MaxBytes: 1 << 20}
	for epoch := uint64(1); ; epoch++ {
		capacity, _, e := anchor.Geometry(ctx, epoch)
		if e != nil {
			break
		}
		geometry := epochConfig(capacity)
		var roots []hashing.Digest
		var ids []string
		for _, typ := range producer.LogTypes {
			tree, e := mktree.NewTree(epoch, typ, capacity, geometry.Stride)
			if e != nil {
				t.Fatal(e)
			}
			rows, e := db.Query(`SELECT record_id,d FROM dhtbacked.records WHERE epoch=$1 AND log_type=$2 ORDER BY leaf_index`, int64(epoch), typ)
			if e != nil {
				t.Fatal(e)
			}
			for rows.Next() {
				var id string
				var raw []byte
				rows.Scan(&id, &raw)
				var digest hashing.Digest
				copy(digest[:], raw)
				tree.Insert(digest)
				ids = append(ids, id)
			}
			rows.Close()
			roots = append(roots, tree.Root())
		}
		got, e := anchor.GetRoot(ctx, epoch)
		if e != nil || got != mktree.RootOfRoots(roots) {
			t.Fatal("native root changed", epoch, e)
		}
		token := ca.Issue("test-auditor", epoch, geometry.LogTypes)
		v, e := verifier.NewBounded(recordStore, client, anchor, geometry, verifier.RequestBounds{Records: 100, ProofRequests: 1000, MapBytes: 2 << 20})
		if e != nil {
			t.Fatal(e)
		}
		result, e := v.VerifyBatch(ctx, &token, ids)
		if e != nil || !result.Accepted {
			t.Fatal("native proof verification changed", epoch, result, e)
		}
	}
}
