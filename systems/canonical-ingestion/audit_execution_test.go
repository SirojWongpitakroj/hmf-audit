package canonicalingestion

import (
	"context"
	"errors"
	"flag"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuditExecutionDefaultsAndValidation(t *testing.T) {
	f := BindAuditFlags(flag.NewFlagSet("audit", flag.ContinueOnError))
	if f.Execution() != DefaultAuditExecution() {
		t.Fatal("unequal default budgets")
	}
	if AuditExecutionFrom(context.Background()).CPUWorkers != 1 {
		t.Fatal("reference path is not serial")
	}
	e := DefaultAuditExecution()
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.CPUWorkers = 0
	if e.Validate() == nil {
		t.Fatal("unbounded/invalid worker setting accepted")
	}
}

func TestAuditParallelOverlapsAndBoundsWorkers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	var active, peak atomic.Int32
	var calls [41]atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- AuditParallel(ctx, len(calls), 2, func(i int) error {
			n := active.Add(1)
			defer active.Add(-1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			if i < 2 {
				started <- struct{}{}
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			calls[i].Add(1)
			return nil
		})
	}()
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("workers did not overlap")
		}
	}
	if peak.Load() != 2 {
		t.Fatal("incorrect worker bound", peak.Load())
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	for i := range calls {
		if calls[i].Load() != 1 {
			t.Fatal("missing/duplicate job", i)
		}
	}
	if active.Load() != 0 || peak.Load() != 2 {
		t.Fatal("workers leaked or exceeded bound")
	}
}

func TestAuditParallelOrderedErrorsAndCancellation(t *testing.T) {
	first, second := errors.New("first"), errors.New("second")
	err := AuditParallel(context.Background(), 16, 2, func(i int) error {
		if i == 0 {
			time.Sleep(time.Millisecond)
			return first
		}
		if i == 1 {
			return second
		}
		return nil
	})
	if !errors.Is(err, first) {
		t.Fatal("completion order changed error", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var called atomic.Int32
	if err := AuditParallel(ctx, 16, 2, func(int) error { called.Add(1); return nil }); !errors.Is(err, context.Canceled) || called.Load() != 0 {
		t.Fatal("cancellation ignored", err)
	}
	for _, n := range []int{-1, 4097} {
		if AuditParallel(context.Background(), n, 2, func(int) error { return nil }) == nil {
			t.Fatal("unbounded tasks accepted")
		}
	}
}
