package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	contract "canonicalingestion"
	"github.com/jackc/pgx/v5"
)

// Always verify through an independent connection, not the writer's returned
// in-memory result. This includes every row's exact payload and prefix hash.
func verifyRows(ctx context.Context, admin *pgx.Conn, schema string, expected []contract.Entry) error {
	var committed int64
	var hash []byte
	if err := admin.QueryRow(ctx, "SELECT committed,prefix_hash FROM "+pgx.Identifier{schema, "streams"}.Sanitize()+" WHERE stream_id='bench'").Scan(&committed, &hash); err != nil {
		return err
	}
	var prefix [32]byte
	rows, err := admin.Query(ctx, "SELECT ordinal,source_id,payload,payload_hash,prefix_hash FROM "+pgx.Identifier{schema, "entries"}.Sanitize()+" WHERE stream_id='bench' ORDER BY ordinal")
	if err != nil {
		return err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var ordinal int64
		var source string
		var payload, ph, pr []byte
		if err = rows.Scan(&ordinal, &source, &payload, &ph, &pr); err != nil {
			return err
		}
		if n >= len(expected) {
			return errors.New("unexpected extra durable benchmark row")
		}
		e := expected[n]
		wantPayload := sha256.Sum256(e.Record)
		prefix = prefixHash(prefix, e)
		if uint64(ordinal) != e.Ordinal || source != e.SourceID || !bytes.Equal(payload, e.Record) || !bytes.Equal(ph, wantPayload[:]) || !bytes.Equal(pr, prefix[:]) {
			return fmt.Errorf("durable benchmark bytes/hash/order differ at %d", n+1)
		}
		n++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if n != len(expected) || committed != int64(n) || !bytes.Equal(hash, prefix[:]) {
		return errors.New("durable cursor/hash/count differs")
	}
	return nil
}

type checkResult struct {
	Name  string `json:"name"`
	Pass  bool   `json:"pass"`
	Error string `json:"error,omitempty"`
}

func correctness(ctx context.Context, admin *pgx.Conn, cfg *pgx.ConnConfig, schema string, mode variant, entries []contract.Entry) []checkResult {
	var results []checkResult
	record := func(name string, fn func() error) {
		err := fn()
		r := checkResult{Name: name, Pass: err == nil}
		if err != nil {
			r.Error = err.Error()
		}
		results = append(results, r)
	}
	fixture := entries[:4]
	newDB := func(fault string) (contract.Store, func()) {
		db, _ := benchmarkDB(cfg, schema, mode, fault)
		return contract.Store{DB: db}, func() { db.Close() }
	}
	record("replay-conflict-order-and-atomicity", func() error {
		if err := resetScratch(ctx, admin, schema); err != nil {
			return err
		}
		s, closeDB := newDB("")
		defer closeDB()
		if _, err := s.AppendWithResult(ctx, "bench", fixture[:2], publicationLimits); err != nil {
			return err
		}
		if _, err := s.AppendWithResult(ctx, "bench", fixture[1:], publicationLimits); err != nil {
			return err
		}
		if _, err := s.AppendWithResult(ctx, "bench", fixture, publicationLimits); err != nil {
			return err
		}
		if err := verifyRows(ctx, admin, schema, fixture); err != nil {
			return err
		}
		changed := fixture[0]
		changed.Record = append(bytes.Clone(changed.Record[:len(changed.Record)-1]), []byte(`,"benchmark_extra":true}`)...)
		if !json.Valid(changed.Record) {
			return errors.New("invalid corruption fixture")
		}
		gap := fixture[3]
		gap.Ordinal = 6
		// entries has no UNIQUE(stream_id,source_id); cross-batch source
		// uniqueness is the MVA journal's. A repeat inside one batch is still
		// rejected, and the new first row must not survive.
		fresh := entries[4]
		duplicate := fresh
		duplicate.Ordinal = 6
		for name, batch := range map[string][]contract.Entry{"changed-payload": {changed}, "gap": {gap}, "batch-duplicate-source": {fresh, duplicate}} {
			r, err := s.AppendWithResult(ctx, "bench", batch, publicationLimits)
			if err == nil || r.Committed != 0 || r.PrefixHash != [32]byte{} {
				return fmt.Errorf("%s accepted invalid append/ack", name)
			}
			if err = verifyRows(ctx, admin, schema, fixture); err != nil {
				return err
			}
		}
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := s.AppendWithResult(cancelled, "bench", entries[4:5], publicationLimits); err == nil {
			return errors.New("cancelled append succeeded")
		}
		return verifyRows(ctx, admin, schema, fixture)
	})
	for _, fault := range []string{"before-insert", "after-insert", "before-cursor", "before-commit", "lost-commit-ack", "disconnect-after-insert", "disconnect-after-commit"} {
		record(fault+"-and-reopen", func() error {
			if err := resetScratch(ctx, admin, schema); err != nil {
				return err
			}
			s, closeDB := newDB(fault)
			r, err := s.AppendWithResult(ctx, "bench", fixture, publicationLimits)
			closeDB() // no writer connection, transaction or RAM state survives
			if err == nil || r.Committed != 0 || r.PrefixHash != [32]byte{} {
				return errors.New("injected failure returned durable success")
			}
			durable := fault == "lost-commit-ack" || fault == "disconnect-after-commit"
			var want []contract.Entry
			if durable {
				want = fixture
			}
			if err = verifyRows(ctx, admin, schema, want); err != nil {
				return err
			}
			reopened, closeReopened := newDB("")
			defer closeReopened()
			if _, err = reopened.AppendWithResult(ctx, "bench", fixture, publicationLimits); err != nil {
				return fmt.Errorf("reopen/replay: %w", err)
			}
			return verifyRows(ctx, admin, schema, fixture)
		})
	}
	return results
}
