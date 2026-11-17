package benchmark

import (
	"bytes"
	contract "canonicalingestion"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"testing"

	"dhtbacked/internal/cassandradht"
	"dhtbacked/internal/hashing"
	"dhtbacked/producer"
)

type bufferedSQLFixture struct {
	rows                     [][]driver.Value
	sink                     *bufferedBarrierSink
	queries, begins, commits int
	commitErr                error
	root                     []byte
}
type bufferedConnector struct{ f *bufferedSQLFixture }

func (c bufferedConnector) Connect(context.Context) (driver.Conn, error) {
	return &bufferedConn{c.f}, nil
}
func (c bufferedConnector) Driver() driver.Driver { return bufferedDriver{} }

type bufferedDriver struct{}

func (bufferedDriver) Open(string) (driver.Conn, error) { return nil, errors.New("connector required") }

type bufferedConn struct{ f *bufferedSQLFixture }

func (c *bufferedConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *bufferedConn) Close() error { return nil }
func (c *bufferedConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *bufferedConn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	if !c.f.sink.flushed {
		return nil, errors.New("anchor attempted before proof acknowledgement")
	}
	c.f.begins++
	return &bufferedTx{c.f}, nil
}
func (c *bufferedConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	if !strings.Contains(q, "ORDER BY log_type,leaf_index LIMIT $2") || (len(args) != 2 && len(args) != 4) || args[0].Value != int64(7) || args[1].Value.(int64) < 1 || args[1].Value.(int64) > 7 {
		return nil, fmt.Errorf("unexpected/unbounded digest query: %s %v", q, args)
	}
	c.f.queries++
	var page [][]driver.Value
	for _, row := range c.f.rows {
		if len(args) == 4 && (row[0].(string) < args[2].Value.(string) || (row[0] == args[2].Value && row[1].(int64) <= args[3].Value.(int64))) {
			continue
		}
		if len(page) >= int(args[1].Value.(int64)) {
			break
		}
		page = append(page, row)
	}
	return &bufferedRows{data: page}, nil
}
func (c *bufferedConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if !c.f.sink.flushed {
		return nil, errors.New("publication row escaped barrier")
	}
	if !strings.Contains(q, "dht_epoch_anchors") && !strings.Contains(q, "dht_ingestion_epochs") {
		return nil, fmt.Errorf("unexpected SQL write: %s", q)
	}
	if strings.Contains(q, "INSERT INTO dhtbacked.dht_epoch_anchors") {
		c.f.root = append([]byte(nil), args[1].Value.([]byte)...)
	}
	return driver.RowsAffected(1), nil
}

type bufferedTx struct{ f *bufferedSQLFixture }

func (t *bufferedTx) Commit() error {
	if t.f.commitErr != nil {
		return t.f.commitErr
	}
	t.f.commits++
	return nil
}
func (t *bufferedTx) Rollback() error { return nil }

type bufferedRows struct {
	data  [][]driver.Value
	index int
}

func (r *bufferedRows) Columns() []string { return []string{"log_type", "leaf_index", "d"} }
func (r *bufferedRows) Close() error      { return nil }
func (r *bufferedRows) Next(dst []driver.Value) error {
	if r.index >= len(r.data) {
		return io.EOF
	}
	copy(dst, r.data[r.index])
	r.index++
	return nil
}

type bufferedBarrierSink struct {
	fairMemoryProofSink
	flushed  bool
	flushErr error
}

func (s *bufferedBarrierSink) Flush() error {
	if s.flushErr != nil {
		return s.flushErr
	}
	s.flushed = true
	return nil
}

func newBufferedSQLFixture(t *testing.T) (*IncrementalDHT, *bufferedSQLFixture, [5]uint64) {
	t.Helper()
	counts := [5]uint64{3, 0, 2, 0, 1}
	f := &bufferedSQLFixture{sink: &bufferedBarrierSink{fairMemoryProofSink: make(fairMemoryProofSink)}}
	for i, n := range counts {
		for j := uint64(0); j < n; j++ {
			d := hashing.H([]byte(fmt.Sprintf("durable/%d/%d", i, j)))
			f.rows = append(f.rows, []driver.Value{producer.LogTypes[i], int64(j), append([]byte(nil), d[:]...)})
		}
	}
	sort.Slice(f.rows, func(i, j int) bool {
		if f.rows[i][0] != f.rows[j][0] {
			return f.rows[i][0].(string) < f.rows[j][0].(string)
		}
		return f.rows[i][1].(int64) < f.rows[j][1].(int64)
	})
	db := sql.OpenDB(bufferedConnector{f})
	t.Cleanup(func() { _ = db.Close() })
	limits := cassandradht.DefaultLimits()
	return &IncrementalDHT{DB: db, Options: IncrementalOptions{Cassandra: &cassandradht.Config{Limits: &limits}}}, f, counts
}

func TestBufferedPublicationRespectsDigestFetchLimit(t *testing.T) {
	a, f, counts := newBufferedSQLFixture(t)
	a.Options.Cassandra.Limits.RecordFetchBatch = 2
	if err := a.publishBufferedEpoch(context.Background(), f.sink, 7, 6, 6, counts); err != nil {
		t.Fatal(err)
	}
	if f.queries != 4 || f.commits != 1 {
		t.Fatal("configured fetch limit or pagination lost rows", f.queries, f.commits)
	}
}

func TestBufferedPublicationDurableReadAndAnchorBarrier(t *testing.T) {
	var wantRoot []byte
	var wantProof fairMemoryProofSink
	for _, parallel := range []bool{false, true} {
		a, f, counts := newBufferedSQLFixture(t)
		if parallel {
			e := contract.DefaultExecution()
			e.CPUWorkers = 2
			a.Execution = &e
		}
		if err := a.publishBufferedEpoch(context.Background(), f.sink, 7, 6, 6, counts); err != nil {
			t.Fatal(err)
		}
		if f.queries != 1 || f.begins != 1 || f.commits != 1 || !f.sink.flushed || len(f.sink.fairMemoryProofSink) == 0 {
			t.Fatal("publication did not use one bounded durable read and acknowledgement barrier", f)
		}
		if !parallel {
			wantRoot, wantProof = f.root, f.sink.fairMemoryProofSink
		} else if !bytes.Equal(wantRoot, f.root) || !reflect.DeepEqual(wantProof, f.sink.fairMemoryProofSink) {
			t.Fatal("parallel construction changed the epoch anchor or proof bytes")
		}
	}
}
func TestBufferedPublicationFailureNeverCompletesEpoch(t *testing.T) {
	for _, failure := range []string{"flush", "commit", "missing", "index", "digest", "unknown"} {
		t.Run(failure, func(t *testing.T) {
			a, f, counts := newBufferedSQLFixture(t)
			switch failure {
			case "flush":
				f.sink.flushErr = errors.New("Cassandra timeout")
			case "commit":
				f.commitErr = errors.New("lost SQL commit")
			case "missing":
				f.rows = f.rows[:len(f.rows)-1]
			case "index":
				f.rows[0][1] = int64(99)
			case "digest":
				f.rows[0][2] = []byte{1}
			case "unknown":
				f.rows[0][0] = "unrecognized"
			}
			if err := a.publishBufferedEpoch(context.Background(), f.sink, 7, 6, 6, counts); err == nil {
				t.Fatal("invalid/unacknowledged epoch completed")
			}
			if f.commits != 0 {
				t.Fatal("failed publication marked complete")
			}
			if failure != "commit" && f.begins != 0 {
				t.Fatal("anchor transaction started before valid acknowledged proofs")
			}
		})
	}
}
