package canonicalingestion_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

// All tests in this file publish at most 100 canonical observations. A selected
// 1M *stage* does not manufacture entries or ingest beyond the published prefix.
func TestFollowPrefixLagFailureAndResume(t *testing.T) {
	db, _ := testfixture.Database(t, "CANONICAL_TEST_POSTGRES_DSN")
	source := contract.Store{DB: db}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := source.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if err := source.Create(ctx, "live", "fingerprint-v1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE native_fixture(consumer text,ordinal bigint,PRIMARY KEY(consumer,ordinal))`); err != nil {
		t.Fatal(err)
	}
	limits := contract.Limits{MaxRecords: 10, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}
	ids := []string{"delia/live", "blas/live", "dht/live"}
	var consumers [3]contract.Consumer
	for i, id := range ids {
		consumers[i] = contract.Consumer{Store: source, ID: id, StreamID: "live", Fingerprint: "fingerprint-v1", ConfigHash: "native-v1"}
		if err := consumers[i].Open(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var slow atomic.Bool
	slow.Store(true)
	var fail atomic.Bool
	fail.Store(true)
	apply := func(i int) contract.Apply {
		return func(ctx context.Context, tx *sql.Tx, p contract.Progress, es []contract.Entry) ([]string, []byte, error) {
			if i == 1 && slow.Load() {
				if err := contract.Wait(ctx, 10*time.Millisecond); err != nil {
					return nil, nil, err
				}
			}
			if i == 1 && es[0].Ordinal > 30 && fail.CompareAndSwap(true, false) {
				return nil, nil, errors.New("injected consumer failure")
			}
			out := make([]string, len(es))
			for j, e := range es {
				out[j] = fmt.Sprint(e.Ordinal)
				if _, err := tx.ExecContext(ctx, `INSERT INTO native_fixture VALUES($1,$2)`, ids[i], e.Ordinal); err != nil {
					return nil, nil, err
				}
			}
			return out, []byte(`{}`), nil
		}
	}
	done := make(chan error, 3)
	for i := range consumers {
		go func(i int) {
			_, err := consumers[i].Follow(ctx, source, 1000000, limits, apply(i), time.Millisecond, nil, nil)
			done <- err
		}(i)
	}
	// A stalled BLAS may lag while the others consume; producer never asks for
	// lockstep. Room for a whole next chunk is required before publication.
	for first := uint64(1); first <= 40; first += 10 {
		if err := contract.WaitForProducerRoom(ctx, source, db, "live", ids, first+9, 30, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		var entries []contract.Entry
		for n := first; n < first+10; n++ {
			entries = append(entries, testfixture.Entry(n))
		}
		if err := source.Append(ctx, "live", entries, limits); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case err := <-done:
		if err == nil || err.Error() != "injected consumer failure" {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p, err := consumers[1].Progress(ctx)
	if err != nil || p.Committed != 30 {
		t.Fatal("failure cursor", p.Committed, err)
	}
	// BLAS is stopped at 30. Published 40 is safe; a proposed publication to 70
	// would exceed lead 30 and must block while DELIA/DHT remain independent.
	throttle, cancelThrottle := context.WithTimeout(ctx, 30*time.Millisecond)
	err = contract.WaitForProducerRoom(throttle, source, db, "live", ids, 70, 30, time.Millisecond)
	cancelThrottle()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("producer failed to throttle", err)
	}
	for _, i := range []int{0, 2} {
		p, err = consumers[i].Progress(ctx)
		if err != nil || p.Committed != 40 {
			t.Fatal("independent consumer stopped", i, p.Committed, err)
		}
	}
	// Resume failed consumer from its own durable cursor.
	slow.Store(false)
	go func() {
		_, err := consumers[1].Follow(ctx, source, 1000000, limits, apply(1), time.Millisecond, nil, nil)
		done <- err
	}()
	for first := uint64(41); first <= 100; first += 10 {
		if err = contract.WaitForProducerRoom(ctx, source, db, "live", ids, first+9, 30, time.Millisecond); err != nil {
			t.Fatal(err)
		}
		var entries []contract.Entry
		for n := first; n < first+10; n++ {
			entries = append(entries, testfixture.Entry(n))
		}
		if err = source.Append(ctx, "live", entries, limits); err != nil {
			t.Fatal(err)
		}
	}
	for {
		all := true
		for _, c := range consumers {
			p, err = c.ValidatePrefix(ctx, source)
			if err != nil || p.Committed > 100 {
				t.Fatal("prefix violation", p, err)
			}
			all = all && p.Committed == 100
		}
		if all {
			break
		}
		if err = contract.Wait(ctx, time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	// Simulated producer death: no more Append calls. Consumers must wait at100.
	if err = contract.Wait(ctx, 20*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, c := range consumers {
		p, _ = c.Progress(ctx)
		if p.Committed != 100 {
			t.Fatal("consumed unpublished entries")
		}
	}
	cancel()
	for range 3 {
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM native_fixture`).Scan(&count); err != nil || count != 300 {
		t.Fatal("replay duplicated/skipped native rows", count, err)
	}
}
