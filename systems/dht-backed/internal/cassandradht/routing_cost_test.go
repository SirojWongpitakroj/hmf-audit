package cassandradht

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"dhtbacked/internal/hashing"
)

// Every lookup walks ceil(log2 peers) routing reads, including proof-cache hits.
// Nothing is simulated: the cost is the measured cost of those reads.
func TestRoutingReadsEveryLookupIncludingCacheHits(t *testing.T) {
	g := newGate(t)
	mem := &memKV{proofs: make(map[hashing.Digest][]byte)}
	req := parentRequests(mem, 1)[0]
	limits := DefaultLimits()
	limits.Workers, limits.CacheBytes, limits.CacheEntries = 2, 1<<20, 8
	d, err := New(context.Background(), mem, g.pk, Config{Peers: 1024, Concurrency: 2, Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		got, err := d.lookup(context.Background(), req.Key)
		if err != nil || !bytes.Equal(got, mem.proofs[req.Key]) {
			t.Fatal(got, err)
		}
	}
	s := d.Stats()
	if s.RoutingReads != 20 || s.ProofReads != 1 || s.CacheHits != 1 || s.CacheMisses != 1 {
		t.Fatalf("routing/cache accounting: %+v", s)
	}
	if err := d.ClearAuditCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := d.lookup(context.Background(), req.Key); err != nil {
		t.Fatal(err)
	}
	if s := d.Stats(); s.ProofReads != 2 || s.RoutingReads != 30 {
		t.Fatal("cold run skipped payload or routing", s)
	}
	d.ResetStats()
	if s := d.Stats(); s.RoutingReads != 0 || s.ProofReads != 0 {
		t.Fatal("counters not reset", s)
	}
}

func TestLookupCancellationDoesNotReachStorage(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: make(map[hashing.Digest][]byte)}
	req := parentRequests(kv, 1)[0]
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := d.lookup(ctx, req.Key); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if kv.routes.Load() != 0 || kv.reads.Load() != 0 {
		t.Fatal("cancelled lookup reached storage")
	}
}

func TestSinglePeerHasNoRoutingReads(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: make(map[hashing.Digest][]byte)}
	req := parentRequests(kv, 1)[0]
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.lookup(context.Background(), req.Key); err != nil {
		t.Fatal(err)
	}
	if s := d.Stats(); s.RoutingReads != 0 || s.ProofReads != 1 {
		t.Fatal(s)
	}
}

func TestRoutingPreservesBoundedConcurrentLookups(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: make(map[hashing.Digest][]byte), delay: time.Millisecond}
	reqs := parentRequests(kv, 8)
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.GetMany(&g.token, reqs)
	if err != nil || len(got) != len(reqs) {
		t.Fatal(len(got), err)
	}
	if peak := kv.maxFlight.Load(); peak != 2 {
		t.Fatal("lookup concurrency changed", peak)
	}
	if s := d.Stats(); s.RoutingReads != 80 || s.ProofReads != 8 {
		t.Fatal(s)
	}
}
