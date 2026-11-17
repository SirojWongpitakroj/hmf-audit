package canonicalingestion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// ManagedContext also cancels when the supervisor's stdin pipe closes. This
// works on Windows and on abrupt supervisor death, without process-local state
// becoming authoritative. Native transactions/outboxes remain the recovery log.
func ManagedContext(managed bool) (context.Context, func()) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(ctx)
	if managed {
		go func() { _, _ = io.Copy(io.Discard, os.Stdin); cancel() }()
	}
	return ctx, func() { cancel(); stop() }
}

func Wait(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("positive poll interval required")
	}
	t := time.NewTimer(interval)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// WaitManifest waits only for the producer to create the stream. SQL errors
// other than an absent stream are failures, never silently retried corruption.
func (s Store) WaitManifest(ctx context.Context, id string, interval time.Duration) (Manifest, error) {
	for {
		m, err := s.Manifest(ctx, id)
		if !errors.Is(err, sql.ErrNoRows) {
			return m, err
		}
		if err = Wait(ctx, interval); err != nil {
			return m, err
		}
	}
}

// ValidatePrefix also runs for already-completed targets on restart.
func (c Consumer) ValidatePrefix(ctx context.Context, source Store) (Progress, error) {
	p, err := c.Progress(ctx)
	if err != nil {
		return p, err
	}
	m, err := source.Manifest(ctx, c.StreamID)
	if err != nil {
		return p, err
	}
	if m.Fingerprint != c.Fingerprint || p.Committed > m.Committed {
		return p, errors.New("consumer is outside authoritative published stream")
	}
	h, err := source.Prefix(ctx, c.StreamID, p.Committed)
	if err == nil && h != p.PrefixHash {
		err = errors.New("consumer prefix differs from immutable stream")
	}
	return p, err
}

// Follow consumes each newly available durable prefix independently. Publication
// recovery runs before any further native acceptance and after every commit.
func (c Consumer) Follow(ctx context.Context, source Store, target uint64, l Limits, apply Apply, poll time.Duration, recover func(context.Context) error, complete func(context.Context) error) (Progress, error) {
	p, err := c.ValidatePrefix(ctx, source)
	if err != nil {
		return p, err
	}
	if p.Committed > target {
		return p, fmt.Errorf("consumer has passed target %d (current %d)", target, p.Committed)
	}
	p, err = c.Begin(ctx, target)
	if err != nil {
		return p, err
	}
	if recover != nil {
		if err = recover(ctx); err != nil {
			return p, err
		}
	}
	for p.Committed < target {
		m, e := source.Manifest(ctx, c.StreamID)
		if e != nil {
			return p, e
		}
		if m.Fingerprint != c.Fingerprint {
			return p, errors.New("stream fingerprint changed")
		}
		if m.Committed <= p.Committed {
			if err = Wait(ctx, poll); err != nil {
				return p, err
			}
			continue
		}
		p, err = c.Step(ctx, source, l, apply)
		if err != nil {
			return p, err
		}
		if recover != nil {
			if err = recover(ctx); err != nil {
				return p, err
			}
		}
	}
	if complete != nil {
		err = complete(ctx)
	}
	return p, err
}

// FollowMilestones also catches up a baseline that has not yet completed MVA's
// preceding milestone. Native stage boundaries (notably BLAS end-of-milestone
// file closure) are retained rather than bypassing predecessor checks.
func (c Consumer) FollowMilestones(ctx context.Context, source Store, target uint64, l Limits, apply Apply, poll time.Duration, recover func(context.Context) error, complete func(context.Context) error) (Progress, error) {
	if _, err := predecessor(target); err != nil {
		return Progress{}, err
	}
	for {
		p, err := c.Progress(ctx)
		if err != nil {
			return p, err
		}
		stage := target
		if p.Target > p.Committed && p.Target > target {
			return p, errors.New("resume the unfinished larger stage first")
		}
		if p.Target > 0 && p.Target <= target && p.Target >= p.Committed {
			var done bool
			if err = c.Store.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM canonical_ingestion.stages WHERE consumer_id=$1 AND target=$2 AND completed_at IS NOT NULL)`, c.ID, int64(p.Target)).Scan(&done); err != nil {
				return p, err
			}
			if !done {
				stage = p.Target
			} else {
				for _, n := range milestones {
					if n > p.Committed && n <= target {
						stage = n
						break
					}
				}
			}
		} else {
			for _, n := range milestones {
				if n > p.Committed && n <= target {
					stage = n
					break
				}
			}
		}
		p, err = c.Follow(ctx, source, stage, l, apply, poll, recover, complete)
		if err != nil || stage == target {
			return p, err
		}
	}
}
