package storage_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"dhtbacked/internal/config"
	"dhtbacked/storage"
)

func sampleRecord(id string, epoch uint64, logType string, leaf uint64) storage.Record {
	return storage.Record{
		RecordID:          id,
		Epoch:             epoch,
		LogType:           logType,
		LeafIndex:         leaf,
		ProducerID:        "producer-" + logType,
		TimestampNanos:    1789101000500000000,
		C:                 []byte{1, 2, 3, 4},
		T:                 []byte{5, 6},
		AD:                []byte{7, 8},
		D:                 sha256.Sum256([]byte(id)),
		Sigma:             []byte{9},
		ProducerPublicKey: []byte{10},
	}
}

// Q43: the schema is exactly the twelve stated columns, all cryptographic byte
// strings are raw BYTEA, and the only additional constraint is
// UNIQUE (epoch, log_type, leaf_index).
func TestSchemaMatchesQ43(t *testing.T) {
	s := ddlOnly(storage.Schema())
	for _, col := range []string{
		"record_id            TEXT   PRIMARY KEY",
		"epoch                BIGINT NOT NULL",
		"log_type             TEXT   NOT NULL",
		"leaf_index           BIGINT NOT NULL",
		"producer_id          TEXT   NOT NULL",
		"timestamp_ns         BIGINT NOT NULL",
		"c                    BYTEA  NOT NULL",
		"t                    BYTEA  NOT NULL",
		"ad                   BYTEA  NOT NULL",
		"d                    BYTEA  NOT NULL",
		"sigma                BYTEA  NOT NULL",
		"producer_public_key  BYTEA  NOT NULL",
		"UNIQUE (epoch, log_type, leaf_index)",
	} {
		if !strings.Contains(s, col) {
			t.Errorf("schema is missing %q", col)
		}
	}
	// §0.6 NAMED EXCEPTION 1: no other index of any kind.
	if regexp.MustCompile(`(?i)create\s+(unique\s+)?index`).MatchString(s) {
		t.Error("schema creates a secondary index; only UNIQUE (epoch, log_type, leaf_index) is authorized")
	}
	if strings.Count(s, "UNIQUE") != 1 {
		t.Error("schema declares more than one UNIQUE constraint")
	}
	if regexp.MustCompile(`(?i)\b(hex|base64|encode)\b`).MatchString(s) {
		t.Error("schema stores cryptographic bytes as text")
	}
}

func TestMemoryStoreEnforcesBothUniqueConstraints(t *testing.T) {
	ctx := context.Background()
	m := storage.NewMemory()
	if err := m.Insert(ctx, sampleRecord("000000000000", 1, "access", 0)); err != nil {
		t.Fatal(err)
	}
	if err := m.Insert(ctx, sampleRecord("000000000000", 1, "api", 5)); err == nil {
		t.Error("duplicate record_id accepted")
	}
	if err := m.Insert(ctx, sampleRecord("000000000001", 1, "access", 0)); err == nil {
		t.Error("duplicate (epoch, log_type, leaf_index) accepted")
	}
	if err := m.Insert(ctx, sampleRecord("000000000002", 2, "access", 0)); err != nil {
		t.Errorf("same leaf index in a different epoch rejected: %v", err)
	}
	if err := m.Insert(ctx, sampleRecord("000000000003", 1, "api", 0)); err != nil {
		t.Errorf("same leaf index for a different log type rejected: %v", err)
	}
	got, err := m.Get(ctx, "000000000000")
	if err != nil {
		t.Fatal(err)
	}
	if got.LeafIndex != 0 || got.Epoch != 1 || got.LogType != "access" {
		t.Error("stored record did not round trip")
	}
	if _, err := m.Get(ctx, "absent"); err != storage.ErrNotFound {
		t.Errorf("Get on an absent record returned %v", err)
	}
}

// openPostgres connects to the PostgreSQL 17 server named by DHT_POSTGRES_DSN.
// It skips when the variable is unset so pure-logic tests still run.
func openPostgres(t *testing.T) *storage.Postgres {
	t.Helper()
	dsn := os.Getenv(config.EnvPostgresDSN)
	if dsn == "" {
		t.Skipf("%s is not set; skipping the PostgreSQL integration test", config.EnvPostgresDSN)
	}
	ctx := context.Background()
	pg, err := storage.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { pg.Close(context.Background()) })
	if err := pg.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return pg
}

func adminConn(t *testing.T) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv(config.EnvPostgresDSN)
	if dsn == "" {
		t.Skipf("%s is not set; skipping the PostgreSQL integration test", config.EnvPostgresDSN)
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("admin connect: %v", err)
	}
	t.Cleanup(func() { c.Close(context.Background()) })
	return c
}

func clearEpoch(t *testing.T, epoch uint64) {
	t.Helper()
	c := adminConn(t)
	if _, err := c.Exec(context.Background(), "DELETE FROM records WHERE epoch = $1", int64(epoch)); err != nil {
		t.Fatalf("clear: %v", err)
	}
}

func TestPostgresRejectsDuplicateLeafPosition(t *testing.T) {
	pg := openPostgres(t)
	ctx := context.Background()
	const epoch = uint64(9001)
	clearEpoch(t, epoch)
	if err := pg.Insert(ctx, sampleRecord("pg-dup-a", epoch, "access", 3)); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := pg.Insert(ctx, sampleRecord("pg-dup-b", epoch, "access", 3))
	if err == nil {
		t.Fatal("the database accepted a duplicate (epoch, log_type, leaf_index)")
	}
	if !strings.Contains(err.Error(), "23505") && !strings.Contains(strings.ToLower(err.Error()), "unique") {
		t.Fatalf("duplicate insert failed for the wrong reason: %v", err)
	}
	if err := pg.Insert(ctx, sampleRecord("pg-dup-a", epoch, "api", 4)); err == nil {
		t.Fatal("the database accepted a duplicate record_id")
	}
	// The same leaf index is legitimate in another tree.
	if err := pg.Insert(ctx, sampleRecord("pg-dup-c", epoch, "api", 3)); err != nil {
		t.Fatalf("same leaf index for a different log type rejected: %v", err)
	}
	clearEpoch(t, epoch)
}

