package benchmark

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeOutbox models sealed epochs; publish drains them oldest first, one at a time.
type fakeOutbox struct {
	mu        sync.Mutex
	sealed    []uint64
	published []uint64
	active    atomic.Int32
	overlap   atomic.Bool
	release   chan struct{} // nil publishes immediately
	fail      error
}

func (o *fakeOutbox) seal(epoch uint64) {
	o.mu.Lock()
	o.sealed = append(o.sealed, epoch)
	o.mu.Unlock()
}
func (o *fakeOutbox) pending(context.Context) (uint64, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return uint64(len(o.sealed)), nil
}
func (o *fakeOutbox) publish(ctx context.Context) error {
	if o.active.Add(1) > 1 {
		o.overlap.Store(true)
	}
	defer o.active.Add(-1)
	for {
		o.mu.Lock()
		if len(o.sealed) == 0 {
			o.mu.Unlock()
			return nil
		}
		next := o.sealed[0]
		o.mu.Unlock()
		if o.fail != nil {
			return o.fail
		}
		if o.release != nil {
			select {
			case <-o.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		o.mu.Lock()
		o.sealed = o.sealed[1:]
		o.published = append(o.published, next)
		o.mu.Unlock()
	}
}

func waitDone(t *testing.T, c <-chan error) error {
	t.Helper()
	select {
	case err := <-c:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		return nil
	}
}

func TestPipelinedPublicationBoundsLagAndPublishesInOrder(t *testing.T) {
	o := &fakeOutbox{release: make(chan struct{})}
	p := StartPipelinedPublication(context.Background(), o.publish, o.pending, 2, nil)
	defer p.Close()
	o.seal(1)
	o.seal(2)
	if err := p.AfterStep(context.Background()); err != nil { // 2 pending <= 2: no wait
		t.Fatal(err)
	}
	o.seal(3)
	waited := make(chan error, 1)
	go func() { waited <- p.AfterStep(context.Background()) }()
	select {
	case err := <-waited:
		t.Fatal("step continued with three unpublished epochs", err)
	case <-time.After(100 * time.Millisecond):
	}
	o.release <- struct{}{} // epoch 1 published: two remain
	if err := waitDone(t, waited); err != nil {
		t.Fatal(err)
	}
	drained := make(chan error, 1)
	go func() { drained <- p.Drain(context.Background()) }()
	o.release <- struct{}{}
	o.release <- struct{}{}
	if err := waitDone(t, drained); err != nil {
		t.Fatal(err)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if len(o.published) != 3 || o.published[0] != 1 || o.published[1] != 2 || o.published[2] != 3 || o.overlap.Load() {
		t.Fatal("publication order/serialization", o.published, o.overlap.Load())
	}
}

func TestPipelinedPublicationFailureStopsConsumer(t *testing.T) {
	fault := errors.New("cassandra unavailable")
	o := &fakeOutbox{fail: fault}
	var stopped atomic.Bool
	p := StartPipelinedPublication(context.Background(), o.publish, o.pending, 1, func() { stopped.Store(true) })
	o.seal(1)
	o.seal(2)
	done := make(chan error, 1)
	go func() { done <- p.AfterStep(context.Background()) }()
	if err := waitDone(t, done); !errors.Is(err, fault) || !stopped.Load() {
		t.Fatal("publisher failure not surfaced", err, stopped.Load())
	}
	if err := p.Drain(context.Background()); !errors.Is(err, fault) {
		t.Fatal("drain after failure", err)
	}
	if err := p.Close(); !errors.Is(err, fault) {
		t.Fatal("close after failure", err)
	}
	if n, _ := o.pending(context.Background()); n != 2 {
		t.Fatal("failed epochs must stay in the outbox", n)
	}
}

func TestPipelinedPublicationCloseJoinsAndCancelUnblocksWait(t *testing.T) {
	o := &fakeOutbox{release: make(chan struct{})}
	p := StartPipelinedPublication(context.Background(), o.publish, o.pending, 0, nil)
	o.seal(1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Drain(ctx) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := waitDone(t, done); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled wait", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	if err := waitDone(t, closed); err != nil {
		t.Fatal("close must cancel an in-flight publish without reporting failure", err)
	}
	if n, _ := o.pending(context.Background()); n != 1 {
		t.Fatal("interrupted epoch must stay in the outbox", n)
	}
}
