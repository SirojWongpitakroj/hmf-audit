package cassandradht

import (
	"bytes"
	"context"
	"dhtbacked/internal/hashing"
	"errors"
	"fmt"
	"sync"
	"testing"
)

type routingFixture struct {
	mu                       sync.Mutex
	native                   []byte
	routes                   map[[2]int]int
	models                   map[string][]byte
	writes, attempts, failAt int
}

func newRoutingFixture() *routingFixture {
	f := &routingFixture{native: bytes.Repeat([]byte{7}, 32), routes: map[[2]int]int{}, models: map[string][]byte{}}
	for peer := 0; peer < 4; peer++ {
		for level := 0; level < 2; level++ {
			f.routes[[2]int{peer, level}] = RouteNext(peer, level)
		}
	}
	return f
}
func (f *routingFixture) checkProofIdentity(ctx context.Context, id []byte) error {
	if !bytes.Equal(f.native, id) {
		return errors.New("wrong native identity")
	}
	return ctx.Err()
}
func (f *routingFixture) loadRoutingIdentity(ctx context.Context, name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.models[name]
	if !ok {
		return nil, ErrNotFound
	}
	return bytes.Clone(b), ctx.Err()
}
func (f *routingFixture) bindRoutingIdentity(ctx context.Context, name string, b []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if old, ok := f.models[name]; ok && !bytes.Equal(old, b) {
		return errors.New("conflict")
	}
	f.models[name] = bytes.Clone(b)
	return ctx.Err()
}
func (f *routingFixture) ensureRoute(ctx context.Context, peer, level int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.failAt > 0 && f.attempts == f.failAt {
		return errors.New("injected acknowledgement failure")
	}
	key := [2]int{peer, level}
	want := RouteNext(peer, level)
	if old, ok := f.routes[key]; ok {
		if old != want {
			return errors.New("conflicting route")
		}
		return nil
	}
	f.routes[key] = want
	f.writes++
	return ctx.Err()
}
func (f *routingFixture) Route(ctx context.Context, peer, level int) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	got, ok := f.routes[[2]int{peer, level}]
	if !ok {
		return 0, ErrNotFound
	}
	return got, ctx.Err()
}

func TestAuditRoutingExpansionRetryAndIdentitySafety(t *testing.T) {
	f := newRoutingFixture()
	f.failAt = 12
	ctx := context.Background()
	if _, err := prepareAuditRouting(ctx, f, f.native, 1024, 4); err == nil {
		t.Fatal("injected setup failure ignored")
	}
	if len(f.models) != 0 {
		t.Fatal("partial model published")
	}
	f.failAt = 0
	id, err := prepareAuditRouting(ctx, f, f.native, 1024, 4)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.routes) != 1024*10 || len(f.models) != 1 {
		t.Fatal("wrong bounded topology", len(f.routes), len(f.models))
	}
	for peer := 0; peer < 4; peer++ {
		for level := 0; level < 2; level++ {
			if f.routes[[2]int{peer, level}] != RouteNext(peer, level) {
				t.Fatal("old route changed")
			}
		}
	}
	writes := f.writes
	again, err := prepareAuditRouting(ctx, f, f.native, 1024, 8)
	if err != nil || id != again || f.writes != writes {
		t.Fatal("restart rewrote state or changed identity", err)
	}
	small, err := AuditRoutingIdentity(f.native, 4)
	if err != nil || small == id {
		t.Fatal("routing identity omits topology")
	}
	wrong := bytes.Repeat([]byte{8}, 32)
	if _, err := prepareAuditRouting(ctx, f, wrong, 1024, 4); err == nil || f.writes != writes {
		t.Fatal("foreign native state adopted")
	}
	f.models["audit-routing/xor-v1/1024"] = wrong
	if _, err := prepareAuditRouting(ctx, f, f.native, 1024, 4); err == nil || f.writes != writes {
		t.Fatal("conflicting routing marker adopted")
	}
	f.models["audit-routing/xor-v1/1024"] = id[:]
	f.routes[[2]int{0, 9}] = 0
	if _, err := prepareAuditRouting(ctx, f, f.native, 1024, 4); err == nil || f.routes[[2]int{0, 9}] != 0 {
		t.Fatal("corruption silently repaired/adopted")
	}
	for _, nodes := range []int{0, 3, 65537} {
		if _, err := prepareAuditRouting(ctx, f, f.native, nodes, 4); err == nil {
			t.Fatal("bad topology accepted")
		}
	}
}

func TestThousandNodeLookupRoutesOnCacheHitsAndCountsEveryOwner(t *testing.T) {
	g := newGate(t)
	kv := &memKV{proofs: make(map[hashing.Digest][]byte)}
	requests := parentRequests(kv, 32)
	l := DefaultLimits()
	l.Workers = 4
	l.CacheBytes = 64 << 10
	l.CacheEntries = 64
	id, err := AuditRoutingIdentity(bytes.Repeat([]byte{1}, 32), 1024)
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(context.Background(), kv, g.pk, Config{Peers: 1024, Concurrency: 4, Limits: &l, RoutingModelIdentity: fmt.Sprintf("%x", id)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetMany(&g.token, requests); err != nil {
		t.Fatal(err)
	}
	first := d.Stats()
	if first.RoutingReads != 320 || first.ProofReads != 32 || len(first.OwnerLookups) != 1024 {
		t.Fatal("incorrect model counters", first.RoutingReads, first.ProofReads, len(first.OwnerLookups))
	}
	var owners, high uint64
	for owner, n := range first.OwnerLookups {
		owners += n
		if owner > 3 {
			high += n
		}
	}
	if owners != 32 || high == 0 {
		t.Fatal("owner accounting was truncated")
	}
	if _, err := d.GetMany(&g.token, requests); err != nil {
		t.Fatal(err)
	}
	second := d.Stats()
	if second.RoutingReads-first.RoutingReads != 320 || second.ProofReads != first.ProofReads || second.CacheHits-first.CacheHits != 32 {
		t.Fatal("cache bypassed ten-hop routing")
	}
	if second.CacheBytes > uint64(l.CacheBytes) || second.CacheEntries > uint64(l.CacheEntries) {
		t.Fatal("cache grew beyond bounds")
	}
	kv.badRoute = true
	if _, err := d.GetMany(&g.token, requests[:1]); err == nil {
		t.Fatal("corrupt routing accepted despite cached proof")
	}
}
