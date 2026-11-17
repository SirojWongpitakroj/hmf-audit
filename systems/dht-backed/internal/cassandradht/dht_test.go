package cassandradht

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/clock"
	"dhtbacked/internal/hashing"
	"dhtbacked/verifier"
)

// memKV is an in-memory KV that counts reads and the most reads in flight at
// once. Every read waits for delay, which lets a test see whether lookups
// overlap.
type memKV struct {
	peers  int
	delay  time.Duration
	proofs map[hashing.Digest][]byte

	mu        sync.Mutex
	routeErr  error
	badRoute  bool
	routes    atomic.Int64
	reads     atomic.Int64
	inFlight  atomic.Int64
	maxFlight atomic.Int64
}

func (m *memKV) enter() func() {
	now := m.inFlight.Add(1)
	for {
		peak := m.maxFlight.Load()
		if now <= peak || m.maxFlight.CompareAndSwap(peak, now) {
			break
		}
	}
	time.Sleep(m.delay)
	return func() { m.inFlight.Add(-1) }
}

func (m *memKV) Proof(ctx context.Context, key hashing.Digest) ([]byte, error) {
	defer m.enter()()
	m.reads.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.proofs[key]
	if !ok {
		return nil, ErrNotFound
	}
	return v, nil
}

func (m *memKV) Route(ctx context.Context, peer, level int) (int, error) {
	defer m.enter()()
	m.routes.Add(1)
	if m.routeErr != nil {
		return 0, m.routeErr
	}
	if m.badRoute {
		return peer, nil
	}
	return RouteNext(peer, level), nil
}

type gateFixture struct {
	ca    *auth.CA
	pk    ed25519.PublicKey
	token auth.Token
}

func newGate(t *testing.T) gateFixture {
	t.Helper()
	pk, sk, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := auth.NewCA(sk, clock.System{})
	if err != nil {
		t.Fatal(err)
	}
	return gateFixture{ca: ca, pk: pk, token: ca.Issue("auditor", 1, []string{"access", "api"})}
}

// parentRequests builds n parent-node requests and stores a payload for each.
func parentRequests(kv *memKV, n int) []verifier.ProofRequest {
	treeID := dht.TreeID(1, "access")
	reqs := make([]verifier.ProofRequest, 0, n)
	for k := 0; k < n; k++ {
		key := dht.KeyP(treeID, 10, uint64(k))
		kv.proofs[key] = []byte(fmt.Sprintf("payload-%d", k))
		reqs = append(reqs, verifier.ProofRequest{
			Kind: verifier.ProofParent, Epoch: 1, LogType: "access", A: 10, B: uint64(k), Key: key,
		})
	}
	return reqs
}

func newDHT(t *testing.T, kv *memKV, g gateFixture, peers, concurrency int) *DHT {
	t.Helper()
	kv.peers = peers
	d, err := New(context.Background(), kv, g.pk, Config{Peers: peers, Concurrency: concurrency})
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestHopsIsCeilLog2(t *testing.T) {
	for peers, want := range map[int]int{1: 0, 2: 1, 3: 2, 4: 2, 5: 3, 16: 4, 17: 5, 1024: 10, 1025: 11} {
		if got := Hops(peers); got != want {
			t.Errorf("Hops(%d) = %d, want %d", peers, got, want)
		}
	}
}

func TestEveryLookupCostsExactlyLogNRoutingReads(t *testing.T) {
	g := newGate(t)
	for _, peers := range []int{1, 2, 16, 100, 1024} {
		kv := &memKV{proofs: map[hashing.Digest][]byte{}}
		d := newDHT(t, kv, g, peers, 4)
		reqs := parentRequests(kv, 50)
		got, err := d.GetMany(&g.token, reqs)
		if err != nil {
			t.Fatalf("peers=%d: %v", peers, err)
		}
		if len(got) != len(reqs) {
			t.Fatalf("peers=%d: %d records back, want %d", peers, len(got), len(reqs))
		}
		st := d.Stats()
		if st.Lookups != 50 || st.ProofReads != 50 {
			t.Fatalf("peers=%d: lookups=%d proofReads=%d, want 50 each", peers, st.Lookups, st.ProofReads)
		}
		if want := int64(50 * Hops(peers)); st.RoutingReads != want || kv.routes.Load() != want {
			t.Fatalf("peers=%d: routing reads %d (kv saw %d), want %d",
				peers, st.RoutingReads, kv.routes.Load(), want)
		}
	}
}

func TestGetManyReturnsEachDistinctKeyOnce(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}}
	d := newDHT(t, kv, g, 16, 8)
	reqs := parentRequests(kv, 20)
	reqs = append(reqs, reqs[:10]...) // ten repeated requests
	got, err := d.GetMany(&g.token, reqs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 20 || d.Stats().Lookups != 20 {
		t.Fatalf("%d records and %d lookups, want 20 and 20", len(got), d.Stats().Lookups)
	}
	for _, r := range reqs {
		if string(got[r.Key]) != fmt.Sprintf("payload-%d", r.B) {
			t.Fatalf("key for k=%d returned %q", r.B, got[r.Key])
		}
	}
}

