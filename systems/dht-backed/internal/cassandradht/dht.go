// Package cassandradht is the persistence-backed, concurrent DHT the fair
// benchmark gives the DHT-backed system.
//
// Proof records (P_j,k, C_s,k, root records) live in Cassandra, keyed by their
// Q45 lookup key, the same way EVSEB keeps its H(LID) -> hLID mapping there.
// Cassandra is not a peer-to-peer overlay, so the O(log n) cost of resolving a
// key in a DHT of n peers is modelled explicitly: every lookup first walks
// ceil(log2 n) routing hops, each a real read of a persisted routing-table row
// in Cassandra, and only then reads the value at the responsible peer. Cassandra
// time is measured; an optional, separately reported simulated peer RTT is added
// at every logical hop by the audit runner.
//
// A verification request hands the whole set of records it needs to GetMany,
// which resolves them with a bounded pool of concurrent lookups.
//
// The native reference verifier retains Q32's single-goroutine model. The
// Experiment 1B execution wrapper may additionally schedule independent leaf
// paths through canonicalingestion.AuditParallel; its ancestor fold stays ordered.
package cassandradht

import (
	"container/list"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/bits"
	"sync"
	"sync/atomic"
	"time"

	"dhtbacked/auth"
	"dhtbacked/dht"
	"dhtbacked/internal/hashing"
	"dhtbacked/verifier"
)

// ErrNotFound reports that no proof record is stored at a key.
var ErrNotFound = errors.New("cassandradht: no record at that key")

// KV is the persistent storage a DHT peer set sits on.
type KV interface {
	// Proof reads the value stored at key, or returns ErrNotFound.
	Proof(ctx context.Context, key hashing.Digest) ([]byte, error)
	// Route reads the routing-table row of a peer at a level: the peer that
	// is one bit-flip away at that level.
	Route(ctx context.Context, peer, level int) (int, error)
}

// Config sets the size of the modelled overlay and the request concurrency.
type Config struct {
	// Audit routing identity is separate from immutable proof/native identity.
	RoutingModelIdentity string
	Overlay              string
	CA                   ed25519.PublicKey
	Limits               *Limits
	// Peers is n, the number of peers in the modelled DHT. A lookup costs
	// ceil(log2 n) routing hops.
	Peers int
	// Concurrency bounds the lookups in flight during one GetMany.
	Concurrency int
	// PeerRTT is an additional simulated network round trip per logical hop.
	// Zero disables simulation. It never replaces measured Cassandra latency.
	PeerRTT time.Duration
}

// Stats counts the storage reads a DHT has served since the last ResetStats.
type Stats struct {
	ReturnedBytes, ProofReadBytes                    uint64
	CacheBytes, CacheEntries, CacheHits, CacheMisses uint64
	OwnerLookups                                     []uint64
	// Lookups is the number of keys resolved.
	Lookups int64
	// RoutingReads is the number of routing-table reads: Lookups * Hops.
	RoutingReads int64
	// ProofReads counts storage value reads on cache misses, including failures.
	ProofReads                   int64
	DelayedHops                  uint64
	PeerDelay, ObservedPeerDelay time.Duration
}

// DHT resolves proof records through the modelled overlay. It is safe for
// concurrent use.
type DHT struct {
	routingModelIdentity          string
	limits                        Limits
	cache                         *boundedCache
	owners                        []atomic.Uint64
	returnedBytes, proofReadBytes atomic.Uint64
	kv                            KV
	ctx                           context.Context
	caPublicKey                   ed25519.PublicKey
	peers                         int
	hops                          int
	workers                       int
	peerRTT                       time.Duration
	delayedHops                   atomic.Uint64
	peerDelay, observedPeerDelay  atomic.Int64

	lookups      atomic.Int64
	routingReads atomic.Int64
	proofReads   atomic.Int64
}

var _ verifier.BatchProofSource = (*DHT)(nil)

// Hops returns the routing hops one lookup takes in a DHT of the given size:
// ceil(log2 peers), and zero for a single peer.
func Hops(peers int) int {
	if peers <= 1 {
		return 0
	}
	return bits.Len(uint(peers - 1))
}

// PeerOf assigns a key to one of n peers: the first eight bytes of SHA256(key)
// read as an unsigned big-endian integer, reduced modulo n. It is the Q10 rule
// with the peer count made a parameter.
func PeerOf(key hashing.Digest, peers int) int {
	h := sha256.Sum256(key[:])
	return int(binary.BigEndian.Uint64(h[:8]) % uint64(peers))
}

