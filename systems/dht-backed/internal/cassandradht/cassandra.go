package cassandradht

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	gocql "github.com/apache/cassandra-gocql-driver/v2"

	"dhtbacked/internal/hashing"
)

// Keyspace is the Cassandra keyspace holding the DHT-backed proof records and
// routing table; fair-persistence/cassandra/dht-backed.cql creates it.
const Keyspace = "dhtbacked"

const (
	selectProof = "SELECT payload FROM proof_entries WHERE proof_key = ?"
	insertProof = "INSERT INTO proof_entries (proof_key, payload) VALUES (?, ?)"
	selectRoute = "SELECT next_peer FROM routing_table WHERE peer = ? AND level = ?"
	insertRoute = "INSERT INTO routing_table (peer, level, next_peer) VALUES (?, ?, ?)"
)

// Cassandra is the KV a DHT sits on: proof_entries holds the records and
// routing_table the overlay's routing state.
type Cassandra struct {
	session       *gocql.Session
	keyspace      string
	maxValueBytes int
}

var _ KV = (*Cassandra)(nil)

// Open connects to the cluster at host:port.
func Open(host string, port int) (*Cassandra, error) {
	if host == "" {
		return nil, errors.New("cassandradht: Cassandra host is required")
	}
	if port <= 0 {
		return nil, fmt.Errorf("cassandradht: invalid Cassandra port %d", port)
	}
	cluster := gocql.NewCluster(host)
	cluster.Port = port
	cluster.Keyspace = Keyspace
	session, err := cluster.CreateSession()
	if err != nil {
		return nil, fmt.Errorf("cassandradht: connect: %w", err)
	}
	return &Cassandra{session: session}, nil
}

// Close releases the session.
func (c *Cassandra) Close() {
	if c != nil && c.session != nil {
		c.session.Close()
	}
}

