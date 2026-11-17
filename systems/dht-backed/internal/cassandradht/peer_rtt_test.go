package cassandradht

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	contract "canonicalingestion"
	"dhtbacked/internal/hashing"
)

// Check every hop, rather than just the overall elapsed lookup duration.
type timedRoutes struct {
	*memKV
	t        *testing.T
	rtt      time.Duration
	previous time.Time
}

func (kv *timedRoutes) Route(ctx context.Context, peer, level int) (int, error) {
	kv.t.Helper()
	if elapsed := time.Since(kv.previous); elapsed < kv.rtt {
		kv.t.Fatalf("hop %d waited %s, want at least %s", level, elapsed, kv.rtt)
	}
	next, err := kv.memKV.Route(ctx, peer, level)
	kv.previous = time.Now()
	return next, err
}

func TestPeerRTTEveryHopIncludingCacheHits(t *testing.T) {
	g := newGate(t)
	mem := &memKV{proofs: make(map[hashing.Digest][]byte)}
	req := parentRequests(mem, 1)[0]
	kv := &timedRoutes{memKV: mem, t: t, rtt: contract.DefaultDHTPeerRTT}
	limits := DefaultLimits()
	limits.Workers, limits.CacheBytes, limits.CacheEntries = 2, 1<<20, 8
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 2, PeerRTT: kv.rtt, Limits: &limits})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		kv.previous = time.Now()
		got, err := d.lookup(context.Background(), req.Key)
		if err != nil || !bytes.Equal(got, mem.proofs[req.Key]) {
			t.Fatal(got, err)
		}
	}
	s := d.Stats()
	if s.RoutingReads != 20 || s.DelayedHops != 20 || s.ProofReads != 1 || s.CacheHits != 1 || s.CacheMisses != 1 ||
		s.PeerDelay != 20*kv.rtt || s.ObservedPeerDelay < s.PeerDelay {
		t.Fatalf("delay/cache accounting: %+v", s)
	}
	if err := d.ClearAuditCache(context.Background()); err != nil {
		t.Fatal(err)
	}
	kv.previous = time.Now()
	if _, err := d.lookup(context.Background(), req.Key); err != nil {
		t.Fatal(err)
	}
	if d.Stats().ProofReads != 2 || d.Stats().DelayedHops != 30 {
		t.Fatal("cold run skipped payload or delay")
	}
	d.ResetStats()
	s = d.Stats()
	if s.DelayedHops != 0 || s.PeerDelay != 0 || s.ObservedPeerDelay != 0 {
		t.Fatal("delay counters not reset")
	}
}

func TestPeerRTTCancellationAndDisabled(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: make(map[hashing.Digest][]byte)}
	req := parentRequests(kv, 1)[0]
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 1, PeerRTT: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := d.lookup(ctx, req.Key); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if kv.routes.Load() != 0 || kv.reads.Load() != 0 || d.Stats().DelayedHops != 0 {
		t.Fatal("cancelled wait reached storage")
	}
	for _, peers := range []int{1, 1024} {
		rtt := time.Duration(0)
		if peers == 1 {
			rtt = contract.DefaultDHTPeerRTT
		}
		d, err := New(context.Background(), kv, g.pk, Config{Peers: peers, Concurrency: 1, PeerRTT: rtt})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := d.lookup(context.Background(), req.Key); err != nil {
			t.Fatal(err)
		}
		if s := d.Stats(); s.DelayedHops != 0 || s.ObservedPeerDelay != 0 || s.PeerDelay != 0 {
			t.Fatal("unexpected simulated wait", s)
		}
	}
	for _, rtt := range []time.Duration{-1, time.Second + 1} {
		if _, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 1, PeerRTT: rtt}); err == nil {
			t.Fatal("invalid RTT accepted")
		}
	}
}

func TestPeerRTTPreservesBoundedConcurrentLookups(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: make(map[hashing.Digest][]byte), delay: time.Millisecond}
	reqs := parentRequests(kv, 8)
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 2, PeerRTT: contract.DefaultDHTPeerRTT})
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
	if s := d.Stats(); s.RoutingReads != 80 || s.DelayedHops != 80 || s.ProofReads != 8 {
		t.Fatal(s)
	}
}
