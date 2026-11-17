package canonicalingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

// Five tiny native milestones exercise catch-up from zero, a completed stage
// and an unfinished stage. Exactly 100 canonical observations, never 1M data.
func TestFollowMilestonesCatchesUpIndependentPositions(t *testing.T) {
	dsn := os.Getenv("CANONICAL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("local PostgreSQL required")
	}
	u, err := url.Parse(dsn)
	if err != nil || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Fatal("loopback fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	admin, err := OpenPostgres(ctx, dsn, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("orchestration_milestones_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		t.Fatal(err)
	}
	defer admin.Exec(`DROP DATABASE "` + name + `" WITH(FORCE)`)
	u.Path = "/" + name
	db, err := OpenPostgres(ctx, u.String(), 6)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Store{DB: db}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	old := milestones
	milestones = [...]uint64{20, 40, 60, 80, 100}
	defer func() { milestones = old }()
	if _, err = db.Exec(`ALTER TABLE canonical_ingestion.stages DROP CONSTRAINT stages_target_check; ALTER TABLE canonical_ingestion.stages ADD CHECK(target IN (20,40,60,80,100))`); err != nil {
		t.Fatal(err)
	}
	if err = s.Create(ctx, "fixture", "fingerprint"); err != nil {
		t.Fatal(err)
	}
	limits := Limits{MaxRecords: 100, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
	var entries []Entry
	for n := uint64(1); n <= 100; n++ {
		source := SourceObservation{Dataset: "HAI", File: "fixture.csv", Row: n, Column: 1}
		body, _ := json.Marshal(map[string]any{"dataset": "HAI", "source": source, "site": "P1", "device": "FT01", "type": "FT", "timestamp": "2022-01-01T00:00:00Z", "value": "1", "label": "Normal"})
		entries = append(entries, Entry{n, source.ID(), body})
	}
	if err = s.Append(ctx, "fixture", entries, limits); err != nil {
		t.Fatal(err)
	}
	apply := func(ctx context.Context, tx *sql.Tx, p Progress, es []Entry) ([]string, []byte, error) {
		ids := make([]string, len(es))
		for i, e := range es {
			ids[i] = fmt.Sprint(e.Ordinal)
		}
		return ids, []byte(`{}`), nil
	}
	for i := 0; i < 3; i++ {
		c := Consumer{Store: s, ID: fmt.Sprint(i), StreamID: "fixture", Fingerprint: "fingerprint", ConfigHash: "native", DeferCompletion: i == 2}
		if err = c.Open(ctx); err != nil {
			t.Fatal(err)
		}
		var completion func(context.Context) error
		if c.DeferCompletion {
			completion = func(ctx context.Context) error {
				return c.CompleteStage(ctx, func(context.Context, *sql.Tx, Progress) error { return nil })
			}
		}
		if i > 0 {
			if _, err = c.Begin(ctx, 20); err != nil {
				t.Fatal(err)
			}
			if _, err = c.Step(ctx, s, limits, apply); err != nil {
				t.Fatal(err)
			}
			if completion != nil {
				if err = completion(ctx); err != nil {
					t.Fatal(err)
				}
			}
		}
		if i == 2 {
			if _, err = c.Begin(ctx, 40); err != nil {
				t.Fatal(err)
			}
			small := limits
			small.MaxRecords = 5
			if _, err = c.Step(ctx, s, small, apply); err != nil {
				t.Fatal(err)
			}
		}
		p, err := c.FollowMilestones(ctx, s, 100, limits, apply, time.Millisecond, nil, completion)
		if err != nil || p.Committed != 100 {
			t.Fatal(p, err)
		}
		var completed int
		if err = db.QueryRow(`SELECT count(*) FROM canonical_ingestion.stages WHERE consumer_id=$1 AND completed_at IS NOT NULL`, c.ID).Scan(&completed); err != nil || completed != 5 {
			t.Fatal("native milestone skipped", completed, err)
		}
		if _, err = c.ValidatePrefix(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
}