// RouteNext is the routing-table entry the store must hold for (peer, level):
// the peer reached by flipping bit level of peer's identifier.
func RouteNext(peer, level int) int { return peer ^ (1 << level) }

// New returns a DHT over kv. ctx bounds every read it issues; caPublicKey is
// the key the access gate checks tokens against (Q4).
func New(ctx context.Context, kv KV, caPublicKey ed25519.PublicKey, cfg Config) (*DHT, error) {
	if kv == nil {
		return nil, errors.New("cassandradht: nil storage")
	}
	if cfg.Peers <= 0 {
		return nil, fmt.Errorf("cassandradht: peer count must be positive, got %d", cfg.Peers)
	}
	if cfg.Concurrency <= 0 {
		return nil, fmt.Errorf("cassandradht: concurrency must be positive, got %d", cfg.Concurrency)
	}
	if cfg.PeerRTT < 0 || cfg.PeerRTT > time.Second {
		return nil, errors.New("cassandradht: peer RTT must be between zero and one second")
	}
	l := DefaultLimits()
	l.Workers = cfg.Concurrency
	if cfg.Limits != nil {
		l = *cfg.Limits
		if l.Workers != cfg.Concurrency {
			return nil, errors.New("cassandradht: concurrency differs from worker bound")
		}
	}
	if err := l.Validate(); err != nil {
		return nil, err
	}
	if cfg.Peers > 65536 {
		return nil, errors.New("cassandradht: logical identity bound exceeded")
	}
	if cfg.RoutingModelIdentity != "" {
		if b, err := hex.DecodeString(cfg.RoutingModelIdentity); err != nil || len(b) != 32 {
			return nil, errors.New("cassandradht: invalid routing model identity")
		}
	}
	return &DHT{
		routingModelIdentity: cfg.RoutingModelIdentity,
		limits:               l, cache: newBoundedCache(l), owners: make([]atomic.Uint64, cfg.Peers),
		kv: kv, ctx: ctx, caPublicKey: caPublicKey,
		peers: cfg.Peers, hops: Hops(cfg.Peers), workers: cfg.Concurrency, peerRTT: cfg.PeerRTT,
	}, nil
}

// Peers, HopsPerLookup and Concurrency report the configuration.
func (d *DHT) Peers() int                   { return d.peers }
func (d *DHT) HopsPerLookup() int           { return d.hops }
func (d *DHT) Concurrency() int             { return d.workers }
func (d *DHT) RoutingModelIdentity() string { return d.routingModelIdentity }
func (d *DHT) PeerRTT() time.Duration       { return d.peerRTT }

// ClearAuditCache must be called between independent audits after workers join.
// It drops proof payloads only, retaining counters and persisted routing rows.
func (d *DHT) ClearAuditCache(context.Context) error {
	d.cache.mu.Lock()
	defer d.cache.mu.Unlock()
	d.cache.items = make(map[hashing.Digest]*list.Element)
	d.cache.order.Init()
	d.cache.bytes = 0
	return nil
}

// ResetStats zeroes the read counters. Call it between requests.
func (d *DHT) ResetStats() {
	d.lookups.Store(0)
	d.routingReads.Store(0)
	d.proofReads.Store(0)
	d.delayedHops.Store(0)
	d.peerDelay.Store(0)
	d.observedPeerDelay.Store(0)
	d.returnedBytes.Store(0)
	d.proofReadBytes.Store(0)
	for i := range d.owners {
		d.owners[i].Store(0)
	}
	d.cache.mu.Lock()
	d.cache.hits = 0
	d.cache.misses = 0
	d.cache.mu.Unlock()
}

// Stats returns the read counters.
func (d *DHT) Stats() Stats {
	s := Stats{
		OwnerLookups:      make([]uint64, len(d.owners)),
		Lookups:           d.lookups.Load(),
		RoutingReads:      d.routingReads.Load(),
		ProofReads:        d.proofReads.Load(),
		DelayedHops:       d.delayedHops.Load(),
		PeerDelay:         time.Duration(d.peerDelay.Load()),
		ObservedPeerDelay: time.Duration(d.observedPeerDelay.Load()),
	}
	s.CacheBytes, s.CacheEntries, s.CacheHits, s.CacheMisses = d.cache.snapshot()
	s.ReturnedBytes = d.returnedBytes.Load()
	s.ProofReadBytes = d.proofReadBytes.Load()
	for i := range d.owners {
		s.OwnerLookups[i] = d.owners[i].Load()
	}
	return s
}

