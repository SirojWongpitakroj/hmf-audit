package auditor

import (
	"sync"
	"time"
)

// RunFunc performs one audit pass (typically: gather records/states, then
// call LogVerify) and reports its ErrList.
type RunFunc func() []uint64

// Scheduler runs audits periodically, per DEV-09/GAP-17: "audits run
// periodically with a configured period; on-demand audit is a test/CLI
// trigger only." §3.4 stage 2 also describes on-demand access ("At any time,
// an external auditor could access the audit phase") — that path is
// TriggerNow, reserved for tests/CLI use, not a second automatic mechanism.
//
// Audit passes never overlap: if a pass is still running when the next tick
// arrives, that tick is skipped rather than starting a second concurrent
// pass (AUDIT-010/GO-003: the audit itself runs on a single goroutine, with
// no parallel batches — Scheduler must not turn that into two audits running
// at once).
type Scheduler struct {
	period time.Duration
	run    RunFunc

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
	busy    bool

	resultsMu sync.Mutex
	results   [][]uint64
}

// NewScheduler creates a periodic audit scheduler. period must be positive
// (AUDIT-002/DEV-09: a configured period, no protocol default).
func NewScheduler(period time.Duration, run RunFunc) *Scheduler {
	return &Scheduler{period: period, run: run}
}

// Start begins the periodic trigger. It is a no-op if already running.
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running || s.period <= 0 {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	s.mu.Unlock()

	go s.loop()
}

func (s *Scheduler) loop() {
	defer close(s.doneCh)
	ticker := time.NewTicker(s.period)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runOnce()
		}
	}
}

func (s *Scheduler) runOnce() {
	s.mu.Lock()
	if s.busy {
		s.mu.Unlock()
		return // skip: a pass is still in flight, no overlap
	}
	s.busy = true
	s.mu.Unlock()

	errList := s.run()

	s.resultsMu.Lock()
	s.results = append(s.results, errList)
	s.resultsMu.Unlock()

	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
}

// TriggerNow runs one audit pass immediately and returns its ErrList
// (DEV-09: on-demand is a test/CLI-only trigger, not part of the automatic
// periodic mechanism). It blocks until the pass completes and does not run
// concurrently with a periodic pass.
func (s *Scheduler) TriggerNow() []uint64 {
	s.mu.Lock()
	for s.busy {
		s.mu.Unlock()
		time.Sleep(time.Millisecond)
		s.mu.Lock()
	}
	s.busy = true
	s.mu.Unlock()

	errList := s.run()

	s.resultsMu.Lock()
	s.results = append(s.results, errList)
	s.resultsMu.Unlock()

	s.mu.Lock()
	s.busy = false
	s.mu.Unlock()
	return errList
}

// Results returns every ErrList recorded so far, in run order.
func (s *Scheduler) Results() [][]uint64 {
	s.resultsMu.Lock()
	defer s.resultsMu.Unlock()
	out := make([][]uint64, len(s.results))
	copy(out, s.results)
	return out
}

// Stop stops the periodic trigger. It blocks until the loop goroutine exits.
// It does not interrupt an in-flight audit pass.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	doneCh := s.doneCh
	s.mu.Unlock()
	<-doneCh
}
