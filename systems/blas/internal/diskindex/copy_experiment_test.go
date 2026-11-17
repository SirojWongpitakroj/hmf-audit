package diskindex

import (
	"context"
	"fmt"
	"testing"
	"time"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
	"github.com/jackc/pgx/v5"
)

// Protocol feasibility experiment only: native Consumer.Step owns a sql.Tx,
// which exposes no Raw/CopyFrom hook. Never open another production connection
// and mistake it for that transaction. No production COPY switch is provided.
func TestBoundedCopyExperiment(t *testing.T) {
	ctx := context.Background()
	var snapshots [2]string
	for mode := range 2 {
		db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
		if err := (Store{Q: db, Limits: Limits{PageBytes: 4096, BitmapBytes: 4096}}).Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		rows := make([][]any, 10000) // synthetic SQL rows, zero canonical observations
		payload := make([]byte, 1024)
		for i := range payload {
			payload[i] = byte(i % 251)
		}
		for i := range rows {
			rows[i] = []any{"copy-fixture", "file/0/iimt", fmt.Sprintf("node/%d", i), payload}
		}
		started := time.Now()
		calls := 0
		if mode == 0 {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			metrics := &contract.IngestMetrics{}
			if err := contract.InsertRows(ctx, tx, `INSERT INTO blas.index_pages(scope,tree,page,payload) VALUES `, "", rows, 4000, 8<<20, metrics); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			calls = int(metrics.SQLBatches.Load())
		} else {
			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(ctx)
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			for next := 0; next < len(rows); next += 4000 {
				batch := rows[next:min(next+4000, len(rows))]
				if n, err := tx.CopyFrom(ctx, pgx.Identifier{"blas", "index_pages"}, []string{"scope", "tree", "page", "payload"}, pgx.CopyFromRows(batch)); err != nil || n != int64(len(batch)) {
					t.Fatal("COPY count/failure", n, err)
				}
				calls++
			}
			if err := tx.Commit(ctx); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("COPY_EXPERIMENT copy=%v rows=10000 payload_bytes=10240000 calls=%d seconds=%.6f", mode == 1, calls, time.Since(started).Seconds())
		snapshots[mode] = testfixture.Snapshot(t, db, `SELECT * FROM blas.index_pages ORDER BY scope,tree,page`)
		if mode == 1 {
			conn, err := pgx.Connect(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close(ctx)
			tx, err := conn.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(ctx, `DELETE FROM blas.index_pages`); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"blas", "index_pages"}, []string{"scope", "tree", "page", "payload"}, pgx.CopyFromRows(rows[:4000])); err != nil {
				t.Fatal(err)
			}
			if _, err := tx.CopyFrom(ctx, pgx.Identifier{"blas", "index_pages"}, []string{"scope", "tree", "page", "payload"}, pgx.CopyFromRows(rows[:1])); err == nil {
				t.Fatal("COPY duplicate did not abort")
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if snapshots[mode] != testfixture.Snapshot(t, db, `SELECT * FROM blas.index_pages ORDER BY scope,tree,page`) {
				t.Fatal("failed COPY changed committed state")
			}
		}
	}
	if snapshots[0] != snapshots[1] {
		t.Fatal("COPY changed row keys/bytes")
	}
}
