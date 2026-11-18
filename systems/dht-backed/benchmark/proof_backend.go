package benchmark

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"

	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/peerdht"
	"dhtbacked/verifier"
)

func (o IncrementalOptions) proofLimits() cassandradht.Limits {
	if o.Cassandra != nil {
		return *o.Cassandra.Limits
	}
	l := o.Peers.Limits
	return cassandradht.Limits{Workers: l.Workers, MaxRequests: l.MaxRequests, QueueBytes: l.QueueBytes, MaxValueBytes: l.MaxValueBytes, ProofBytes: l.ProofBytes, AncestorMapBytes: l.AncestorMapBytes, CacheBytes: l.CacheBytes, CacheEntries: l.CacheEntries, RecordFetchBatch: l.RecordFetchBatch, RecordBytes: l.RecordBytes}
}
func (o IncrementalOptions) proofAuthority() ed25519.PublicKey {
	if o.Cassandra != nil {
		return o.Cassandra.CA
	}
	return o.Peers.CA
}
func (o IncrementalOptions) overlay() string {
	if o.Cassandra != nil {
		return o.Cassandra.Overlay
	}
	return o.Peers.Overlay
}

// ProofIdentity binds the durable proof namespace to the exact native run and
// immutable canonical stream. Neither contact-point order nor cache size alters
// key derivation, native IDs, record contents, roots or proof serialization.
func (a *IncrementalDHT) ProofIdentity(stream, fingerprint string) [32]byte {
	raw, _ := json.Marshal([]string{"logical-cassandra-v1", a.ConfigHash(), stream, fingerprint})
	return sha256.Sum256(raw)
}

func RequireBaselineDatabase(ctx context.Context, db *sql.DB) error {
	var name string
	if err := db.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		return err
	}
	if name != "baselines_db" {
		return errors.New("DHT active deployment requires the shared PostgreSQL database baselines_db")
	}
	return nil
}

type proofStats struct {
	lookups, hops, routes, reads, bytes, readBytes uint64
	cacheBytes, cacheEntries, hits, misses         uint64
	owners                                         []uint64
	logicalNodes, routingHops                      int
	routingIdentity                                string
	logical                                        bool
}

func (a *IncrementalDHT) snapshotProof(source verifier.ProofSource) (proofStats, error) {
	var s proofStats
	switch client := source.(type) {
	case *cassandradht.DHT:
		if a.Options.Cassandra == nil {
			return s, errors.New("DHT proof backend differs from durable native configuration")
		}
		expected := a.Options.Cassandra.Peers
		if a.AuditLogicalNodes > 0 {
			expected = a.AuditLogicalNodes
		}
		if client.Peers() != expected || client.RoutingModelIdentity() != a.AuditRoutingModelIdentity {
			return s, errors.New("DHT proof backend differs from durable native configuration")
		}
		r := client.Stats()
		s = proofStats{lookups: uint64(r.Lookups), routes: uint64(r.RoutingReads), reads: uint64(r.ProofReads), bytes: r.ReturnedBytes, readBytes: r.ProofReadBytes, cacheBytes: r.CacheBytes, cacheEntries: r.CacheEntries, hits: r.CacheHits, misses: r.CacheMisses, logical: true}
		s.owners = r.OwnerLookups
		s.logicalNodes, s.routingHops, s.routingIdentity = client.Peers(), client.HopsPerLookup(), client.RoutingModelIdentity()
	case *peerdht.Client:
		if a.Options.Cassandra != nil {
			return s, errors.New("HTTP peers cannot serve a Cassandra-bound native run")
		}
		r := client.Stats()
		s = proofStats{lookups: r.Lookups, hops: r.Hops, reads: r.StorageReads, bytes: r.ProofBytes, cacheBytes: r.CacheBytes, cacheEntries: r.CacheEntries, hits: r.CacheHits, misses: r.CacheMisses}
	default:
		return s, errors.New("unsupported native DHT proof source")
	}
	return s, nil
}
