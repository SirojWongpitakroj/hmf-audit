package canonicalingestion

import (
	"context"
	"database/sql"
	"errors"
)

// CompleteStage is for schemes whose authentication publication crosses storage
// systems. Their outbox is durable with the logical log/cursor transaction;
// recovery drains it idempotently before this native publication check runs.
// Logical committed count alone is never sufficient to benchmark such a stage.
func (c Consumer) CompleteStage(ctx context.Context, check func(context.Context, *sql.Tx, Progress) error) error {
	if !c.DeferCompletion || check == nil {
		return errors.New("external publication check required")
	}
	tx, err := c.Store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := c.load(ctx, tx, true)
	if err != nil {
		return err
	}
	if p.Target == 0 || p.Committed != p.Target {
		return errors.New("selected target is not exactly ingested")
	}
	if err = check(ctx, tx, p); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE canonical_ingestion.stages SET completed_at=COALESCE(completed_at,now()) WHERE consumer_id=$1 AND target=$2`, c.ID, int64(p.Target))
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return errors.New("missing manual stage publication history")
	}
	return tx.Commit()
}

// RequireMilestone is used while the caller holds the consumer lease. A passed
// target is rejected for measurements: benchmarking 1M against a 10M state
// would silently change the experiment.
func (c Consumer) RequireMilestone(ctx context.Context, n uint64) error {
	if _, err := predecessor(n); err != nil {
		return err
	}
	p, err := c.Progress(ctx)
	if err != nil {
		return err
	}
	if p.Committed != n {
		return errors.New("persistent dataset does not equal requested measurement N")
	}
	var done bool
	err = c.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM canonical_ingestion.stages WHERE consumer_id=$1 AND target=$2 AND completed_at IS NOT NULL)`, c.ID, int64(n)).Scan(&done)
	if err != nil {
		return err
	}
	if !done {
		return errors.New("native authentication publication is not complete")
	}
	return nil
}
