package canonicalingestion

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

func TestOrderedOutOfOrderBackpressureAndByteBounds(t *testing.T) {
	e := DefaultExecution()
	e.CPUWorkers = 4
	e.InputQueueRecords = 2
	e.PreparedQueueRecords = 2
	e.ReorderMaxRecords = 7
	e.QueueMaxBytes = 700
	e.ReorderMaxBytes = 700
	m := &IngestMetrics{}
	monitor := StartMemoryMonitor(time.Millisecond)
	defer func() {
		t.Logf("BACKPRESSURE peak_input=%d peak_prepared=%d peak_reorder=%d peak_reserved_bytes=%d peak_rss_bytes=%d", m.PeakInput.Load(), m.PeakPrepared.Load(), m.PeakReorder.Load(), m.PeakReservedBytes.Load(), monitor.Stop())
	}()
	items := make([]int, 10000)
	for i := range items {
		items[i] = i
	}
	head, started := make(chan struct{}), make(chan struct{})
	var prepared, active, maxActive atomic.Int64
	done := make(chan error, 1)
	go func() {
		next := 0
		done <- Ordered(context.Background(), e, m, items, func(int) int { return 100 }, func(ctx context.Context, n int) (int, int, error) {
			a := active.Add(1)
			peak(&maxActive, a)
			defer active.Add(-1)
			if n == 0 {
				close(started)
				select {
				case <-head:
				case <-ctx.Done():
					return 0, 0, ctx.Err()
				}
			}
			prepared.Add(1)
			return n, 100, nil
		}, func(i, n int) error {
			if i != next || n != next {
				return fmt.Errorf("order gap: %d/%d/%d", i, n, next)
			}
			next++
			return nil
		})
	}()
	<-started
	deadline := time.Now().Add(time.Second)
	expectedBlocked := int64(min(e.ReorderMaxRecords, e.PreparationBytes()/100) - 1)
	for prepared.Load() < expectedBlocked && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := prepared.Load(); n > expectedBlocked {
		t.Fatalf("slow head failed to backpressure workers: %d", n)
	}
	close(head)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pipeline deadlocked")
	}
	if maxActive.Load() > 4 || maxActive.Load() < 2 {
		t.Fatal("worker limit/concurrency", maxActive.Load())
	}
	if m.PeakReservedBytes.Load() > 700 || m.PeakReorder.Load() > 7 || m.PeakInput.Load() > 2 || m.PeakPrepared.Load() > 2 {
		t.Fatalf("pipeline bounds violated: %+v", m)
	}
}

func TestOrderedFailureAndCancellationNeverSkip(t *testing.T) {
	for _, failure := range []int{0, 3, 19} {
		e := DefaultExecution()
		e.CPUWorkers = 8
		e.ReorderMaxRecords = 4
		var consumed []int
		items := make([]int, 20)
		for i := range items {
			items[i] = i
		}
		err := Ordered(context.Background(), e, nil, items, func(int) int { return 64 }, func(ctx context.Context, n int) (int, int, error) {
			if n == failure {
				return 0, 0, errors.New("injected preparation failure")
			}
			return n, 64, nil
		}, func(i, n int) error { consumed = append(consumed, n); return nil })
		if err == nil {
			t.Fatal("preparation failure accepted")
		}
		for i, n := range consumed {
			if n != i || n >= failure {
				t.Fatal("cursor skipped failed record", consumed)
			}
		}
	}
	e := DefaultExecution()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Ordered(ctx, e, nil, []int{1}, func(int) int { return 1 }, func(ctx context.Context, n int) (int, int, error) { return n, 1, nil }, func(int, int) error { return nil }); err == nil {
		t.Fatal("cancellation lost")
	}
}

func TestOrderedRejectsOversizeInputAndOutput(t *testing.T) {
	e := DefaultExecution()
	e.QueueMaxBytes = 100
	e.ReorderMaxBytes = 100
	for _, input := range []int{101, 50} {
		err := Ordered(context.Background(), e, nil, []int{input}, func(n int) int { return n }, func(context.Context, int) (int, int, error) { return 1, 101, nil }, func(int, int) error { t.Fatal("oversized result consumed"); return nil })
		if err == nil {
			t.Fatal("oversize accepted")
		}
	}
}

func TestMemoEvictsAndDisables(t *testing.T) {
	m := NewMemo(2, 300)
	m.Store("a", "1")
	m.Store("b", "2")
	m.Load("a")
	m.Store("c", "3")
	if _, ok := m.Load("b"); ok {
		t.Fatal("LRU entry retained")
	}
	n, b := m.Usage()
	if n > 2 || b > 300 {
		t.Fatal("cache exceeded limits", n, b)
	}
	m.Store("huge", string(make([]byte, 301)))
	if _, ok := m.Load("huge"); ok {
		t.Fatal("oversized cache entry retained")
	}
	disabled := NewMemo(0, 0)
	disabled.Store("x", "y")
	if n, _ := disabled.Usage(); n != 0 {
		t.Fatal("disabled cache populated")
	}
}

func TestExecutionSettingsAreBounded(t *testing.T) {
	e := DefaultExecution()
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	e.IOWorkers = 65
	if e.Validate() == nil {
		t.Fatal("unbounded storage workers")
	}
	e = DefaultExecution()
	e.SQLPoolSize = 1
	if e.Validate() == nil {
		t.Fatal("pool cannot support lease plus transaction")
	}
}
