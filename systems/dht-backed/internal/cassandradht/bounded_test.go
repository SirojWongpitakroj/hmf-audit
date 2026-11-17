package cassandradht

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/verifier"
)

func boundedFixture(t *testing.T) (*DHT, *memKV, gateFixture, []verifier.ProofRequest) {
	t.Helper()
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}}
	requests := parentRequests(kv, 100)
	l := DefaultLimits()
	l.Workers = 2
	l.QueueBytes = 64
	l.CacheBytes = 1024
	l.CacheEntries = 3
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 4, Concurrency: 2, Limits: &l})
	if err != nil {
		t.Fatal(err)
	}
	return d, kv, g, requests
}
func TestBoundedCacheOwnershipAndRestart(t *testing.T) {
	d, kv, g, requests := boundedFixture(t)
	if _, err := d.GetMany(&g.token, requests[:2]); err != nil {
		t.Fatal(err)
	}
	first := d.Stats()
	if _, err := d.GetMany(&g.token, requests[:2]); err != nil {
		t.Fatal(err)
	}
	second := d.Stats()
	if second.ProofReads != first.ProofReads || second.CacheHits-first.CacheHits != 2 || second.RoutingReads-first.RoutingReads != 4 {
		t.Fatal("warm proof reuse bypassed logical routing or missed cache", first, second)
	}
	if _, err := d.GetMany(&g.token, requests); err != nil {
		t.Fatal(err)
	}
	s := d.Stats()
	if s.CacheBytes > 1024 || s.CacheEntries > 3 || len(d.cache.items) > 3 {
		t.Fatal("cache exceeds explicit limits", s)
	}
	for owner, n := range s.OwnerLookups {
		if n == 0 {
			t.Fatal("fixture did not exercise logical owner", owner)
		}
	}
	l := d.limits
	fresh, err := New(context.Background(), kv, g.pk, Config{Peers: 4, Concurrency: 2, Limits: &l})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Stats().CacheEntries != 0 {
		t.Fatal("restart preloaded proof keyspace")
	}
	for _, r := range requests {
		if PeerOf(r.Key, 4) != PeerOf(r.Key, fresh.Peers()) {
			t.Fatal("owner changed across restart")
		}
	}
	bad := g.token
	bad.Signature = append([]byte(nil), bad.Signature...)
	bad.Signature[0] ^= 1
	if _, err = d.GetMany(&bad, requests[:1]); err == nil {
		t.Fatal("warm cache bypassed authorization")
	}
}
func TestProofRequestValueAndByteBounds(t *testing.T) {
	d, kv, g, requests := boundedFixture(t)
	d.limits.MaxRequests = 1
	if _, err := d.GetMany(&g.token, requests); err == nil {
		t.Fatal("request bound ignored")
	}
	d.limits.MaxRequests = 1000
	d.limits.ProofBytes = 512
	if _, err := d.GetMany(&g.token, requests); err == nil {
		t.Fatal("metadata byte bound ignored")
	}
	d.limits.ProofBytes = 8 << 20
	kv.proofs[requests[0].Key] = bytes.Repeat([]byte{1}, d.limits.MaxValueBytes+1)
	if _, err := d.GetMany(&g.token, requests[:1]); err == nil {
		t.Fatal("oversized persisted proof accepted")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	d.ctx = cancelled
	if _, err := d.GetMany(&g.token, requests[1:2]); err == nil {
		t.Fatal("cancelled request accepted")
	}
}

type immutableFixture struct {
	mu     sync.Mutex
	values map[hashing.Digest][]byte
	fail   bool
}

func (s *immutableFixture) PutProof(ctx context.Context, k hashing.Digest, b []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("injected storage failure")
	}
	if old, ok := s.values[k]; ok && !bytes.Equal(old, b) {
		return errors.New("conflicting replay")
	}
	s.values[k] = append([]byte(nil), b...)
	return ctx.Err()
}
func TestPublisherBoundsImmutableReplayAndAcknowledgement(t *testing.T) {
	l := DefaultLimits()
	l.Workers = 2
	l.QueueBytes = 1024
	store := &immutableFixture{values: map[hashing.Digest][]byte{}}
	p, err := NewPublisher(context.Background(), store, l)
	if err != nil {
		t.Fatal(err)
	}
	for i := uint64(0); i < 100; i++ {
		if err = p.Put(dht.KeyM(dht.TreeID(1, "access"), i), []byte{1}); err != nil {
			t.Fatal(err)
		}
		p.mu.Lock()
		outstandingBytes := p.bytes
		p.mu.Unlock()
		if outstandingBytes > l.QueueBytes {
			t.Fatal("publication spool exceeded byte bound")
		}
	}
	if err = p.Flush(); err != nil || len(store.values) != 100 {
		t.Fatal("publication was not fully acknowledged", err)
	}
	key := dht.KeyM(dht.TreeID(1, "access"), 0)
	if err = p.Put(key, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err = p.Flush(); err != nil {
		t.Fatal("identical replay failed", err)
	}
	if err = p.Put(key, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if err = p.Flush(); err == nil {
		t.Fatal("conflicting sealed replay accepted")
	}
	store.fail = true
	p, _ = NewPublisher(context.Background(), store, l)
	p.Put(key, []byte{1})
	if err = p.Flush(); err == nil {
		t.Fatal("failure falsely acknowledged")
	}
}

// Opt-in integration against a real Cassandra service. The fixture owns only
// its unique test keyspace; it never truncates an existing production store.
func TestPersistentCassandraRestartAndImmutableProofs(t *testing.T) {
	hosts := os.Getenv("DHT_TEST_CASSANDRA_HOSTS")
	if hosts == "" {
		t.Skip("DHT_TEST_CASSANDRA_HOSTS unset: real Cassandra restart validation not executed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	l := DefaultLimits()
	d := Deployment{LogicalNodes: 4, Overlay: "cassandra-test", Hosts: strings.Split(hosts, ","), Port: 9042, Keyspace: fmt.Sprintf("dht_test_%d", time.Now().UnixNano()), ReplicationFactor: 1, TimeoutSeconds: 10, Limits: l}
	store, err := OpenPersistent(ctx, d, true)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if store != nil {
			store.session.Query("DROP KEYSPACE " + d.Keyspace).ExecContext(context.Background())
			store.Close()
		}
	}()
	id := hashing.H([]byte(d.Keyspace))
	if err = store.Bind(ctx, id[:], true); err != nil {
		t.Fatal(err)
	}
	if err = store.EnsureRoutes(ctx, 4); err != nil {
		t.Fatal(err)
	}
	key := dht.KeyR(dht.TreeID(1, "access"))
	value := []byte("persistent-proof-fixture")
	if err = store.PutProof(ctx, key, value); err != nil {
		t.Fatal(err)
	}
	if err = store.PutProof(ctx, key, value); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenPersistent(ctx, d, false)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Bind(ctx, id[:], false); err != nil {
		t.Fatal(err)
	}
	if err = store.CheckRoutes(ctx, 4); err != nil {
		t.Fatal(err)
	}
	got, err := store.Proof(ctx, key)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatal("restart lost proof", err)
	}
	if err = store.PutProof(ctx, key, []byte("different")); err == nil {
		t.Fatal("conflicting proof replay accepted")
	}
	if err = store.Reset(ctx); err == nil {
		t.Fatal("active proof history allowed legacy truncation")
	}
}
