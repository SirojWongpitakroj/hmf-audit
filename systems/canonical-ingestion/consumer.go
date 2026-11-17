package canonicalingestion

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
)

type Consumer struct {
	Store                                 Store // The baseline's database, not necessarily the source database.
	ID, StreamID, Fingerprint, ConfigHash string
	// External authentication publication (DHT) must finish before CompleteStage.
	DeferCompletion bool
	Execution       *Execution
	Metrics         *IngestMetrics
}
type Progress struct {
	Committed, Target uint64
	PrefixHash        [32]byte
	Checkpoint        []byte
}

func (c Consumer) Open(ctx context.Context) error {
	if c.ID == "" || c.StreamID == "" || c.Fingerprint == "" || c.ConfigHash == "" {
		return errors.New("consumer identity, stream, fingerprint and native configuration hash required")
	}
	_, err := c.Store.DB.ExecContext(ctx, `INSERT INTO canonical_ingestion.consumers(consumer_id,stream_id,fingerprint,config_hash,prefix_hash,deferred_completion) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, c.ID, c.StreamID, c.Fingerprint, c.ConfigHash, make([]byte, 32), c.DeferCompletion)
	if err != nil {
		return err
	}
	_, err = c.Progress(ctx)
	return err
}

func (c Consumer) Progress(ctx context.Context) (Progress, error) {
	return c.load(ctx, c.Store.DB, false)
}

type queryRower interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (c Consumer) load(ctx context.Context, q queryRower, lock bool) (Progress, error) {
	var p Progress
	var id, fp, config string
	var n, target int64
	var hash []byte
	var deferred bool
	stmt := `SELECT stream_id,fingerprint,config_hash,committed,target,prefix_hash,native_checkpoint,deferred_completion FROM canonical_ingestion.consumers WHERE consumer_id=$1`
	if lock {
		stmt += ` FOR UPDATE`
	}
	err := q.QueryRowContext(ctx, stmt, c.ID).Scan(&id, &fp, &config, &n, &target, &hash, &p.Checkpoint, &deferred)
	if err != nil {
		return p, err
	}
	if id != c.StreamID || fp != c.Fingerprint || config != c.ConfigHash || deferred != c.DeferCompletion {
		return p, errors.New("consumer stream or native configuration changed")
	}
	if n < 0 || target < 0 || len(hash) != 32 {
		return p, errors.New("invalid durable consumer progress")
	}
	p.Committed = uint64(n)
	p.Target = uint64(target)
	copy(p.PrefixHash[:], hash)
	return p, nil
}

// Begin selects exactly one manual milestone; a larger stage cannot be selected
// until its predecessor was durably completed. Equal/passed targets are no-ops.
func (c Consumer) Begin(ctx context.Context, target uint64) (Progress, error) {
	prior, err := predecessor(target)
	if err != nil {
		return Progress{}, err
	}
	tx, err := c.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	p, err := c.load(ctx, tx, true)
	if err != nil {
		return p, err
	}
	if p.Committed >= target {
		return p, nil
	}
	if p.Target > p.Committed && p.Target != target {
		return p, errors.New("another incremental stage is unfinished; resume it first")
	}
	if prior > 0 {
		var done bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM canonical_ingestion.stages WHERE consumer_id=$1 AND target=$2 AND completed_at IS NOT NULL)`, c.ID, int64(prior)).Scan(&done)
		if err != nil {
			return p, err
		}
		if !done || p.Committed < prior {
			return p, fmt.Errorf("complete the %d milestone first", prior)
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO canonical_ingestion.stages(consumer_id,target,started_total) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, c.ID, int64(target), int64(p.Committed))
	if err != nil {
		return p, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE canonical_ingestion.consumers SET target=$2 WHERE consumer_id=$1`, c.ID, int64(target))
	if err != nil {
		return p, err
	}
	p.Target = target
	err = tx.Commit()
	return p, err
}

// Apply must persist the native records and all required recovery/authentication
// state using tx, then return one native ID per entry and a bounded checkpoint.
// External stores require a durable transactional outbox plus reconciliation;
// returning success before external publication would violate this contract.
type Apply func(context.Context, *sql.Tx, Progress, []Entry) (nativeIDs []string, checkpoint []byte, err error)

// Step advances one bounded batch atomically. It serializes writers using the
// durable cursor row. Failure or process death rolls back both the native writes
// and mappings. An ambiguous commit is resolved by re-reading Progress on restart.
func (c Consumer) Step(ctx context.Context, source Store, l Limits, apply Apply) (out Progress, stepErr error) {
	defer func() {
		if stepErr != nil && c.Metrics != nil {
			c.Metrics.FailedBatches.Add(1)
		}
	}()
	if c.Execution != nil {
		if err := c.Execution.Validate(); err != nil {
			return Progress{}, err
		}
		l = c.Execution.EffectiveLimits(l)
	}
	if err := l.Validate(); err != nil {
		return Progress{}, err
	}
	if apply == nil {
		return Progress{}, errors.New("native transaction adapter required")
	}
	manifest, err := source.Manifest(ctx, c.StreamID)
	if err != nil {
		return Progress{}, err
	}
	if manifest.Fingerprint != c.Fingerprint {
		return Progress{}, errors.New("source stream fingerprint changed")
	}
	tx, err := c.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return Progress{}, err
	}
	defer tx.Rollback()
	p, err := c.load(ctx, tx, true)
	if err != nil {
		return p, err
	}
	if p.Target == 0 {
		return p, errors.New("select one incremental stage first")
	}
	if p.Committed >= p.Target {
		return p, nil
	}
	oldHash, err := prefixAt(ctx, source.DB, c.StreamID, p.Committed)
	if err != nil {
		return p, err
	}
	if oldHash != p.PrefixHash {
		return p, errors.New("consumer prefix differs from immutable stream")
	}
	available := min(p.Target, manifest.Committed)
	if available <= p.Committed {
		return p, errors.New("source finalized prefix not yet available")
	}
	entries, err := source.read(ctx, c.StreamID, p.Committed, available, l, false)
	if err != nil {
		return p, err
	}
	if len(entries) == 0 {
		return p, errors.New("missing source prefix")
	}
	if c.Metrics != nil {
		budget := 0
		for _, e := range entries {
			budget += len(e.Record)*8 + 4096
		}
		c.Metrics.PendingBatchBytes.Store(int64(budget))
		peak(&c.Metrics.PeakPendingBatchBytes, int64(budget))
		defer c.Metrics.PendingBatchBytes.Store(0)
	}
	hashes, err := PrepareSlice(ctx, c.Execution, c.Metrics, entries, func(e Entry) int { return len(e.Record)*4 + 2048 }, func(ctx context.Context, e Entry) ([32]byte, int, error) {
		if err := e.Validate(l.MaxRecordBytes); err != nil {
			return [32]byte{}, 0, err
		}
		return sha256.Sum256(e.Record), 64, nil
	})
	if err != nil {
		return p, err
	}
	ids, checkpoint, err := apply(ctx, tx, p, entries)
	if err != nil {
		return p, err
	}
	if len(ids) != len(entries) {
		return p, errors.New("native adapter did not account for every logical record")
	}
	if len(checkpoint) > l.MaxBatchBytes {
		return p, errors.New("native checkpoint exceeds byte budget")
	}
	prefix := p.PrefixHash
	var mappingRows [][]any
	for i, e := range entries {
		if ids[i] == "" {
			return p, errors.New("empty native ID")
		}
		h := hashes[i]
		mappingRows = append(mappingRows, []any{c.ID, int64(e.Ordinal), e.SourceID, ids[i], h[:]})
		prefix = nextHashPayload(prefix, e, h)
	}
	if err = InsertRows(ctx, tx, `INSERT INTO canonical_ingestion.mappings(consumer_id,ordinal,source_id,native_id,payload_hash) VALUES `, "", mappingRows, l.MaxRecords, l.MaxBatchBytes, c.Metrics); err != nil {
		return p, err
	}
	n := entries[len(entries)-1].Ordinal
	sourceHash, err := prefixAt(ctx, source.DB, c.StreamID, n)
	if err != nil {
		return p, err
	}
	if !bytes.Equal(prefix[:], sourceHash[:]) {
		return p, errors.New("source payload/prefix integrity check failed")
	}
	_, err = tx.ExecContext(ctx, `UPDATE canonical_ingestion.consumers SET committed=$2,prefix_hash=$3,native_checkpoint=$4 WHERE consumer_id=$1`, c.ID, int64(n), prefix[:], checkpoint)
	if err != nil {
		return p, err
	}
	if n == p.Target && !c.DeferCompletion {
		var result sql.Result
		result, err = tx.ExecContext(ctx, `UPDATE canonical_ingestion.stages SET completed_at=now() WHERE consumer_id=$1 AND target=$2 AND completed_at IS NULL`, c.ID, int64(p.Target))
		if err != nil {
			return p, err
		}
		if rows, _ := result.RowsAffected(); rows != 1 {
			return p, errors.New("missing running manual stage")
		}
	}
	if err = tx.Commit(); err != nil {
		return p, err
	}
	bytes := 0
	for _, e := range entries {
		bytes += len(e.Record)
	}
	c.Metrics.Committed(len(entries), bytes)
	p.Committed = n
	p.PrefixHash = prefix
	p.Checkpoint = checkpoint
	return p, nil
}

func (c Consumer) Run(ctx context.Context, source Store, mode string, l Limits, apply Apply) (Progress, error) {
	n, err := Target(mode)
	if err != nil {
		return Progress{}, err
	}
	current, err := c.Progress(ctx)
	if err != nil {
		return current, err
	}
	if current.Committed >= n {
		return current, nil
	}
	if c.Metrics != nil && current.Target > current.Committed {
		c.Metrics.Recoveries.Add(1)
	}
	manifest, err := source.Manifest(ctx, c.StreamID)
	if err != nil {
		return current, err
	}
	if manifest.Fingerprint != c.Fingerprint || manifest.Committed < n {
		return current, fmt.Errorf("publish the complete canonical target %d first (available prefix %d)", n, manifest.Committed)
	}
	p, err := c.Begin(ctx, n)
	if err != nil {
		return p, err
	}
	for p.Committed < n {
		p, err = c.Step(ctx, source, l, apply)
		if err != nil {
			return p, err
		}
	}
	return p, nil
}
