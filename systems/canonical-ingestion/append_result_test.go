package canonicalingestion

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// This in-memory SQL fixture checks the production Append and AppendWithResult
// paths, including their exact inserted payload/hash bytes and commit barrier.
type appendFixture struct {
	mu                           sync.Mutex
	n                            int64
	hash                         []byte
	rows                         map[int64][]driver.Value
	commitErr                    error
	applyOnError                 bool
	commitEntered, commitRelease chan struct{}
	queries, commits             int
}
type appendConn struct {
	d  *appendFixture
	tx *appendTx
}
type appendTx struct {
	c    *appendConn
	n    int64
	hash []byte
	rows map[int64][]driver.Value
}
type appendRows struct {
	names  []string
	values [][]driver.Value
}

var appendFixtureID atomic.Uint64

func (d *appendFixture) Open(string) (driver.Conn, error) { return &appendConn{d: d}, nil }
func (c *appendConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (c *appendConn) Close() error { return nil }
func (c *appendConn) Begin() (driver.Tx, error) {
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	tx := &appendTx{c: c, n: c.d.n, hash: bytes.Clone(c.d.hash), rows: map[int64][]driver.Value{}}
	for k, v := range c.d.rows {
		tx.rows[k] = v
	}
	c.tx = tx
	return tx, nil
}
func (c *appendConn) QueryContext(_ context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.d.mu.Lock()
	c.d.queries++
	c.d.mu.Unlock()
	if strings.HasPrefix(q, "SELECT committed,prefix_hash") && strings.HasSuffix(q, "FOR UPDATE") {
		return &appendRows{[]string{"committed", "prefix_hash"}, [][]driver.Value{{c.tx.n, bytes.Clone(c.tx.hash)}}}, nil
	}
	if strings.HasPrefix(q, "SELECT source_id,payload") {
		v, ok := c.tx.rows[args[1].Value.(int64)]
		r := &appendRows{names: []string{"source_id", "payload"}}
		if ok {
			r.values = [][]driver.Value{{v[2], v[3]}}
		}
		return r, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", q)
}
func (c *appendConn) ExecContext(_ context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	if strings.HasPrefix(q, "INSERT INTO canonical_ingestion.entries") {
		for i := 0; i < len(args); i += 6 {
			row := make([]driver.Value, 6)
			for j := range row {
				v := args[i+j].Value
				if b, ok := v.([]byte); ok {
					v = bytes.Clone(b)
				}
				row[j] = v
			}
			n := row[1].(int64)
			if _, ok := c.tx.rows[n]; ok {
				return nil, errors.New("duplicate ordinal")
			}
			c.tx.rows[n] = row
		}
	} else if strings.HasPrefix(q, "UPDATE canonical_ingestion.streams") {
		c.tx.n = args[1].Value.(int64)
		c.tx.hash = bytes.Clone(args[2].Value.([]byte))
	} else {
		return nil, fmt.Errorf("unexpected exec: %s", q)
	}
	return driver.RowsAffected(1), nil
}
func (tx *appendTx) Commit() error {
	d := tx.c.d
	if d.commitEntered != nil {
		close(d.commitEntered)
		<-d.commitRelease
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.commits++
	if d.commitErr == nil || d.applyOnError {
		d.n = tx.n
		d.hash = bytes.Clone(tx.hash)
		d.rows = tx.rows
	}
	return d.commitErr
}
func (tx *appendTx) Rollback() error    { return nil }
func (r *appendRows) Columns() []string { return r.names }
func (r *appendRows) Close() error      { return nil }
func (r *appendRows) Next(out []driver.Value) error {
	if len(r.values) == 0 {
		return io.EOF
	}
	copy(out, r.values[0])
	r.values = r.values[1:]
	return nil
}
func newAppendFixture(t *testing.T) (Store, *appendFixture) {
	t.Helper()
	d := &appendFixture{hash: make([]byte, 32), rows: map[int64][]driver.Value{}}
	name := fmt.Sprintf("append-result-%d", appendFixtureID.Add(1))
	sql.Register(name, d)
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	return Store{DB: db}, d
}
func TestAppendResultMatchesLegacyAndReplay(t *testing.T) {
	l := Limits{10, 8192, 2048}
	ctx := context.Background()
	legacy, ld := newAppendFixture(t)
	optimized, od := newAppendFixture(t)
	es := []Entry{entry(1), entry(2), entry(3)}
	if err := legacy.Append(ctx, "s", es, l); err != nil {
		t.Fatal(err)
	}
	r, err := optimized.AppendWithResult(ctx, "s", es, l)
	if err != nil {
		t.Fatal(err)
	}
	var want [32]byte
	for _, e := range es {
		want = nextHash(want, e)
	}
	if r.Committed != 3 || r.PrefixHash != want || !bytes.Equal(ld.hash, od.hash) {
		t.Fatalf("cursor/hash: %+v", r)
	}
	for n, row := range ld.rows {
		for i, v := range row {
			if a, ok := v.([]byte); ok {
				if !bytes.Equal(a, od.rows[n][i].([]byte)) {
					t.Fatal("stored bytes differ")
				}
			} else if v != od.rows[n][i] {
				t.Fatal("stored field differs")
			}
		}
	}
	r, err = optimized.AppendWithResult(ctx, "s", es, l)
	if err != nil || r.Committed != 3 || r.PrefixHash != want {
		t.Fatal("replay", r, err)
	}
	r, err = optimized.AppendWithResult(ctx, "s", []Entry{entry(3), entry(4)}, l)
	want = nextHash(want, entry(4))
	if err != nil || r.Committed != 4 || r.PrefixHash != want {
		t.Fatal("mixed replay", r, err)
	}
	if od.queries != 7 {
		t.Fatalf("unexpected queries (manifest reread?): %d", od.queries)
	}
}
func TestAppendResultFailureAndUncertainCommit(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprint(applied), func(t *testing.T) {
			s, d := newAppendFixture(t)
			d.commitErr = errors.New("lost commit acknowledgement")
			d.applyOnError = applied
			es := []Entry{entry(1), entry(2)}
			l := Limits{10, 8192, 2048}
			r, err := s.AppendWithResult(context.Background(), "s", es, l)
			if err == nil || r.Committed != 0 || r.PrefixHash != [32]byte{} {
				t.Fatal("failed commit exposed cursor", r, err)
			}
			if (d.n == 2) != applied {
				t.Fatal("fixture visibility")
			}
			d.commitErr = nil
			r, err = s.AppendWithResult(context.Background(), "s", es, l)
			if err != nil || r.Committed != 2 || len(d.rows) != 2 {
				t.Fatal("retry", r, err)
			}
		})
	}
}
func TestAppendResultRejectsConflictAndGap(t *testing.T) {
	for _, kind := range []string{"payload", "source", "gap", "duplicate-source", "invalid", "limits"} {
		t.Run(kind, func(t *testing.T) {
			s, d := newAppendFixture(t)
			ctx := context.Background()
			l := Limits{10, 8192, 2048}
			if err := s.Append(ctx, "s", []Entry{entry(1)}, l); err != nil {
				t.Fatal(err)
			}
			before := bytes.Clone(d.hash)
			e := entry(1)
			var batch []Entry
			switch kind {
			case "payload":
				e.Record = append(bytes.Clone(e.Record[:len(e.Record)-1]), []byte(`,"extra":true}`)...)
			case "source":
				e.SourceID = entry(2).SourceID
			case "gap":
				e = entry(3)
			case "duplicate-source":
				e = entry(2)
				e.Ordinal = 3
				batch = []Entry{entry(2), e}
			case "invalid":
				e.Record = []byte("broken")
			case "limits":
				l.MaxRecords = 0
			}
			if batch == nil {
				batch = []Entry{e}
			}
			r, err := s.AppendWithResult(ctx, "s", batch, l)
			if err == nil || r.Committed != 0 || r.PrefixHash != [32]byte{} || d.n != 1 || !bytes.Equal(before, d.hash) || len(d.rows) != 1 {
				t.Fatal("invalid append changed state", r, err)
			}
		})
	}
}
func TestAppendResultWaitsForCommit(t *testing.T) {
	s, d := newAppendFixture(t)
	d.commitEntered = make(chan struct{})
	d.commitRelease = make(chan struct{})
	finished := make(chan AppendResult, 1)
	errs := make(chan error, 1)
	go func() {
		r, err := s.AppendWithResult(context.Background(), "s", []Entry{entry(1)}, Limits{10, 8192, 2048})
		finished <- r
		errs <- err
	}()
	select {
	case <-d.commitEntered:
	case <-time.After(3 * time.Second):
		t.Fatal("commit not reached")
	}
	select {
	case <-finished:
		t.Fatal("returned before commit")
	default:
	}
	close(d.commitRelease)
	if r := <-finished; r.Committed != 1 {
		t.Fatal(r)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}
