package auditor

import (
	"sync/atomic"
	"testing"
	"time"
)

// AUDIT-002/DEV-09: audits run periodically with a configured period.
func TestSchedulerRunsPeriodically(t *testing.T) {
	var calls int32
	s := NewScheduler(20*time.Millisecond, func() []uint64 {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	s.Start()
	time.Sleep(110 * time.Millisecond)
	s.Stop()

	got := atomic.LoadInt32(&calls)
	if got < 2 {
		t.Fatalf("scheduler fired %d times in ~110ms at a 20ms period, want >= 2", got)
	}
}

// DEV-09: on-demand is a test/CLI-only trigger, separate from the automatic
// periodic mechanism.
func TestSchedulerTriggerNow(t *testing.T) {
	var calls int32
	s := NewScheduler(time.Hour, func() []uint64 { // long period: only TriggerNow should fire
		atomic.AddInt32(&calls, 1)
		return []uint64{7}
	})
	errList := s.TriggerNow()
	if len(errList) != 1 || errList[0] != 7 {
		t.Fatalf("TriggerNow returned %v, want [7]", errList)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("run called %d times, want 1", calls)
	}
}

func TestSchedulerNoOverlap(t *testing.T) {
	var concurrent int32
	var maxConcurrent int32
	s := NewScheduler(5*time.Millisecond, func() []uint64 {
		n := atomic.AddInt32(&concurrent, 1)
		for {
			cur := atomic.LoadInt32(&maxConcurrent)
			if n <= cur || atomic.CompareAndSwapInt32(&maxConcurrent, cur, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond) // longer than the period
		atomic.AddInt32(&concurrent, -1)
		return nil
	})
	s.Start()
	time.Sleep(120 * time.Millisecond)
	s.Stop()

	if got := atomic.LoadInt32(&maxConcurrent); got > 1 {
		t.Fatalf("max concurrent audit passes = %d, want <= 1 (no overlap)", got)
	}
}

func TestSchedulerResultsRecorded(t *testing.T) {
	s := NewScheduler(time.Hour, func() []uint64 { return []uint64{1, 2} })
	s.TriggerNow()
	s.TriggerNow()
	results := s.Results()
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
}

func TestSchedulerStopIdempotent(t *testing.T) {
	s := NewScheduler(10*time.Millisecond, func() []uint64 { return nil })
	s.Start()
	s.Stop()
	s.Stop() // must not hang or panic
}

func TestSchedulerZeroPeriodDoesNotStart(t *testing.T) {
	var calls int32
	s := NewScheduler(0, func() []uint64 {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	s.Start()
	time.Sleep(20 * time.Millisecond)
	s.Stop()
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("scheduler with zero period fired automatically")
	}
}