// Proof reads the value stored at key.
func (c *Cassandra) Proof(ctx context.Context, key hashing.Digest) ([]byte, error) {
	var payload []byte
	err := c.session.Query("SELECT payload FROM "+c.table("proof_entries")+" WHERE proof_key = ?", key[:]).ScanContext(ctx, &payload)
	if errors.Is(err, gocql.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if c.maxValueBytes > 0 && len(payload) > c.maxValueBytes {
		return nil, errors.New("cassandradht: stored proof exceeds value bound")
	}
	return payload, nil
}

// Route reads one routing-table row.
func (c *Cassandra) Route(ctx context.Context, peer, level int) (int, error) {
	var next int
	if err := c.session.Query("SELECT next_peer FROM "+c.table("routing_table")+" WHERE peer = ? AND level = ?", peer, level).ScanContext(ctx, &next); err != nil {
		return 0, err
	}
	return next, nil
}

// Reset removes every proof record and routing row.
func (c *Cassandra) Reset(ctx context.Context) error {
	if c.keyspace != "" {
		return errors.New("cassandradht: active proof history cannot be reset through the legacy fixture API")
	}
	for _, table := range []string{"proof_entries", "routing_table"} {
		if err := c.session.Query("TRUNCATE " + table).ExecContext(ctx); err != nil {
			return fmt.Errorf("cassandradht: truncate %s: %w", table, err)
		}
	}
	return nil
}

// SeedRoutes writes the routing table for a DHT of the given size: one row per
// (peer, level), pointing at the peer one bit-flip away.
func (c *Cassandra) SeedRoutes(ctx context.Context, peers int) error {
	for peer := 0; peer < peers; peer++ {
		for level := 0; level < Hops(peers); level++ {
			if err := c.session.Query(insertRoute, peer, level, RouteNext(peer, level)).ExecContext(ctx); err != nil {
				return fmt.Errorf("cassandradht: seed route (%d,%d): %w", peer, level, err)
			}
		}
	}
	return nil
}

// Seeder bulk-loads proof records with a bounded pool of writers. It is setup
// work, outside every measured request.
type Seeder struct {
	ctx  context.Context
	c    *Cassandra
	rows chan seedRow
	wait sync.WaitGroup

	closeOnce sync.Once
	mu        sync.Mutex
	firstErr  error
}

type seedRow struct {
	key     hashing.Digest
	payload []byte
}

// NewSeeder starts writers workers.
func (c *Cassandra) NewSeeder(ctx context.Context, writers int) *Seeder {
	s := &Seeder{ctx: ctx, c: c, rows: make(chan seedRow, writers*64)}
	s.wait.Add(writers)
	for range writers {
		go func() {
			defer s.wait.Done()
			s.drain(s.writeBatch)
		}()
	}
	return s
}

const (
	// seedBatchRows and seedBatchBytes bound one unlogged batch of proof
	// records. The rows are in different partitions, and Cassandra rejects a
	// batch above batch_size_fail_threshold (50 KiB), so the byte bound stays
	// well below it. A single record larger than the bound is written alone.
	seedBatchRows  = 32
	seedBatchBytes = 32 << 10

	seedWriteAttempts = 5
)

// drain takes queued rows off s.rows in groups of at most seedBatchRows rows
// and seedBatchBytes bytes and passes each group to write. It never waits for
// a group to fill: it takes what is already queued, so a slow producer gets
// small batches rather than added latency. It returns when s.rows is closed
// and empty. write must not keep the slice.
func (s *Seeder) drain(write func([]seedRow)) {
	batch := make([]seedRow, 0, seedBatchRows)
	size := func(row seedRow) int { return len(row.key) + len(row.payload) }
	for first := range s.rows {
		batch = append(batch[:0], first)
		bytes := size(first)
	fill:
		for len(batch) < seedBatchRows && bytes < seedBatchBytes {
			select {
			case row, ok := <-s.rows:
				if !ok {
					break fill
				}
				if bytes+size(row) > seedBatchBytes {
					write(batch)
					batch = append(batch[:0], row)
					bytes = size(row)
					continue
				}
				batch = append(batch, row)
				bytes += size(row)
			default:
				break fill
			}
		}
		write(batch)
	}
}

// writeBatch writes rows as one unlogged batch (or one INSERT for a single
// row). The inserts are idempotent, so a transient Cassandra failure is
// retried a few times; anything else is recorded and stops the seeder.
func (s *Seeder) writeBatch(rows []seedRow) {
	if len(rows) == 0 || s.Err() != nil {
		return
	}
	var err error
	for attempt := 1; attempt <= seedWriteAttempts; attempt++ {
		if err = s.ctx.Err(); err != nil {
			break
		}
		if err = s.execBatch(rows); err == nil || !transientWriteError(err) || attempt == seedWriteAttempts {
			break
		}
		select {
		case <-s.ctx.Done():
		case <-time.After(time.Duration(1<<(attempt-1)) * 100 * time.Millisecond):
		}
	}
	if err != nil {
		s.setErr(fmt.Errorf("cassandradht: write proof records: %w", err))
	}
}

func (s *Seeder) execBatch(rows []seedRow) error {
	if len(rows) == 1 {
		return s.c.session.Query(insertProof, rows[0].key[:], rows[0].payload).ExecContext(s.ctx)
	}
	batch := s.c.session.Batch(gocql.UnloggedBatch)
	for _, row := range rows {
		batch.Query(insertProof, row.key[:], row.payload)
	}
	return batch.ExecContext(s.ctx)
}

func transientWriteError(err error) bool {
	var timeout *gocql.RequestErrWriteTimeout
	var unavailable *gocql.RequestErrUnavailable
	var overloaded *gocql.RequestErrOverloaded
	return errors.As(err, &timeout) || errors.As(err, &unavailable) ||
		errors.As(err, &overloaded) || errors.Is(err, gocql.ErrTimeoutNoResponse)
}

// Put queues one record. The caller must not modify payload afterwards.
func (s *Seeder) Put(key hashing.Digest, payload []byte) error {
	if err := s.Err(); err != nil {
		return err
	}
	select {
	case s.rows <- seedRow{key: key, payload: payload}:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}

// Flush waits for every queued record to be written and stops the writers. The
// Seeder cannot be used afterwards.
func (s *Seeder) Flush() error {
	s.closeOnce.Do(func() {
		close(s.rows)
		s.wait.Wait()
	})
	return s.Err()
}

func (s *Seeder) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.firstErr == nil {
		s.firstErr = err
	}
}

// Err returns the first write failure, if any.
func (s *Seeder) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.firstErr
}
