package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
	"dhtbacked/internal/cassandradht"
	gocql "github.com/apache/cassandra-gocql-driver/v2"
)

func scaleProofBackend(t *testing.T, a *IncrementalDHT, cfg cassandradht.Config, keys string) (fixtureProofStore, func(), string) {
	t.Helper()
	if os.Getenv("CONCURRENT_SCALE_CASSANDRA") != "1" {
		proof, close := fixtureProof(t, cfg, a.ProofIdentity("scale-fixture", "v1"), t.TempDir(), "")
		return proof, close, "DHT-backed (disk proof fixture)"
	}
	host := os.Getenv("CONCURRENT_TEST_CASSANDRA_HOSTS")
	if host != "127.0.0.1" && host != "localhost" {
		t.Fatal("Cassandra scale fixture requires an explicit loopback host")
	}
	d := cassandradht.Deployment{LogicalNodes: 4, Overlay: cfg.Overlay, CAFile: filepath.Join(keys, "ca.pk"), Hosts: []string{host}, Port: 9042, Keyspace: fmt.Sprintf("dht_scale_%d", time.Now().UnixNano()), ReplicationFactor: 1, LocalDC: "datacenter1", TimeoutSeconds: 30, Limits: *cfg.Limits}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deployment.json")
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	proof, closeProof := fixtureProof(t, cfg, a.ProofIdentity("scale-fixture", "v1"), t.TempDir(), path)
	return proof, func() {
		closeProof()
		cluster := gocql.NewCluster(host)
		admin, err := cluster.CreateSession()
		if err != nil {
			t.Error(err)
			return
		}
		defer admin.Close()
		if err := admin.Query(`DROP KEYSPACE ` + d.Keyspace).Exec(); err != nil {
			t.Error(err)
		}
	}, "DHT-backed (local Cassandra)"
}

func TestConcurrentDHTScale(t *testing.T) {
	n := testfixture.ScaleRecords(t)
	keys := t.TempDir()
	if err := PrepareDHTKeys(keys); err != nil {
		t.Fatal(err)
	}
	for _, mode := range testfixture.Modes() {
		t.Run(mode, func(t *testing.T) {
			db, dsn := testfixture.Database(t, "DHT_TEST_POSTGRES_DSN")
			e := testfixture.Execution(t)
			m := &contract.IngestMetrics{}
			ctx := contract.WithIngestMetrics(context.Background(), m)
			db.Close()
			db, err := contract.OpenPostgres(ctx, dsn, e.SQLPoolSize)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			a, cfg := logicalOptions(t, db, keys, t.TempDir())
			a.Execution = nil
			cfg.Limits.RecordFetchBatch = 256
			a.Options.EpochCapacity = 1000
			a.Metrics = m
			if err := a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			proof, closeProof, scheme := scaleProofBackend(t, a, cfg, keys)
			defer closeProof()
			limits := *cfg.Limits
			limits.Workers = 8
			if mode == "new" {
				limits.Workers = e.IOWorkers
			}
			limits.QueueBytes = 65536
			limits.MaxRequests = 32768
			if mode == "new" {
				limits.QueueBytes = e.StorageQueueBytes()
				limits.MaxRequests = min(32768, e.BatchMaxRecords)
			}
			publisher, err := cassandradht.NewPublisher(ctx, proof, limits)
			if err != nil {
				t.Fatal(err)
			}
			publisher.Metrics = m
			fn := a.applySequentialReference
			if mode == "new" {
				a.Execution = &e
				fn = a.Apply
			}
			p := contract.Progress{Target: uint64(n)}
			testfixture.Measure(t, scheme, mode, n, e, m, db, func() {
				for p.Committed < uint64(n) {
					count := min(e.BatchMaxRecords, int(uint64(n)-p.Committed))
					entries := testfixture.Entries(p.Committed+1, count)
					tx, err := db.BeginTx(ctx, nil)
					if err != nil {
						t.Fatal(err)
					}
					_, checkpoint, err := fn(ctx, tx, p, entries)
					if err != nil {
						tx.Rollback()
						t.Fatal(err)
					}
					if err := tx.Commit(); err != nil {
						t.Fatal(err)
					}
					p.Committed += uint64(count)
					p.Checkpoint = checkpoint
					if err := a.PublishPending(ctx, publisher); err != nil {
						t.Fatal(err)
					}
					bytes := 0
					for _, entry := range entries {
						bytes += len(entry.Record)
					}
					m.Committed(count, bytes)
				}
			})
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			err = a.PublicationReady(ctx, tx, p)
			tx.Rollback()
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