// lookup resolves one key. The walk starts at peer 0 and corrects one bit of
// the peer identifier per hop, most significant first, so it is always exactly
// Hops(n) routing reads long regardless of where the key lives. It fails if
// the persisted routing table does not lead to the responsible peer.
func (d *DHT) lookup(ctx context.Context, key hashing.Digest) ([]byte, error) {
	target := PeerOf(key, d.peers)
	cur := 0
	for level := d.hops - 1; level >= 0; level-- {
		if err := d.waitPeerRTT(ctx); err != nil {
			return nil, err
		}
		next, err := d.kv.Route(ctx, cur, level)
		if err != nil {
			return nil, fmt.Errorf("cassandradht: route (%d,%d): %w", cur, level, err)
		}
		if next != RouteNext(cur, level) {
			return nil, errors.New("cassandradht: conflicting persisted logical route")
		}
		d.routingReads.Add(1)
		if (cur^target)>>level&1 == 1 {
			cur = next
		}
	}
	if cur != target {
		return nil, fmt.Errorf("cassandradht: routing reached peer %d, want %d", cur, target)
	}
	value, hit := d.cache.get(key)
	if !hit {
		var err error
		value, err = d.kv.Proof(ctx, key)
		d.proofReads.Add(1)
		if err != nil {
			return nil, err
		}
		if len(value) > d.limits.MaxValueBytes {
			return nil, errors.New("cassandradht: stored proof exceeds value bound")
		}
		d.proofReadBytes.Add(uint64(len(value)))
		d.cache.put(key, value)
	}
	d.lookups.Add(1)
	d.owners[target].Add(1)
	d.returnedBytes.Add(uint64(len(value)))
	return value, nil
}

// waitPeerRTT models one whole request/response RTT, not a delay in each
// direction. Timers yield the worker; no busy-wait or global serialization.
// Observed time is summed across workers and is not additive wall-clock time.
func (d *DHT) waitPeerRTT(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if d.peerRTT == 0 {
		return nil
	}
	started := time.Now()
	timer := time.NewTimer(d.peerRTT)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		d.observedPeerDelay.Add(int64(time.Since(started)))
		return ctx.Err()
	case <-timer.C:
		d.observedPeerDelay.Add(int64(time.Since(started)))
		if err := ctx.Err(); err != nil {
			return err
		}
		d.delayedHops.Add(1)
		d.peerDelay.Add(int64(d.peerRTT))
		return nil
	}
}

// authorize is the Phase 5(a) gate: the token must verify under pk_CA for the
// requested epoch and log type, and the supplied key must be the one derived
// from the request envelope (Q13, Q41).
func (d *DHT) authorize(tok *auth.Token, epoch uint64, logType string, supplied, expected hashing.Digest) error {
	if err := auth.Verify(d.caPublicKey, tok, time.Now(), epoch, logType); err != nil {
		return err
	}
	if supplied != expected {
		return dht.ErrKeyMismatch
	}
	return nil
}

func (d *DHT) get(tok *auth.Token, epoch uint64, logType string, supplied, expected hashing.Digest) ([]byte, error) {
	if err := d.authorize(tok, epoch, logType, supplied, expected); err != nil {
		return nil, err
	}
	value, err := d.lookup(d.ctx, supplied)
	if errors.Is(err, ErrNotFound) {
		return nil, dht.ErrNotFound
	}
	return value, err
}

// GetParent serves a P_j,k record to an authorized auditor.
func (d *DHT) GetParent(tok *auth.Token, req dht.ParentRequest) ([]byte, error) {
	return d.get(tok, req.Epoch, req.LogType, req.Key,
		dht.KeyP(dht.TreeID(req.Epoch, req.LogType), req.J, req.K))
}

// GetCert serves a C_s,k record to an authorized auditor.
func (d *DHT) GetCert(tok *auth.Token, req dht.CertRequest) ([]byte, error) {
	return d.get(tok, req.Epoch, req.LogType, req.Key,
		dht.KeyC(dht.TreeID(req.Epoch, req.LogType), req.S, req.K))
}

