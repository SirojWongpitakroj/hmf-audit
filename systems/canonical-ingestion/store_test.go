package canonicalingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func entry(n uint64) Entry {
	s := SourceObservation{Dataset: "WADI", File: "WADI/raw/source.csv", Row: n, Column: 2}
	b, _ := json.Marshal(struct {
		Dataset   string            `json:"dataset"`
		Source    SourceObservation `json:"source"`
		Value     string            `json:"value"`
		Site      string            `json:"site"`
		Device    string            `json:"device"`
		Type      string            `json:"type"`
		Timestamp time.Time         `json:"timestamp"`
		Label     string            `json:"label"`
	}{"WADI", s, "1", "site", "sensor", "sensor", time.Unix(int64(n), 0).UTC(), "normal"})
	return Entry{Ordinal: n, SourceID: s.ID(), Record: b}
}
func TestIdentity(t *testing.T) {
	e := entry(1)
	if err := e.Validate(4096); err != nil {
		t.Fatal(err)
	}
	changed := entry(2)
	if changed.SourceID == e.SourceID {
		t.Fatal("equal values collapsed")
	}
	e.SourceID = changed.SourceID
	if e.Validate(4096) == nil {
		t.Fatal("mismatched identity accepted")
	}
	for _, mode := range []string{"1M", "10M", "50M", "100M", "250M"} {
		n, err := Target(mode)
		if err != nil || n == 0 {
			t.Fatal(mode, err)
		}
	}
	if _, err := Target("all"); err == nil {
		t.Fatal("automatic progression accepted")
	}
}

func TestDurableContract(t *testing.T) {
	dsn := os.Getenv("CANONICAL_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("CANONICAL_TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(4)
	s := Store{DB: db}
	if err = s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	id := fmt.Sprintf("contract-test-%d", time.Now().UnixNano())
	defer func() {
		db.Exec(`DELETE FROM canonical_ingestion.mappings WHERE consumer_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.stages WHERE consumer_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.consumers WHERE consumer_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.entries WHERE stream_id=$1`, id)
		db.Exec(`DELETE FROM canonical_ingestion.streams WHERE stream_id=$1`, id)
	}()
	if err = s.Create(ctx, id, "fixture-v1"); err != nil {
		t.Fatal(err)
	}
	limits := Limits{MaxRecords: 10, MaxBatchBytes: 4096, MaxRecordBytes: 512}
	var es []Entry
	for i := uint64(1); i <= 10; i++ {
		es = append(es, entry(i))
	}
	if err = s.Append(ctx, id, es, limits); err != nil {
		t.Fatal(err)
	}
	if err = s.Append(ctx, id, es, limits); err != nil {
		t.Fatal("identical replay", err)
	}
	bad := entry(1)
	bad.Record = append(bad.Record[:len(bad.Record)-1], []byte(`,"extra":true}`)...)
	if err = s.Append(ctx, id, []Entry{bad}, limits); err == nil {
		t.Fatal("changed immutable payload accepted")
	}
	// Cross-batch source uniqueness belongs to the producer journal; the stream
	// itself still rejects a repeated source inside one batch.
	duplicate := entry(11)
	duplicate.Ordinal = 12
	if err = s.Append(ctx, id, []Entry{entry(11), duplicate}, limits); err == nil {
		t.Fatal("duplicate source in one batch accepted")
	}
	c := Consumer{Store: s, ID: id, StreamID: id, Fingerprint: "fixture-v1", ConfigHash: "native-v1", DeferCompletion: true}
	if err = c.Open(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Begin(ctx, 10000000); err == nil {
		t.Fatal("skipped predecessor accepted")
	}
	if _, err = c.Begin(ctx, 1000000); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Begin(ctx, 10000000); err == nil {
		t.Fatal("changed unfinished target accepted")
	}
	// A small internal target tests the exact final-batch mechanism without
	// writing a million fixture rows; production Begin accepts only five targets.
	if _, err = db.Exec(`UPDATE canonical_ingestion.consumers SET target=7 WHERE consumer_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	failed := func(ctx context.Context, tx *sql.Tx, p Progress, es []Entry) ([]string, []byte, error) {
		_, e := tx.ExecContext(ctx, `INSERT INTO canonical_ingestion.mappings(consumer_id,ordinal,source_id,native_id,payload_hash) VALUES($1,99,'rolled-back','rolled-back',$2)`, id, make([]byte, 32))
		if e != nil {
			return nil, nil, e
		}
		return nil, nil, errors.New("injected before commit")
	}
	if _, err = c.Step(ctx, s, limits, failed); err == nil {
		t.Fatal("fault not returned")
	}
	p, err := c.Progress(ctx)
	if err != nil || p.Committed != 0 {
		t.Fatal("advanced failed cursor", p, err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM canonical_ingestion.mappings WHERE consumer_id=$1`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("native mutation escaped rollback", err, count)
	}
	apply := func(_ context.Context, _ *sql.Tx, p Progress, es []Entry) ([]string, []byte, error) {
		ids := make([]string, len(es))
		for i, e := range es {
			ids[i] = fmt.Sprintf("native:%d", e.Ordinal)
		}
		return ids, []byte(fmt.Sprint(es[len(es)-1].Ordinal)), nil
	}
	limits.MaxRecords = 3
	p, err = c.Step(ctx, s, limits, apply)
	if err != nil || p.Committed != 3 {
		t.Fatal(p, err)
	}
	// A fresh pool and Consumer have no access to the first process's RAM.
	reopened, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	resumed := c
	resumed.Store = Store{DB: reopened}
	if err = resumed.Open(ctx); err != nil {
		t.Fatal(err)
	}
	p, err = resumed.Step(ctx, s, limits, apply)
	if err != nil || p.Committed != 6 {
		t.Fatal(p, err)
	}
	p, err = resumed.Step(ctx, s, limits, apply)
	if err != nil || p.Committed != 7 || string(p.Checkpoint) != "7" {
		t.Fatal("exact stop", p, err)
	}
	p, err = resumed.Step(ctx, s, limits, apply)
	if err != nil || p.Committed != 7 {
		t.Fatal("no-op", p, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM canonical_ingestion.mappings WHERE consumer_id=$1`, id).Scan(&count); err != nil || count != 7 {
		t.Fatal(count, err)
	}
	different := resumed
	readA, err := c.AcquireRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	readB, err := resumed.AcquireRead(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if write, e := c.Acquire(ctx); e == nil {
		write.Close()
		t.Fatal("ingestion acquired lease while audits held dataset fixed")
	}
	readA.Close()
	readB.Close()
	write, err := c.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	write.Close()
	different.ConfigHash = "changed"
	if err = different.Open(ctx); err == nil {
		t.Fatal("geometry change accepted")
	}
}
