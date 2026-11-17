// Package diskindex stores BLAS's canonical tree leaves, bitmap fragments and
// authentication nodes separately. It changes storage ownership, not hashing.
package diskindex

import (
	"container/list"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"blas/bhash"
)

type Limits struct{ PageBytes, CacheBytes, CacheEntries, BitmapBytes int }

func (l Limits) Validate() error {
	if l.PageBytes < 32 || l.CacheBytes < 0 || l.CacheEntries < 0 || l.BitmapBytes < 1 {
		return errors.New("diskindex: explicit positive page/bitmap budgets and nonnegative cache budgets required")
	}
	return nil
}

type Stats struct{ CacheBytes, CacheEntries, CacheHits, CacheMisses, StorageReads, StorageWrites uint64 }
type cached struct {
	key   string
	value []byte
}
type Cache struct {
	mu     sync.Mutex
	limits Limits
	items  map[string]*list.Element
	lru    *list.List
	stats  Stats
}

func NewCache(l Limits) (*Cache, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	return &Cache{limits: l, items: map[string]*list.Element{}, lru: list.New()}, nil
}
func (c *Cache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if e := c.items[key]; e != nil {
		c.lru.MoveToFront(e)
		c.stats.CacheHits++
		return append([]byte(nil), e.Value.(cached).value...), true
	}
	c.stats.CacheMisses++
	return nil, false
}
func (c *Cache) put(key string, b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cost := len(key) + len(b) + 128
	if c.limits.CacheEntries == 0 || c.limits.CacheBytes < cost {
		return
	}
	if _, ok := c.items[key]; ok {
		return
	}
	for len(c.items) >= c.limits.CacheEntries || c.stats.CacheBytes+uint64(cost) > uint64(c.limits.CacheBytes) {
		e := c.lru.Back()
		v := e.Value.(cached)
		delete(c.items, v.key)
		c.stats.CacheBytes -= uint64(len(v.key) + len(v.value) + 128)
		c.lru.Remove(e)
	}
	e := c.lru.PushFront(cached{key, append([]byte(nil), b...)})
	c.items[key] = e
	c.stats.CacheBytes += uint64(cost)
	c.stats.CacheEntries = uint64(len(c.items))
}
func (c *Cache) Stats() Stats { c.mu.Lock(); defer c.mu.Unlock(); return c.stats }

// Clear drops only audit read pages. Lifetime counters remain monotonic.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.lru.Init()
	c.stats.CacheBytes, c.stats.CacheEntries = 0, 0
}
func (c *Cache) read()  { c.mu.Lock(); c.stats.StorageReads++; c.mu.Unlock() }
func (c *Cache) write() { c.mu.Lock(); c.stats.StorageWrites++; c.mu.Unlock() }

type SQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}
type Store struct {
	CompactZeroBitmaps bool
	Q                  SQL
	Cache              *Cache
	Limits             Limits
	Scope              string
	Timing             *IOTiming
	Batch              *WriteBatch
	Computations       *ComputationCache // pure hash memo; never controls persistence
}

type IOTiming struct {
	Rows, Bytes uint64
	Read        time.Duration
	Statements  uint64
}

func (c *Cache) GetValue(key string) ([]byte, bool) { return c.get(key) }
func (c *Cache) PutValue(key string, b []byte)      { c.put(key, b) }

type timedRow struct {
	row     *sql.Row
	started time.Time
	timing  *IOTiming
}

func (r timedRow) Scan(v ...any) error {
	err := r.row.Scan(v...)
	if r.timing != nil {
		r.timing.Read += time.Since(r.started)
		r.timing.Statements++
		if err == nil {
			r.timing.Rows++
			for _, value := range v {
				switch p := value.(type) {
				case *[]byte:
					r.timing.Bytes += uint64(len(*p))
				case *string:
					r.timing.Bytes += uint64(len(*p))
				case *int64, *uint64:
					r.timing.Bytes += 8
				case *int, *int32, *uint32:
					r.timing.Bytes += 4
				case *bool:
					r.timing.Bytes++
				}
			}
		}
	}
	return err
}
func (s Store) Row(ctx context.Context, q string, args ...any) timedRow {
	start := time.Now()
	return timedRow{row: s.Q.QueryRowContext(ctx, q, args...), started: start, timing: s.Timing}
}

