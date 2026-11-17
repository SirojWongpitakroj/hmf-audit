package orchestration

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestSnapshotReportsProofPrefixAndRejectsChangedIdentity(t *testing.T) {
	db, _ := testfixture.Database(t, "CANONICAL_TEST_POSTGRES_DSN")
	ctx := context.Background()
	s := contract.Store{DB: db}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	c := Defaults()
	c.RunID = "fixture"
	c.Stream = "fixture"
	fp := "mva-canonical-v1/coverage-scheduler-v1/source-hash"
	if err := s.Create(ctx, c.Stream, fp); err != nil {
		t.Fatal(err)
	}
	l := contract.Limits{MaxRecords: 4, MaxBatchBytes: 65536, MaxRecordBytes: 4096}
	es := []contract.Entry{testfixture.Entry(1), testfixture.Entry(2), testfixture.Entry(3), testfixture.Entry(4)}
	if err := s.Append(ctx, c.Stream, es, l); err != nil {
		t.Fatal(err)
	}
	for _, scheme := range []string{"delia", "blas", "dht"} {
		consumer := contract.Consumer{Store: s, ID: scheme + "/fixture", StreamID: c.Stream, Fingerprint: fp, ConfigHash: "native"}
		if err := consumer.Open(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := consumer.Begin(ctx, 1000000); err != nil {
			t.Fatal(err)
		}
		_, err := consumer.Step(ctx, s, l, func(_ context.Context, _ *sql.Tx, _ contract.Progress, es []contract.Entry) ([]string, []byte, error) {
			out := make([]string, len(es))
			for i, e := range es {
				out[i] = fmt.Sprint(e.Ordinal)
			}
			return out, []byte(`{}`), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := db.Exec(`CREATE TABLE ingestion_run(singleton boolean,committed bigint,index_hash text);INSERT INTO ingestion_run VALUES(true,4,'source-hash');
CREATE SCHEMA dhtbacked;CREATE TABLE dhtbacked.dht_ingestion_epochs(run_id text,epoch bigint,source_highwater bigint,published_at timestamptz);
INSERT INTO dhtbacked.dht_ingestion_epochs VALUES('fixture',1,2,now()),('fixture',2,4,NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	p, err := snapshot(ctx, databases{source: db, native: db, journal: db}, c, 1000000)
	if err != nil || p.DHTAccepted != 4 || p.DHTProofComplete != 2 || p.DHT.Lag != 2 || p.Complete() {
		t.Fatal(p, err)
	}
	if _, err = db.Exec(`UPDATE dhtbacked.dht_ingestion_epochs SET published_at=now()`); err != nil {
		t.Fatal(err)
	}
	p, err = snapshot(ctx, databases{source: db, native: db, journal: db}, c, 1000000)
	if err != nil || p.DHTProofComplete != 4 {
		t.Fatal(p, err)
	}
	if _, err = db.Exec(`UPDATE canonical_ingestion.consumers SET fingerprint='changed' WHERE consumer_id='blas/fixture'`); err != nil {
		t.Fatal(err)
	}
	if _, err = snapshot(ctx, databases{source: db, native: db, journal: db}, c, 1000000); err == nil {
		t.Fatal("changed stream identity accepted")
	}
}
