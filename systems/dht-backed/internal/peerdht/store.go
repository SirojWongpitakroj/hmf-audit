// Package peerdht implements the deployed four-peer overlay. Each peer owns a
// separate node-local PostgreSQL KV database on persistent storage. It is not
// the centralized log database and not the Cassandra-backed overlay simulation.
package peerdht

import (
	"bytes"
	contract "canonicalingestion"
	"container/list"
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"

	"dhtbacked/internal/hashing"
)

type Limits struct {
	Workers, MaxRequests, QueueBytes                             int
	MaxValueBytes, MaxRequestBytes, ProofBytes, AncestorMapBytes int
	CacheBytes, CacheEntries, RecordFetchBatch, RecordBytes      int
}

func (l Limits) Validate() error {
	if l.Workers < 1 || l.Workers > 1024 || l.MaxRequests < 1 || l.QueueBytes < 32 || l.MaxValueBytes < 1 || l.MaxRequestBytes < l.MaxValueBytes || l.ProofBytes < l.MaxValueBytes || l.AncestorMapBytes < l.MaxValueBytes || l.CacheBytes < 0 || l.CacheEntries < 0 || l.RecordFetchBatch < 1 || l.RecordBytes < 1 {
		return errors.New("peerdht: invalid explicit request/cache/worker limits")
	}
	return nil
}

type Stats struct {
	CacheEnabled                                                                                                  bool
	CacheBytes, CacheEntries, CacheHits, CacheMisses, StorageReads, StorageWrites, Lookups, Hops, AncestorRecords uint64
	ProofBytes                                                                                                    uint64
	Memory                                                                                                        contract.MemorySnapshot
}
type cached struct {
	key   hashing.Digest
	value []byte
}
type cache struct {
	mu                  sync.Mutex
	limits              Limits
	items               map[hashing.Digest]*list.Element
	lru                 *list.List
	bytes, hits, misses uint64
}

func newCache(l Limits) *cache {
	return &cache{limits: l, items: map[hashing.Digest]*list.Element{}, lru: list.New()}
}
func (c *cache) get(k hashing.Digest) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.items[k]; e != nil {
		c.hits++
		c.lru.MoveToFront(e)
		return append([]byte(nil), e.Value.(cached).value...), true
	}
	c.misses++
	return nil, false
}
func (c *cache) put(k hashing.Digest, v []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cost := uint64(len(v) + 256)
	if c.limits.CacheEntries == 0 || cost > uint64(c.limits.CacheBytes) {
		return
	}
	if _, ok := c.items[k]; ok {
		return
	}
	for len(c.items) >= c.limits.CacheEntries || c.bytes+cost > uint64(c.limits.CacheBytes) {
		e := c.lru.Back()
		old := e.Value.(cached)
		delete(c.items, old.key)
		c.bytes -= uint64(len(old.value) + 256)
		c.lru.Remove(e)
	}
	c.items[k] = c.lru.PushFront(cached{k, append([]byte(nil), v...)})
	c.bytes += cost
}
func (c *cache) stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Stats{CacheEnabled: c.limits.CacheEntries > 0 && c.limits.CacheBytes > 0, CacheBytes: c.bytes, CacheEntries: uint64(len(c.items)), CacheHits: c.hits, CacheMisses: c.misses}
}

type LocalStore struct {
	DB            *sql.DB
	Node          int
	Overlay       string
	MaxValueBytes int
	Reads, Writes atomic.Uint64
}

func (s *LocalStore) Migrate(ctx context.Context, identity []byte) error {
	if s.Node < 0 || s.Node >= 4 || s.Overlay == "" || s.MaxValueBytes < 1 {
		return errors.New("peerdht: invalid node-local store")
	}
	_, err := s.DB.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS dht_peer;
 CREATE TABLE IF NOT EXISTS dht_peer.identity(singleton boolean PRIMARY KEY CHECK(singleton),node integer NOT NULL,overlay text NOT NULL,identity bytea NOT NULL);
 CREATE TABLE IF NOT EXISTS dht_peer.entries(key bytea PRIMARY KEY CHECK(octet_length(key)=32),value bytea NOT NULL);`)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO dht_peer.identity(singleton,node,overlay,identity) VALUES(true,$1,$2,$3) ON CONFLICT DO NOTHING`, s.Node, s.Overlay, identity)
	if err != nil {
		return err
	}
	var node int
	var overlay string
	var stored []byte
	if err = s.DB.QueryRowContext(ctx, `SELECT node,overlay,identity FROM dht_peer.identity WHERE singleton`).Scan(&node, &overlay, &stored); err != nil {
		return err
	}
	if node != s.Node || overlay != s.Overlay || !bytes.Equal(stored, identity) {
		return errors.New("peerdht: database belongs to another node/overlay/authority; each peer requires its own persistent database")
	}
	return nil
}
func (s *LocalStore) Get(ctx context.Context, k hashing.Digest) ([]byte, error) {
	s.Reads.Add(1)
	var b []byte
	var size int
	err := s.DB.QueryRowContext(ctx, `SELECT octet_length(value),CASE WHEN octet_length(value)<=$2 THEN value ELSE NULL END FROM dht_peer.entries WHERE key=$1`, k[:], s.MaxValueBytes).Scan(&size, &b)
	if err != nil {
		return nil, err
	}
	if size > s.MaxValueBytes {
		return nil, errors.New("peerdht: stored entry exceeds value budget")
	}
	return b, nil
}
func (s *LocalStore) Put(ctx context.Context, k hashing.Digest, b []byte) error {
	if len(b) == 0 || len(b) > s.MaxValueBytes {
		return errors.New("peerdht: entry exceeds value budget")
	}
	s.Writes.Add(1)
	result, err := s.DB.ExecContext(ctx, `INSERT INTO dht_peer.entries(key,value) VALUES($1,$2) ON CONFLICT DO NOTHING`, k[:], b)
	if err != nil {
		return err
	}
	if n, _ := result.RowsAffected(); n == 1 {
		return nil
	}
	old, err := s.Get(ctx, k)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, b) {
		return errors.New("peerdht: conflicting replay of finalized authentication entry")
	}
	return nil
}