func (s Store) Migrate(ctx context.Context) error {
	if err := s.Limits.Validate(); err != nil {
		return err
	}
	_, err := s.Q.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS blas;
 CREATE TABLE IF NOT EXISTS blas.index_pages(scope text NOT NULL,tree text NOT NULL,page text NOT NULL,payload bytea NOT NULL,PRIMARY KEY(scope,tree,page));
 CREATE TABLE IF NOT EXISTS blas.index_leaves(scope text NOT NULL,tree text NOT NULL,pos bigint NOT NULL,keyword bytea NOT NULL,value bigint NOT NULL,PRIMARY KEY(scope,tree,pos));
 CREATE INDEX IF NOT EXISTS blas_index_keyword ON blas.index_leaves(scope,tree,keyword);
 CREATE INDEX IF NOT EXISTS blas_index_value ON blas.index_leaves(scope,tree,value);`)
	return err
}

// Writers never use the read cache. Tree names are immutable generations; a
// rollback must not leave uncommitted pages visible through a process cache.
func (s Store) Put(ctx context.Context, tree, page string, b []byte) error {
	if len(b) > s.Limits.PageBytes {
		return fmt.Errorf("diskindex: page exceeds %d-byte limit", s.Limits.PageBytes)
	}
	if s.Batch != nil {
		if s.Cache != nil {
			return errors.New("diskindex: write batch cannot share a read cache")
		}
		return s.Batch.page(ctx, s.Scope, tree, page, b)
	}
	_, err := s.Q.ExecContext(ctx, `INSERT INTO blas.index_pages(scope,tree,page,payload) VALUES($1,$2,$3,$4)`, s.Scope, tree, page, b)
	if s.Cache != nil {
		s.Cache.write()
	}
	return err
}
func (s Store) Get(ctx context.Context, tree, page string) ([]byte, error) {
	keyBytes, _ := json.Marshal([]string{s.Scope, tree, page})
	key := string(keyBytes)
	if s.Cache != nil {
		if b, ok := s.Cache.get(key); ok {
			return b, nil
		}
		s.Cache.read()
	}
	var b []byte
	var size int
	err := s.Row(ctx, `SELECT octet_length(payload),CASE WHEN octet_length(payload)<=$4 THEN payload ELSE NULL END FROM blas.index_pages WHERE scope=$1 AND tree=$2 AND page=$3`, s.Scope, tree, page, s.Limits.PageBytes).Scan(&size, &b)
	if err != nil {
		return nil, err
	}
	if size > s.Limits.PageBytes {
		return nil, errors.New("diskindex: stored page exceeds configured budget")
	}
	if s.Cache != nil {
		s.Cache.put(key, b)
	}
	return b, nil
}
func (s Store) Hash(ctx context.Context, tree string, level, pos uint64) (bhash.Digest, error) {
	b, err := s.Get(ctx, tree, fmt.Sprintf("node/%d/%d", level, pos))
	if err != nil {
		return bhash.Digest{}, err
	}
	var n node
	if err = json.Unmarshal(b, &n); err != nil {
		return bhash.Digest{}, err
	}
	return n.Hash, nil
}
func (s Store) Delete(ctx context.Context, tree string) error {
	if s.Cache != nil {
		return errors.New("diskindex: deletion requires cache-free writer")
	}
	if _, err := s.Q.ExecContext(ctx, `DELETE FROM blas.index_pages WHERE scope=$1 AND tree=$2`, s.Scope, tree); err != nil {
		return err
	}
	_, err := s.Q.ExecContext(ctx, `DELETE FROM blas.index_leaves WHERE scope=$1 AND tree=$2`, s.Scope, tree)
	return err
}

type Metadata struct {
	ZeroMarkers bool `json:",omitempty"`
	Kind        string
	Fanout      int
	N, Leaves   uint64
	Root        bhash.Digest
}
type node struct {
	Hash bhash.Digest
	V    []int64
	Min  int64
}
type leaf struct {
	Zero    bool `json:",omitempty"`
	Keyword []byte
	Value   int64
	OID     uint64
}

func (s Store) putJSON(ctx context.Context, tree, page string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.Put(ctx, tree, page, b)
}
func (s Store) getJSON(ctx context.Context, tree, page string, v any) error {
	b, err := s.Get(ctx, tree, page)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
