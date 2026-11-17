package canonicalingestion

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestCrashHelper(t *testing.T) {
	if os.Getenv("CANONICAL_CRASH_HELPER") != "1" {
		return
	}
	db, err := OpenPostgres(context.Background(), os.Getenv("CANONICAL_TEST_POSTGRES_DSN"), 4)
	if err != nil {
		t.Fatal(err)
	}
	id := os.Getenv("CANONICAL_CRASH_ID")
	c := Consumer{Store: Store{DB: db}, ID: id, StreamID: id, Fingerprint: "fixture-v1", ConfigHash: "native-v1", DeferCompletion: true}
	lease, err := c.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	_, err = c.Step(context.Background(), c.Store, Limits{MaxRecords: 3, MaxBatchBytes: 8192, MaxRecordBytes: 1024}, func(ctx context.Context, tx *sql.Tx, p Progress, e []Entry) ([]string, []byte, error) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO canonical_ingestion.crash_probe(id) VALUES($1)`, id); err != nil {
			t.Fatal(err)
		}
		os.Exit(27)
		return nil, nil, nil
	})
	t.Fatal("crash injection did not exit", err)
}
func TestProcessCrashRollsBackNativeState(t *testing.T) {
	dsn := os.Getenv("CANONICAL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CANONICAL_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := OpenPostgres(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Store{DB: db}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS canonical_ingestion.crash_probe(id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("crash-test-%d", time.Now().UnixNano())
	defer func() {
		db.Exec(`DELETE FROM canonical_ingestion.crash_probe WHERE id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.mappings WHERE consumer_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.stages WHERE consumer_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.consumers WHERE consumer_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.entries WHERE stream_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.streams WHERE stream_id=$1`, id)
	}()
	if err = s.Create(ctx, id, "fixture-v1"); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(ctx, id, []Entry{entry(1), entry(2), entry(3)}, Limits{MaxRecords: 3, MaxBatchBytes: 8192, MaxRecordBytes: 1024}); err != nil {
		t.Fatal(err)
	}
	c := Consumer{Store: s, ID: id, StreamID: id, Fingerprint: "fixture-v1", ConfigHash: "native-v1", DeferCompletion: true}
	if err = c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Begin(ctx, 1000000); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(executable, "-test.run=^TestCrashHelper$", "-test.count=1")
	cmd.Env = append(os.Environ(), "CANONICAL_CRASH_HELPER=1", "CANONICAL_CRASH_ID="+id)
	if err = cmd.Run(); err == nil {
		t.Fatal("crashing child unexpectedly succeeded")
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM canonical_ingestion.crash_probe WHERE id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("uncommitted native mutation survived process crash", count, err)
	}
	p, err := c.Progress(ctx)
	if err != nil || p.Committed != 0 {
		t.Fatal("cursor advanced after process crash", p, err)
	}
	lease, err := c.Acquire(ctx)
	if err != nil {
		t.Fatal("process crash retained exclusive lease", err)
	}
	lease.Close()
}
