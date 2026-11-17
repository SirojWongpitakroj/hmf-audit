package canonicalingestion_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

type crashSQLKey struct{}
type crashTracer struct {
	phase          string
	native, mapped atomic.Bool
}

func (t *crashTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	query := strings.ToUpper(data.SQL)
	if t.phase == "before-cursor" && t.native.Load() && strings.HasPrefix(query, "UPDATE CANONICAL_INGESTION.CONSUMERS SET COMMITTED") {
		os.Exit(27)
	}
	return context.WithValue(ctx, crashSQLKey{}, query)
}
func (t *crashTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	if data.Err != nil {
		return
	}
	query, _ := ctx.Value(crashSQLKey{}).(string)
	if strings.HasPrefix(query, "INSERT INTO CANONICAL_INGESTION.NATIVE_CRASH_PROBE") {
		t.native.Store(true)
		if t.phase == "native-written" {
			os.Exit(27)
		}
	}
	if strings.HasPrefix(query, "INSERT INTO CANONICAL_INGESTION.MAPPINGS") {
		t.mapped.Store(true)
		if t.phase == "mapped" {
			os.Exit(27)
		}
	}
	if t.phase == "source-read" && strings.HasPrefix(query, "SELECT ORDINAL,SOURCE_ID,OCTET_LENGTH(PAYLOAD)") {
		os.Exit(27)
	}
	if t.phase == "after-cursor-update" && strings.HasPrefix(query, "UPDATE CANONICAL_INGESTION.CONSUMERS SET COMMITTED") {
		os.Exit(27)
	}
	if t.phase == "after-commit" && t.native.Load() && query == "COMMIT" {
		os.Exit(27)
	}
}
func probeApply(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
	ids := make([]string, len(entries))
	for i, e := range entries {
		if e.Ordinal != p.Committed+uint64(i)+1 {
			return nil, nil, fmt.Errorf("gap")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO canonical_ingestion.native_crash_probe(ordinal,payload) VALUES($1,$2)`, int64(e.Ordinal), []byte(e.Record)); err != nil {
			return nil, nil, err
		}
		ids[i] = fmt.Sprint(e.Ordinal)
	}
	return ids, []byte("bounded-checkpoint-v1"), nil
}
func TestConcurrentCrashChild(t *testing.T) {
	phase := os.Getenv("INGESTION_CRASH_PHASE")
	if phase == "" {
		return
	}
	config, err := pgx.ParseConfig(os.Getenv("INGESTION_CRASH_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	config.Tracer = &crashTracer{phase: phase}
	db := stdlib.OpenDB(*config)
	defer db.Close()
	db.SetMaxOpenConns(4)
	c := contract.Consumer{Store: contract.Store{DB: db}, ID: "native-fault", StreamID: "fixture", Fingerprint: "fixture-v1", ConfigHash: "native-v1", DeferCompletion: true}
	e := contract.DefaultExecution()
	e.CPUWorkers = 4
	c.Execution = &e
	lease, err := c.Acquire(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	_, err = c.Step(context.Background(), c.Store, contract.Limits{MaxRecords: 3, MaxBatchBytes: 8192, MaxRecordBytes: 1024}, func(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
		if phase == "prepared" {
			os.Exit(27)
		}
		return probeApply(ctx, tx, p, entries)
	})
	t.Fatal("crash point was not reached", err)
}

func TestConcurrentProcessCrashPreservesContiguousCursor(t *testing.T) {
	for _, phase := range []string{"source-read", "prepared", "native-written", "mapped", "before-cursor", "after-cursor-update", "after-commit"} {
		t.Run(phase, func(t *testing.T) {
			db, dsn := testfixture.Database(t, "CANONICAL_TEST_POSTGRES_DSN")
			ctx := context.Background()
			source := contract.Store{DB: db}
			if err := source.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`CREATE TABLE canonical_ingestion.native_crash_probe(ordinal bigint PRIMARY KEY,payload bytea NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			limits := contract.Limits{MaxRecords: 3, MaxBatchBytes: 8192, MaxRecordBytes: 1024}
			if err := source.Create(ctx, "fixture", "fixture-v1"); err != nil {
				t.Fatal(err)
			}
			if err := source.Append(ctx, "fixture", testfixture.Entries(1, 3), limits); err != nil {
				t.Fatal(err)
			}
			c := contract.Consumer{Store: source, ID: "native-fault", StreamID: "fixture", Fingerprint: "fixture-v1", ConfigHash: "native-v1", DeferCompletion: true}
			if err := c.Open(ctx); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Begin(ctx, 1000000); err != nil {
				t.Fatal(err)
			}
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			child := exec.Command(executable, "-test.run=^TestConcurrentCrashChild$", "-test.count=1")
			child.Env = append(os.Environ(), "INGESTION_CRASH_PHASE="+phase, "INGESTION_CRASH_DSN="+dsn)
			if output, err := child.CombinedOutput(); err == nil {
				t.Fatal("child did not crash", string(output))
			} else if status, ok := err.(*exec.ExitError); !ok || status.ExitCode() != 27 {
				t.Fatal("unexpected child failure", err, string(output))
			}
			p, err := c.Progress(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(0)
			if phase == "after-commit" {
				want = 3
			}
			if p.Committed != want {
				t.Fatalf("crash advanced/lost cursor: %d wanted %d", p.Committed, want)
			}
			var records, mappings int
			if err := db.QueryRow(`SELECT count(*) FROM canonical_ingestion.native_crash_probe`).Scan(&records); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM canonical_ingestion.mappings`).Scan(&mappings); err != nil {
				t.Fatal(err)
			}
			if records != int(want) || mappings != int(want) {
				t.Fatal("native/mapping/cursor atomicity lost", records, mappings, want)
			}
			if err := source.Append(ctx, "fixture", testfixture.Entries(4, 3), limits); err != nil {
				t.Fatal(err)
			}
			for p.Committed < 6 {
				next := p.Committed + 1
				p, err = c.Step(ctx, source, limits, func(ctx context.Context, tx *sql.Tx, p contract.Progress, entries []contract.Entry) ([]string, []byte, error) {
					if entries[0].Ordinal != next {
						return nil, nil, fmt.Errorf("resume skipped ordinal")
					}
					return probeApply(ctx, tx, p, entries)
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			var count, distinct int
			var low, high int64
			if err := db.QueryRow(`SELECT count(*),count(DISTINCT source_id),min(ordinal),max(ordinal) FROM canonical_ingestion.mappings`).Scan(&count, &distinct, &low, &high); err != nil || count != 6 || distinct != 6 || low != 1 || high != 6 {
				t.Fatal("replay duplicate/gap", count, distinct, low, high, err)
			}
			deadline := time.Now().Add(time.Second)
			for {
				lease, err := c.Acquire(ctx)
				if err == nil {
					lease.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("process crash retained lease", err)
				}
				time.Sleep(time.Millisecond)
			}
		})
	}
}
