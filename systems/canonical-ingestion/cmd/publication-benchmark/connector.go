package main

// Benchmark-only driver adapter. Store.AppendWithResult remains the production
// implementation under test. Only its table schema and INSERT transport change.
// COPY executes on the SAME pgx connection as database/sql's active transaction;
// it never opens a side connection or acknowledges an uncommitted batch.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	contract "canonicalingestion"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type variant struct {
	Name  string `json:"name"`
	Chunk int    `json:"insert_chunk_rows,omitempty"`
	Copy  bool   `json:"copy"`
}

var variants = []variant{
	{Name: "current"},
	{Name: "values-100", Chunk: 100},
	{Name: "values-250", Chunk: 250},
	{Name: "values-500", Chunk: 500},
	{Name: "values-1000", Chunk: 1000},
	{Name: "copy", Copy: true},
}

type writeSnapshot struct {
	Calls, Rows, MinRows, MaxRows int64
	Nanos                         int64
}
type writeStats struct {
	mu sync.Mutex
	s  writeSnapshot
}

func (s *writeStats) observe(rows int64, elapsed time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.s.Calls++
	s.s.Rows += rows
	s.s.Nanos += int64(elapsed)
	if s.s.Calls == 1 || rows < s.s.MinRows {
		s.s.MinRows = rows
	}
	if rows > s.s.MaxRows {
		s.s.MaxRows = rows
	}
}
func (s *writeStats) snapshot() writeSnapshot { s.mu.Lock(); defer s.mu.Unlock(); return s.s }

type benchConnector struct {
	driver.Connector
	schema string
	mode   variant
	stats  *writeStats
	fault  string // consumed once on this connection, correctness probes only
}
type benchConn struct {
	*stdlib.Conn
	owner *benchConnector
	fault string
}

func benchmarkDB(cfg *pgx.ConnConfig, schema string, mode variant, fault string) (*sql.DB, *writeStats) {
	stats := &writeStats{}
	c := &benchConnector{Connector: stdlib.GetConnector(*cfg.Copy()), schema: schema, mode: mode, stats: stats, fault: fault}
	db := sql.OpenDB(c)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, stats
}
func (c *benchConnector) Connect(ctx context.Context) (driver.Conn, error) {
	base, err := c.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	native, ok := base.(*stdlib.Conn)
	if !ok {
		base.Close()
		return nil, errors.New("benchmark requires pgx stdlib connection")
	}
	return &benchConn{Conn: native, owner: c, fault: c.fault}, nil
}
func (c *benchConn) rewrite(q string) string {
	return strings.ReplaceAll(q, "canonical_ingestion.", pgx.Identifier{c.owner.schema}.Sanitize()+".")
}
func (c *benchConn) Prepare(q string) (driver.Stmt, error) { return c.Conn.Prepare(c.rewrite(q)) }
func (c *benchConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	return c.Conn.PrepareContext(ctx, c.rewrite(q))
}
func (c *benchConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	return c.Conn.QueryContext(ctx, c.rewrite(q), args)
}
func (c *benchConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *benchConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	tx, err := c.Conn.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &benchTx{Tx: tx, c: c}, nil
}

var injected = errors.New("benchmark injected publication failure")

func (c *benchConn) takeFault(name string) bool {
	if c.fault == name {
		c.fault = ""
		return true
	}
	return false
}
func (c *benchConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	isInsert := strings.HasPrefix(q, "INSERT INTO canonical_ingestion.entries(")
	if strings.HasPrefix(q, "UPDATE canonical_ingestion.streams SET") && c.takeFault("before-cursor") {
		return nil, injected
	}
	q = c.rewrite(q)
	if !isInsert {
		return c.Conn.ExecContext(ctx, q, args)
	}
	if c.takeFault("before-insert") {
		return nil, injected
	}
	if len(args) == 0 || len(args)%6 != 0 {
		return nil, errors.New("unexpected canonical INSERT shape")
	}
	var result driver.Result
	var err error
	if c.owner.mode.Name == "current" {
		started := time.Now()
		result, err = c.Conn.ExecContext(ctx, q, args)
		c.owner.stats.observe(int64(len(args)/6), time.Since(started))
	} else {
		rows := make([][]any, len(args)/6)
		for i := range rows {
			rows[i] = make([]any, 6)
			for j := range 6 {
				rows[i][j] = args[i*6+j].Value
			}
		}
		if c.owner.mode.Copy {
			started := time.Now()
			var n int64
			// Native Conn.CopyFrom participates in the transaction already begun
			// by stdlib.BeginTx; database/sql serializes this driver call.
			n, err = c.Conn.Conn().CopyFrom(ctx, pgx.Identifier{c.owner.schema, "entries"}, []string{"stream_id", "ordinal", "source_id", "payload", "payload_hash", "prefix_hash"}, pgx.CopyFromRows(rows))
			c.owner.stats.observe(int64(len(rows)), time.Since(started))
			if err == nil && n != int64(len(rows)) {
				err = errors.New("COPY acknowledged a different row count")
			}
			result = driver.RowsAffected(n)
		} else {
			prefix := "INSERT INTO " + pgx.Identifier{c.owner.schema, "entries"}.Sanitize() + "(stream_id,ordinal,source_id,payload,payload_hash,prefix_hash) VALUES "
			err = contract.InsertRows(ctx, nativeExecutor{c}, prefix, "", rows, c.owner.mode.Chunk, 8<<20, nil)
			result = driver.RowsAffected(len(rows))
		}
	}
	if err != nil {
		return nil, err
	}
	if c.takeFault("after-insert") {
		return nil, injected
	}
	if c.takeFault("disconnect-after-insert") {
		_ = c.Conn.Close() // physical connection loss with an open transaction
		return nil, injected
	}
	return result, nil
}

type nativeExecutor struct{ c *benchConn }

func (x nativeExecutor) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	started := time.Now()
	tag, err := x.c.Conn.Conn().Exec(ctx, q, args...)
	x.c.owner.stats.observe(int64(len(args)/6), time.Since(started))
	return driver.RowsAffected(tag.RowsAffected()), err
}

type benchTx struct {
	driver.Tx
	c *benchConn
}

func (t *benchTx) Commit() error {
	if t.c.takeFault("before-commit") {
		_ = t.Tx.Rollback()
		return injected
	}
	err := t.Tx.Commit()
	if err != nil {
		return err
	}
	if t.c.takeFault("lost-commit-ack") {
		return injected
	}
	if t.c.takeFault("disconnect-after-commit") {
		_ = t.c.Conn.Close()
		return injected
	}
	return nil
}

func createStream(ctx context.Context, admin *pgx.Conn, schema string) error {
	_, err := admin.Exec(ctx, "INSERT INTO "+pgx.Identifier{schema, "streams"}.Sanitize()+"(stream_id,fingerprint,prefix_hash) VALUES('bench','publication-benchmark-v1',$1)", make([]byte, 32))
	return err
}
func resetScratch(ctx context.Context, admin *pgx.Conn, schema string) error {
	_, err := admin.Exec(ctx, "TRUNCATE TABLE "+pgx.Identifier{schema, "entries"}.Sanitize()+", "+pgx.Identifier{schema, "streams"}.Sanitize())
	if err != nil {
		return fmt.Errorf("reset benchmark tables: %w", err)
	}
	return createStream(ctx, admin, schema)
}