// GetRoot serves a root record to an authorized auditor.
func (d *DHT) GetRoot(tok *auth.Token, req dht.RootRequest) ([]byte, error) {
	return d.get(tok, req.Epoch, req.LogType, req.Key,
		dht.KeyR(dht.TreeID(req.Epoch, req.LogType)))
}

// GetMicroRoot serves a micro-root record to an authorized auditor.
func (d *DHT) GetMicroRoot(tok *auth.Token, req dht.MicroRootRequest) ([]byte, error) {
	return d.get(tok, req.Epoch, req.LogType, req.Key,
		dht.KeyM(dht.TreeID(req.Epoch, req.LogType), req.WindowID))
}

// expectedKey is the lookup key a request's envelope derives (Q41).
func expectedKey(r verifier.ProofRequest) (hashing.Digest, error) {
	treeID := dht.TreeID(r.Epoch, r.LogType)
	switch r.Kind {
	case verifier.ProofParent:
		return dht.KeyP(treeID, r.A, r.B), nil
	case verifier.ProofCert:
		return dht.KeyC(treeID, r.A, r.B), nil
	case verifier.ProofRoot:
		return dht.KeyR(treeID), nil
	}
	return hashing.Digest{}, fmt.Errorf("cassandradht: unknown proof kind %d", r.Kind)
}

// GetMany authorizes every request and then resolves the distinct keys with at
// most Concurrency lookups in flight. The token is verified once per (epoch,
// log type), not once per key. The first failure cancels the lookups still
// running and is returned; the map is complete only when the error is nil.
func (d *DHT) GetMany(tok *auth.Token, requests []verifier.ProofRequest) (map[hashing.Digest][]byte, error) {
	if len(requests) > d.limits.MaxRequests {
		return nil, errors.New("cassandradht: proof request count bound exceeded")
	}
	metadataBytes := 0
	for _, r := range requests {
		metadataBytes += len(r.LogType) + 256
		if metadataBytes > d.limits.AncestorMapBytes || metadataBytes > d.limits.ProofBytes {
			return nil, errors.New("cassandradht: proof request metadata bound exceeded")
		}
	}
	type gate struct {
		epoch   uint64
		logType string
	}
	verified := make(map[gate]struct{})
	keys := make([]hashing.Digest, 0, len(requests))
	queued := make(map[hashing.Digest]struct{}, len(requests))
	for _, r := range requests {
		expected, err := expectedKey(r)
		if err != nil {
			return nil, err
		}
		g := gate{r.Epoch, r.LogType}
		if _, done := verified[g]; !done {
			if err := d.authorize(tok, r.Epoch, r.LogType, r.Key, expected); err != nil {
				return nil, err
			}
			verified[g] = struct{}{}
		} else if r.Key != expected {
			return nil, dht.ErrKeyMismatch
		}
		if _, dup := queued[r.Key]; dup {
			continue
		}
		queued[r.Key] = struct{}{}
		keys = append(keys, r.Key)
	}
	return d.resolve(keys, metadataBytes)
}

// resolve looks every key up with a bounded pool of workers.
func (d *DHT) resolve(keys []hashing.Digest, used int) (map[hashing.Digest][]byte, error) {
	out := make(map[hashing.Digest][]byte, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	ctx, cancel := context.WithCancel(d.ctx)
	defer cancel()

	jobs := make(chan hashing.Digest, min(len(keys), d.limits.QueueBytes/32))

	var (
		mu       sync.Mutex
		firstErr error
		wait     sync.WaitGroup
	)
	workers := min(d.workers, len(keys))
	wait.Add(workers)
	for range workers {
		go func() {
			defer wait.Done()
			for key := range jobs {
				if ctx.Err() != nil {
					return
				}
				value, err := d.lookup(ctx, key)
				mu.Lock()
				if err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("lookup %x: %w", key[:4], err)
						cancel()
					}
				} else {
					cost := len(value) + 256
					if cost > d.limits.ProofBytes-used || cost > d.limits.AncestorMapBytes-used {
						if firstErr == nil {
							firstErr = errors.New("cassandradht: proof/result map byte bound exceeded")
							cancel()
						}
					} else {
						used += cost
						out[key] = value
					}
				}
				mu.Unlock()
			}
		}()
	}
sendLoop:
	for _, key := range keys {
		select {
		case jobs <- key:
		case <-ctx.Done():
			break sendLoop
		}
	}
	close(jobs)
	wait.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