func TestConcurrencyIsBoundedAndActuallyOverlaps(t *testing.T) {
	g := newGate(t)
	for _, limit := range []int{1, 4, 8} {
		kv := &memKV{proofs: map[hashing.Digest][]byte{}, delay: 2 * time.Millisecond}
		d := newDHT(t, kv, g, 16, limit)
		if _, err := d.GetMany(&g.token, parentRequests(kv, 64)); err != nil {
			t.Fatal(err)
		}
		if peak := kv.maxFlight.Load(); peak > int64(limit) {
			t.Fatalf("limit %d: %d reads in flight at once", limit, peak)
		}
		if limit > 1 && kv.maxFlight.Load() < 2 {
			t.Fatalf("limit %d: lookups never overlapped", limit)
		}
	}
}

func TestConcurrentLookupsAreFasterThanSequential(t *testing.T) {
	g := newGate(t)
	run := func(limit int) time.Duration {
		kv := &memKV{proofs: map[hashing.Digest][]byte{}, delay: time.Millisecond}
		d := newDHT(t, kv, g, 16, limit)
		reqs := parentRequests(kv, 40)
		start := time.Now()
		if _, err := d.GetMany(&g.token, reqs); err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}
	sequential, concurrent := run(1), run(8)
	if concurrent > sequential/2 {
		t.Fatalf("8 workers took %v, 1 worker %v: no speed-up", concurrent, sequential)
	}
}

func TestMissingRecordFailsTheBatch(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}}
	d := newDHT(t, kv, g, 16, 4)
	reqs := parentRequests(kv, 10)
	delete(kv.proofs, reqs[7].Key)
	if _, err := d.GetMany(&g.token, reqs); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing record returned %v, want ErrNotFound", err)
	}
}

func TestRoutingTableThatDoesNotReachThePeerFails(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}, badRoute: true}
	d := newDHT(t, kv, g, 16, 4)
	if _, err := d.GetMany(&g.token, parentRequests(kv, 10)); err == nil {
		t.Fatal("a routing table that never reaches the responsible peer was accepted")
	}
}

func TestRoutingReadFailureFailsTheBatch(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}, routeErr: errors.New("node down")}
	d := newDHT(t, kv, g, 16, 4)
	if _, err := d.GetMany(&g.token, parentRequests(kv, 10)); err == nil {
		t.Fatal("a failed routing read was ignored")
	}
}

func TestGateRejectsBadTokenAndMismatchedKey(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}}
	d := newDHT(t, kv, g, 16, 4)
	reqs := parentRequests(kv, 4)

	forged := g.token
	forged.Signature = append([]byte(nil), forged.Signature...)
	forged.Signature[0] ^= 0xFF
	if _, err := d.GetMany(&forged, reqs); err == nil {
		t.Error("a token with a forged signature was served")
	}

	other := g.ca.Issue("auditor", 2, []string{"access"})
	if _, err := d.GetMany(&other, reqs); err == nil {
		t.Error("a token for another epoch was served")
	}

	bad := append([]verifier.ProofRequest(nil), reqs...)
	bad[2].B++ // envelope no longer derives the supplied key
	if _, err := d.GetMany(&g.token, bad); !errors.Is(err, dht.ErrKeyMismatch) {
		t.Errorf("mismatched key returned %v, want ErrKeyMismatch", err)
	}
	if kv.reads.Load() != 0 {
		t.Errorf("%d value reads happened for rejected requests", kv.reads.Load())
	}
}

func TestSingleLookupsUseTheSameRoute(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: map[hashing.Digest][]byte{}}
	d := newDHT(t, kv, g, 16, 4)
	req := parentRequests(kv, 1)[0]
	raw, err := d.GetParent(&g.token, dht.ParentRequest{Epoch: 1, LogType: "access", J: req.A, K: req.B, Key: req.Key})
	if err != nil || string(raw) != "payload-0" {
		t.Fatalf("GetParent = %q, %v", raw, err)
	}
	if st := d.Stats(); st.RoutingReads != 4 || st.ProofReads != 1 {
		t.Fatalf("stats %+v, want 4 routing reads and 1 value read", st)
	}
	d.ResetStats()
	if st := d.Stats(); !reflect.DeepEqual(st, Stats{OwnerLookups: make([]uint64, 16)}) {
		t.Fatalf("stats after reset %+v", st)
	}
	if _, err := d.GetParent(&g.token, dht.ParentRequest{Epoch: 1, LogType: "access", J: 10, K: 999,
		Key: dht.KeyP(dht.TreeID(1, "access"), 10, 999)}); !errors.Is(err, dht.ErrNotFound) {
		t.Fatalf("absent record returned %v, want dht.ErrNotFound", err)
	}
}

func TestNewRejectsBadConfig(t *testing.T) {
	g := newGate(t)
	kv := &memKV{}
	for _, cfg := range []Config{{Peers: 0, Concurrency: 1}, {Peers: 16, Concurrency: 0}, {Peers: -1, Concurrency: 1}} {
		if _, err := New(context.Background(), kv, g.pk, cfg); err == nil {
			t.Errorf("config %+v was accepted", cfg)
		}
	}
	if _, err := New(context.Background(), nil, g.pk, Config{Peers: 1, Concurrency: 1}); err == nil {
		t.Error("nil storage was accepted")
	}
}

func TestPeerOfIsStableAndInRange(t *testing.T) {
	key := dht.KeyR(dht.TreeID(1, "access"))
	if PeerOf(key, 16) != dht.NodeIndex(key) {
		t.Fatal("PeerOf(key, 16) disagrees with the Q10 node assignment")
	}
	for _, peers := range []int{1, 3, 100} {
		if p := PeerOf(key, peers); p < 0 || p >= peers {
			t.Fatalf("PeerOf(key, %d) = %d", peers, p)
		}
	}
}
