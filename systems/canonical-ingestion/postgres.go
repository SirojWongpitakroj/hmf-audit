package canonicalingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func OpenPostgres(ctx context.Context, dsn string, maxConnections int) (*sql.DB, error) {
	if dsn == "" || maxConnections < 2 {
		return nil, errors.New("PostgreSQL DSN and pool size >=2 required")
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	if m := metricsInContext(ctx); m != nil {
		cfg.Tracer = sqlMetricsTracer{m}
		m.SQLTraced.Store(true)
	}
	db := stdlib.OpenDB(*cfg)
	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(maxConnections)
	ping, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err = db.PingContext(ping); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

type Lease struct {
	conn   *sql.Conn
	key    string
	shared bool
}

func (l *Lease) Check(ctx context.Context) error {
	if l == nil || l.conn == nil {
		return errors.New("dataset lease is closed")
	}
	var one int
	return l.conn.QueryRowContext(ctx, `SELECT 1`).Scan(&one)
}

// Acquire excludes another ingestion or measurement for this consumer. The
// PostgreSQL session lock is automatically released if the process dies.
func (c Consumer) Acquire(ctx context.Context) (*Lease, error) {
	return c.acquire(ctx, false)
}

// AcquireRead holds the dataset fixed while allowing an orchestrator and its
// audit subprocess to share read leases. Incremental ingestion is excluded.
func (c Consumer) AcquireRead(ctx context.Context) (*Lease, error) { return c.acquire(ctx, true) }
func (c Consumer) acquire(ctx context.Context, shared bool) (*Lease, error) {
	conn, err := c.Store.DB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	key := "canonical-consumer/" + c.ID
	var ok bool
	query := `SELECT pg_try_advisory_lock(hashtextextended($1,0))`
	if shared {
		query = `SELECT pg_try_advisory_lock_shared(hashtextextended($1,0))`
	}
	err = conn.QueryRowContext(ctx, query, key).Scan(&ok)
	if err != nil || !ok {
		conn.Close()
		if err == nil {
			err = fmt.Errorf("consumer %s is busy with ingestion or measurement", c.ID)
		}
		return nil, err
	}
	return &Lease{conn: conn, key: key, shared: shared}, nil
}
func (l *Lease) Close() error {
	if l == nil || l.conn == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := `SELECT pg_advisory_unlock(hashtextextended($1,0))`
	if l.shared {
		query = `SELECT pg_advisory_unlock_shared(hashtextextended($1,0))`
	}
	_, err := l.conn.ExecContext(ctx, query, l.key)
	err = errors.Join(err, l.conn.Close())
	l.conn = nil
	return err
}
