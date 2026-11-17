package durable

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"blas/internal/diskindex"
	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestConcurrentAuditMatchesSerialAndRejectsTampering(t *testing.T) {
	for _, aggregation := range []bool{false, true} {
		t.Run(fmt.Sprint(aggregation), func(t *testing.T) {
			db, dsn := testfixture.Database(t, "BLAS_TEST_POSTGRES_DSN")
			db.SetMaxOpenConns(6)
			ctx := context.Background()
			a := generatorAdapter(db)
			a.Options.Aggregation = aggregation
			a.Options.Index.CacheBytes, a.Options.Index.CacheEntries = 1<<20, 128
			source := contract.Store{DB: db}
			es := generatorEntries(96)
			if err := source.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			if err := source.Create(ctx, "fixture", "same-source"); err != nil {
				t.Fatal(err)
			}
			if err := source.Append(ctx, "fixture", es, contract.Limits{MaxRecords: 96, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}); err != nil {
				t.Fatal(err)
			}
			if err := a.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			c := contract.Consumer{Store: source, ID: "blas/low-risk", StreamID: "fixture", Fingerprint: "same-source", ConfigHash: a.Options.ConfigHash()}
			if err := c.Open(ctx); err != nil {
				t.Fatal(err)
			}
			// Select a native stage but ingest only 96 fixture observations. These
			// complete all 4-entry files and 3-file blocks without partial closure.
			if _, err := c.Begin(ctx, 1000000); err != nil {
				t.Fatal(err)
			}
			for {
				p, err := c.Step(ctx, source, contract.Limits{MaxRecords: 24, MaxBatchBytes: 1 << 20, MaxRecordBytes: 65536}, a.Apply)
				if err != nil {
					t.Fatal(err)
				}
				if p.Committed == 96 {
					break
				}
			}
			before := nativeSnapshot(t, db)
			request := contract.AuditRequest{Version: 1, StreamID: c.StreamID, Fingerprint: c.Fingerprint, ExpectedN: 96, Entries: es[:12]}
			var comparison [2]testfixture.LocalAuditResult
			for mode := range 2 {
				policy := contract.DefaultAuditReads()
				policy.Legacy = mode == 0
				runCtx := contract.WithAuditReads(contract.WithAuditExecution(ctx, contract.DefaultAuditExecution()), policy)
				pageCache, _ := diskindex.NewCache(a.Options.Index)
				label := []string{"before", "after"}[mode]
				comparison[mode] = testfixture.MeasureAudit(t, fmt.Sprint("BLAS-agg-", aggregation), label, 96, 12, func() (contract.AuditMetrics, error) { return a.Audit(runCtx, c, request, pageCache, true) })
			}
			old, new := comparison[0].Metrics, comparison[1].Metrics
			if new.TransactionQueries >= old.TransactionQueries || new.TransactionLogicalVisits != old.TransactionLogicalVisits || new.RequestedRecords != old.RequestedRecords || new.ProofBytes != old.ProofBytes || new.ReturnedRecords != old.ReturnedRecords {
				t.Fatal("bulk reads changed work/evidence or failed to reduce calls", old, new)
			}
			if new.RequestCachePeakBytes > uint64(contract.DefaultAuditReads().CacheBytes) {
				t.Fatal("block cache exceeded byte bound")
			}
			cache, _ := diskindex.NewCache(a.Options.Index)
			coldCtx := contract.WithAuditCacheReset(ctx, func(context.Context) error { cache.Clear(); return nil })
			for range 2 {
				m, e := contract.AuditWithColdCaches(coldCtx, func(ctx context.Context) (contract.AuditMetrics, error) {
					if cache.Stats().CacheEntries != 0 {
						t.Fatal("warm audit start")
					}
					return a.Audit(ctx, c, request, cache, true)
				})
				if e != nil || m.RequestedRecords != 12 || m.StorageReads == 0 || cache.Stats().CacheEntries != 0 || !m.ApplicationCacheResetBefore || !m.ApplicationCacheResetAfter {
					t.Fatal("cold-cache audit failed", m, e)
				}
			}
			// Bulk row ordering is exact for every chunk size. Mutation of a
			// privately decoded transaction must never poison shared encoded rows.
			legacyPolicy := contract.DefaultAuditReads()
			legacyPolicy.Legacy = true
			reference, e := a.NewReader(contract.WithAuditReads(ctx, legacyPolicy), nil).transactions(0)
			if e != nil {
				t.Fatal(e)
			}
			for _, batchRows := range []int{1, 2, 256} {
				p := contract.DefaultAuditReads()
				p.BatchRows = batchRows
				shared := contract.NewAuditReadCache[uint64, [][]byte](p)
				readCtx := context.WithValue(contract.WithAuditReads(ctx, p), blockInputsKey{}, shared)
				got, e := a.NewReader(readCtx, nil).transactions(0)
				if e != nil || !reflect.DeepEqual(reference, got) {
					t.Fatal("bulk transaction order differs", batchRows, e)
				}
				got[0].FID++
				got[0].Sig[0] ^= 1
				got, e = a.NewReader(readCtx, nil).transactions(0)
				if e != nil || !reflect.DeepEqual(reference, got) {
					t.Fatal("shared transaction mutated", e)
				}
			}
			// A gap or corrupt object must fail, even if a prior request cached
			// that block. Every audit creates a fresh cache.
			var original []byte
			if e := db.QueryRow(`SELECT object FROM blas.durable_objects WHERE run_id=$1 AND kind='tx' AND owner=0 AND oid=1`, a.Options.RunID).Scan(&original); e != nil {
				t.Fatal(e)
			}
			if _, e := db.Exec(`UPDATE blas.durable_objects SET oid=999 WHERE run_id=$1 AND kind='tx' AND owner=0 AND oid=1`, a.Options.RunID); e != nil {
				t.Fatal(e)
			}
			if _, e := a.NewReader(ctx, nil).transactions(0); e == nil {
				t.Fatal("missing transaction accepted")
			}
			if _, e := db.Exec(`UPDATE blas.durable_objects SET oid=1 WHERE run_id=$1 AND kind='tx' AND owner=0 AND oid=999`, a.Options.RunID); e != nil {
				t.Fatal(e)
			}
			if _, e := db.Exec(`UPDATE blas.durable_objects SET object=$2 WHERE run_id=$1 AND kind='tx' AND owner=0 AND oid=1`, a.Options.RunID, []byte("corrupt")); e != nil {
				t.Fatal(e)
			}
			if _, e := a.NewReader(ctx, nil).transactions(0); e == nil {
				t.Fatal("corrupt transaction accepted")
			}
			if _, e := db.Exec(`UPDATE blas.durable_objects SET object=$2 WHERE run_id=$1 AND kind='tx' AND owner=0 AND oid=1`, a.Options.RunID, original); e != nil {
				t.Fatal(e)
			}
			serial, err := a.Audit(ctx, c, request, cache, true)
			if err != nil {
				t.Fatal(err)
			}
			logical := func(m contract.AuditMetrics) []uint64 {
				return []uint64{m.RequestedRecords, m.ReturnedRecords, m.NativeQueries, m.EvidenceBytes, m.BitmapBytes, m.ProofBytes}
			}
			for _, workers := range []int{2, 8, 2} {
				policy := contract.DefaultAuditExecution()
				policy.CPUWorkers = workers
				tracked, activity := contract.TrackAuditActivity(contract.WithAuditExecution(ctx, policy))
				got, err := a.Audit(tracked, c, request, cache, true)
				if err != nil || !reflect.DeepEqual(logical(got), logical(serial)) {
					t.Fatal("parallel logical audit differs", workers, got, serial, err)
				}
				if got.CacheBytes > got.CacheMaxBytes || got.CacheEntries > got.CacheMaxEntries {
					t.Fatal("cache exceeded budget")
				}
				if activity.Peak() < 2 || activity.Peak() > min(workers, policy.IOWorkers, policy.SQLPoolSize) {
					t.Fatal("point audits did not overlap within budget", activity.Peak())
				}
			}
			// Reopen from durable state with an empty cache; execution changes cannot
			// alter stored configuration or require rebuilding authenticated state.
			reopened, err := contract.OpenPostgres(ctx, dsn, 6)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			a.DB = reopened
			c.Store.DB = reopened
			got, err := a.Audit(contract.WithAuditExecution(ctx, contract.DefaultAuditExecution()), c, request, nil, true)
			if err != nil || !reflect.DeepEqual(logical(got), logical(serial)) || a.Options.ConfigHash() != c.ConfigHash {
				t.Fatal("restart/fingerprint changed", err)
			}
			if before != nativeSnapshot(t, db) {
				t.Fatal("read-only audit mutated native state")
			}
			// Corrupt one authenticated header, bypassing every retained page cache.
			if _, err := db.Exec(`UPDATE blas.durable_headers SET payload=$2 WHERE run_id=$1`, a.Options.RunID, []byte("tampered")); err != nil {
				t.Fatal(err)
			}
			for _, testCtx := range []context.Context{ctx, contract.WithAuditExecution(ctx, contract.DefaultAuditExecution())} {
				if _, err := a.Audit(testCtx, c, request, nil, true); err == nil {
					t.Fatal("tampered header accepted")
				}
			}
		})
	}
}
