package benchmark

import (
	"context"
	"errors"
	"sync"
	"time"
)

// PipelinedPublication runs PublishPending on one background goroutine while
// consumer steps continue. It is the only publisher, so epochs are still
// published oldest first and anchors commit in epoch order. Proofs are rebuilt
// from committed digests, so the stored proofs and anchors are unchanged; only
// published_at timing moves. The SQL outbox stays the recovery source: a crash
// or publisher error leaves unpublished epochs for the next PublishPending.
type PipelinedPublication struct {
	publish    func(context.Context) error
	pending    func(context.Context) (uint64, error)
	maxPending uint64
	poll       time.Duration
	wake       chan struct{}
	done       chan struct{}
	cancel     context.CancelFunc
	mu         sync.Mutex
	err        error
}

// StartPipelinedPublication calls onError once if a publication fails, so the
// caller can stop consuming; the error is also returned by later calls.
func StartPipelinedPublication(ctx context.Context, publish func(context.Context) error, pending func(context.Context) (uint64, error), maxPending uint64, onError func()) *PipelinedPublication {
	ctx, cancel := context.WithCancel(ctx)
	p := &PipelinedPublication{publish: publish, pending: pending, maxPending: maxPending, poll: 20 * time.Millisecond, wake: make(chan struct{}, 1), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(p.done)
		for {
			select {
			case <-ctx.Done():
				return
			case <-p.wake:
			}
			if err := publish(ctx); err != nil {
				if ctx.Err() == nil {
					p.mu.Lock()
					p.err = err
					p.mu.Unlock()
					if onError != nil {
						onError()
					}
				}
				return
			}
		}
	}()
	return p
}

func (p *PipelinedPublication) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// AfterStep wakes the publisher and returns once at most maxPending sealed
// epochs remain unpublished, bounding how far the cursor runs ahead of proofs.
func (p *PipelinedPublication) AfterStep(ctx context.Context) error {
	return p.waitFor(ctx, p.maxPending)
}

// Drain waits until every sealed epoch is published. Stage completion calls it
// before the unchanged PublicationReady check.
func (p *PipelinedPublication) Drain(ctx context.Context) error {
	return p.waitFor(ctx, 0)
}

func (p *PipelinedPublication) waitFor(ctx context.Context, limit uint64) error {
	for {
		if err := p.Err(); err != nil {
			return err
		}
		select {
		case p.wake <- struct{}{}:
		default: // a wake is already queued
		}
		n, err := p.pending(ctx)
		if err != nil {
			return err
		}
		if n <= limit {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.done:
			if err := p.Err(); err != nil {
				return err
			}
			return errors.New("dht publication: background publisher stopped")
		case <-time.After(p.poll):
		}
	}
}

// Close cancels and joins the publisher. Unpublished epochs stay in the outbox.
func (p *PipelinedPublication) Close() error {
	p.cancel()
	<-p.done
	return p.Err()
}

// UnpublishedEpochs counts sealed native epochs still waiting for proofs.
func (a *IncrementalDHT) UnpublishedEpochs(ctx context.Context) (uint64, error) {
	var n int64
	err := a.DB.QueryRowContext(ctx, `SELECT count(*) FROM dhtbacked.dht_ingestion_epochs WHERE run_id=$1 AND published_at IS NULL`, a.Options.RunID).Scan(&n)
	return uint64(n), err
}
