// Package testfixture supplies local-only isolated compatibility fixtures.
// It is never called by an ingestion executable.
package testfixture

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sync/atomic"
	"testing"
	"time"

	contract "canonicalingestion"
)

var sequence atomic.Uint64

func Database(t testing.TB, env string) (*sql.DB, string) {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " unset; isolated PostgreSQL test unavailable")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1" {
		t.Fatal("concurrency fixture refuses a non-loopback database")
	}
	ctx := context.Background()
	admin, err := contract.OpenPostgres(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	var nonce [8]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("ingestion_concurrency_%x_%d", nonce, sequence.Add(1))
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE "`+name+`"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := contract.OpenPostgres(ctx, u.String(), 16)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Close()
		if _, err := admin.Exec(`DROP DATABASE "` + name + `" WITH(FORCE)`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	return db, u.String()
}

func Entry(n uint64) contract.Entry {
	dataset := []string{"WADI", "HAI", "SWAT"}[n%3]
	source := contract.SourceObservation{Dataset: dataset, File: "fixture.csv", Row: n, Column: 2}
	stamp := time.Unix(1700000000+int64(n), 0).UTC()
	if os.Getenv("CONCURRENT_SOURCE_PATTERN") != "monotonic" {
		year := map[string]int{"WADI": 2017, "HAI": 2022, "SWAT": 2015}[dataset]
		stamp = time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(n/3) * time.Second)
	}
	payload, _ := json.Marshal(struct {
		Dataset   string                     `json:"dataset"`
		Site      string                     `json:"site"`
		Device    string                     `json:"device"`
		Type      string                     `json:"type"`
		Timestamp time.Time                  `json:"timestamp"`
		Value     string                     `json:"value"`
		Label     string                     `json:"label"`
		Source    contract.SourceObservation `json:"source"`
	}{dataset, dataset + ":P1", fmt.Sprintf("sensor-%d", n%11), "sensor", stamp, fmt.Sprint(n), "normal", source})
	return contract.Entry{Ordinal: n, SourceID: source.ID(), Record: payload}
}

func Entries(start uint64, count int) []contract.Entry {
	out := make([]contract.Entry, count)
	for i := range out {
		out[i] = Entry(start + uint64(i))
	}
	return out
}

// Snapshot hashes ordered rows incrementally, never materializing history.
// Each query must explicitly exclude nondeterministic wall-clock columns.
func Snapshot(t testing.TB, db *sql.DB, queries ...string) string {
	t.Helper()
	h := sha256.New()
	for _, query := range queries {
		rows, err := db.Query(`SELECT row_to_json(s)::text FROM (` + query + `) s`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var b string
			if err := rows.Scan(&b); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			fmt.Fprintln(h, b)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}