func TestPostgresRoundTripPreservesRawBytes(t *testing.T) {
	pg := openPostgres(t)
	ctx := context.Background()
	const epoch = uint64(9002)
	clearEpoch(t, epoch)
	r := sampleRecord("pg-roundtrip", epoch, "security", 11)
	r.C = []byte{0x00, 0xFF, 0x10, 0x7F, 0x80}
	r.AD = []byte{0xDE, 0xAD, 0xBE, 0xEF}
	if err := pg.Insert(ctx, r); err != nil {
		t.Fatalf("insert: %v", err)
	}
	got, err := pg.Get(ctx, "pg-roundtrip")
	if err != nil {
		t.Fatal(err)
	}
	if string(got.C) != string(r.C) || string(got.AD) != string(r.AD) || got.D != r.D {
		t.Fatal("raw bytes did not survive the round trip")
	}
	if got.LeafIndex != r.LeafIndex || got.Epoch != r.Epoch {
		t.Fatal("epoch or leaf index did not survive the round trip")
	}
	if _, err := pg.Get(ctx, "pg-absent"); err != storage.ErrNotFound {
		t.Errorf("Get on an absent record returned %v", err)
	}
	clearEpoch(t, epoch)
}

// CopyRecords must load a batch through COPY with the same raw-byte and
// BIGINT semantics as Insert, reject out-of-range integers before touching the
// database, and be a no-op for an empty batch.
func TestPostgresCopyRecordsRoundTrip(t *testing.T) {
	pg := openPostgres(t)
	ctx := context.Background()
	const epoch = uint64(9003)
	clearEpoch(t, epoch)
	defer clearEpoch(t, epoch)

	if err := pg.CopyRecords(ctx, nil); err != nil {
		t.Fatalf("empty COPY: %v", err)
	}
	batch := make([]storage.Record, 0, 50)
	for i := uint64(0); i < 50; i++ {
		r := sampleRecord(fmt.Sprintf("pg-copy-%03d", i), epoch, "access", i)
		r.C = []byte{0x00, 0xFF, byte(i)}
		batch = append(batch, r)
	}
	if err := pg.CopyRecords(ctx, batch); err != nil {
		t.Fatalf("COPY: %v", err)
	}
	for _, want := range batch {
		got, err := pg.Get(ctx, want.RecordID)
		if err != nil {
			t.Fatalf("Get %s: %v", want.RecordID, err)
		}
		if string(got.C) != string(want.C) || got.D != want.D ||
			got.LeafIndex != want.LeafIndex || got.Epoch != want.Epoch {
			t.Fatalf("%s did not survive COPY", want.RecordID)
		}
	}

	// A duplicate leaf position fails the whole COPY atomically.
	dup := []storage.Record{
		sampleRecord("pg-copy-new", epoch, "api", 0),
		sampleRecord("pg-copy-dup", epoch, "access", 7),
	}
	if err := pg.CopyRecords(ctx, dup); err == nil {
		t.Fatal("COPY accepted a duplicate (epoch, log_type, leaf_index)")
	}
	if _, err := pg.Get(ctx, "pg-copy-new"); err != storage.ErrNotFound {
		t.Fatalf("failed COPY left a partial row behind: %v", err)
	}

	tooBig := sampleRecord("pg-copy-big", epoch, "access", 1<<63)
	if err := pg.CopyRecords(ctx, []storage.Record{tooBig}); err == nil {
		t.Fatal("COPY accepted a leaf index outside BIGINT")
	}
}

// §0.6 NAMED EXCEPTION 1: the live table carries exactly the primary key and
// UNIQUE (epoch, log_type, leaf_index) — and nothing else.
// ddlOnly strips SQL comment lines so the assertions below examine executable
// DDL rather than the explanatory header.
func ddlOnly(s string) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func TestPostgresHasOnlyTheAuthorizedIndexes(t *testing.T) {
	openPostgres(t)
	c := adminConn(t)
	rows, err := c.Query(context.Background(),
		"SELECT indexdef FROM pg_indexes WHERE tablename = 'records' ORDER BY indexname")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var defs []string
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			t.Fatal(err)
		}
		defs = append(defs, def)
	}
	if len(defs) != 2 {
		t.Fatalf("records carries %d indexes, want exactly 2 (primary key + the one authorized unique constraint): %v", len(defs), defs)
	}
	var sawPK, sawUnique bool
	for _, def := range defs {
		norm := strings.ToLower(strings.ReplaceAll(def, " ", ""))
		switch {
		case strings.Contains(norm, "(record_id)"):
			sawPK = true
		case strings.Contains(norm, "(epoch,log_type,leaf_index)"):
			sawUnique = true
		default:
			t.Errorf("unauthorized index: %s", def)
		}
	}
	if !sawPK || !sawUnique {
		t.Fatalf("expected indexes are missing: %v", defs)
	}
}

// Q46: startup fails fast when PostgreSQL is unreachable.
func TestOpenFailsFastOnUnreachablePostgres(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := storage.Open(ctx, "postgres://nobody:nobody@127.0.0.1:1/nodb?connect_timeout=2"); err == nil {
		t.Fatal("Open succeeded against an unreachable server")
	}
	if _, err := storage.Open(ctx, "not a dsn"); err == nil {
		t.Fatal("Open succeeded with a malformed DSN")
	}
}
