package orchestration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	contract "canonicalingestion"
)

type Cursor struct {
	Committed     uint64  `json:"committed"`
	ProofComplete uint64  `json:"proof_complete,omitempty"`
	Lag           uint64  `json:"lag"`
	Rate          float64 `json:"records_per_second"`
	Completed     bool    `json:"completed"`
}
type Progress struct {
	Time                time.Time            `json:"time"`
	Target              uint64               `json:"target"`
	MVAFinalized        uint64               `json:"mva_finalized"`
	Published           uint64               `json:"published"`
	Fingerprint         string               `json:"fingerprint"`
	PrefixHash          string               `json:"prefix_sha256"`
	DELIA               Cursor               `json:"delia"`
	BLAS                Cursor               `json:"blas"`
	DHT                 Cursor               `json:"dht"`
	DHTAccepted         uint64               `json:"dht_accepted"`
	DHTProofComplete    uint64               `json:"dht_proof_complete"`
	PublishedRate       float64              `json:"published_records_per_second"`
	Children            map[string]Telemetry `json:"processes,omitempty"`
	Final               bool                 `json:"final"`
	MVAFinalizeFirst    bool                 `json:"mva_finalize_first,omitempty"`
	MVAPublishEachEpoch bool                 `json:"mva_publish_each_epoch,omitempty"`
	Phase               string               `json:"phase,omitempty"`
}
type databases struct {
	source, native, journal *sql.DB
	close                   func()
}

func open(ctx context.Context) (databases, error) {
	s, n, closeDB, err := contract.OpenIngestionDatabases(ctx, 3)
	if err != nil {
		return databases{}, err
	}
	j, err := contract.OpenEnvPostgres(ctx, "MVA_CHECKPOINT", "", 3)
	if err != nil {
		closeDB()
		return databases{}, err
	}
	return databases{s, n, j, func() { j.Close(); closeDB() }}, nil
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func tableExists(ctx context.Context, db queryRower, name string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, name).Scan(&exists)
	return exists, err
}
func snapshot(ctx context.Context, db databases, c Config, target uint64) (Progress, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p := Progress{Time: time.Now().UTC(), Target: target, MVAFinalizeFirst: c.MVAFinalizeFirst, MVAPublishEachEpoch: c.MVAPublishEachEpoch}
	native, err := db.native.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return p, err
	}
	defer native.Rollback()
	for _, pair := range []struct {
		id  string
		out *Cursor
	}{{"delia/" + c.RunID, &p.DELIA}, {"blas/" + c.RunID, &p.BLAS}, {"dht/" + c.RunID, &p.DHT}} {
		var stream, fp string
		var prefix []byte
		err := native.QueryRowContext(ctx, `SELECT committed,stream_id,fingerprint,prefix_hash,EXISTS(SELECT 1 FROM canonical_ingestion.stages s WHERE s.consumer_id=c.consumer_id AND s.target=$2 AND s.completed_at IS NOT NULL) FROM canonical_ingestion.consumers c WHERE consumer_id=$1`, pair.id, int64(target)).Scan(&pair.out.Committed, &stream, &fp, &prefix, &pair.out.Completed)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return p, err
		}
		if err == nil {
			m, e := (contract.Store{DB: db.source}).Manifest(ctx, c.Stream)
			if e != nil {
				return p, e
			}
			if stream != c.Stream || fp != m.Fingerprint {
				return p, fmt.Errorf("%s stream fingerprint changed", pair.id)
			}
			h, e := (contract.Store{DB: db.source}).Prefix(ctx, c.Stream, pair.out.Committed)
			if e != nil {
				return p, e
			}
			if string(h[:]) != string(prefix) {
				return p, fmt.Errorf("%s prefix hash differs", pair.id)
			}
		}
	}
	exists, err := tableExists(ctx, native, "dhtbacked.dht_ingestion_epochs")
	if err != nil {
		return p, err
	}
	if exists {
		// Publications are ordered. source_highwater is the original durable
		// canonical ordinal, unlike an epoch number or padded native record ID.
		err = native.QueryRowContext(ctx, `SELECT COALESCE((SELECT source_highwater FROM dhtbacked.dht_ingestion_epochs WHERE run_id=$1 AND published_at IS NOT NULL AND epoch < COALESCE((SELECT epoch FROM dhtbacked.dht_ingestion_epochs WHERE run_id=$1 AND published_at IS NULL ORDER BY epoch LIMIT 1),9223372036854775807) ORDER BY epoch DESC LIMIT 1),0)`, c.RunID).Scan(&p.DHT.ProofComplete)
		if err != nil {
			return p, err
		}
	}
	m, err := (contract.Store{DB: db.source}).Manifest(ctx, c.Stream)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	p.Published, p.Fingerprint, p.PrefixHash = m.Committed, m.Fingerprint, fmt.Sprintf("%x", m.PrefixHash)
	exists, err = tableExists(ctx, db.journal, "ingestion_run")
	if err != nil {
		return p, err
	}
	if exists {
		var hash string
		err = db.journal.QueryRowContext(ctx, `SELECT committed,index_hash FROM ingestion_run WHERE singleton`).Scan(&p.MVAFinalized, &hash)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return p, err
		}
		if p.Fingerprint != "" && err == nil && p.Fingerprint != "mva-canonical-v1/coverage-scheduler-v1/"+hash {
			return p, fmt.Errorf("canonical fingerprint differs from MVA finalized journal")
		}
	}
	if (!c.MVAFinalizeFirst && !c.MVAPublishEachEpoch && p.Published > p.MVAFinalized) || p.MVAFinalized > target {
		return p, fmt.Errorf("published/finalized state outside selected target")
	}
	for _, s := range []*Cursor{&p.DELIA, &p.BLAS, &p.DHT} {
		if s.Committed > p.Published {
			return p, fmt.Errorf("consumer exceeds published canonical prefix")
		}
		s.Lag = p.Published - s.Committed
	}
	if p.DHT.ProofComplete > p.DHT.Committed {
		return p, fmt.Errorf("proof completion exceeds DHT acceptance")
	}
	p.DHT.Lag = p.Published - p.DHT.ProofComplete
	p.DHTAccepted, p.DHTProofComplete = p.DHT.Committed, p.DHT.ProofComplete
	return p, nil
}
func (p Progress) Complete() bool {
	if p.MVAFinalizeFirst || p.MVAPublishEachEpoch {
		return p.MVAFinalized == p.Target && p.Published >= p.Target && p.DELIA.Committed >= p.Target && p.BLAS.Committed >= p.Target && p.DHT.Committed >= p.Target && p.DHT.ProofComplete >= p.Target && p.DELIA.Completed && p.BLAS.Completed && p.DHT.Completed
	}
	return p.Published == p.Target && p.MVAFinalized == p.Target && p.DELIA.Committed == p.Target && p.BLAS.Committed == p.Target && p.DHT.Committed == p.Target && p.DHT.ProofComplete == p.Target && p.DELIA.Completed && p.BLAS.Completed && p.DHT.Completed
}
