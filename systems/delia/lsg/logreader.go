// Package lsg implements the LSG (Log Serialization, State Generation)
// phase (LSG-001..012; §3.3 p. 3289). LSG runs only at the network layer: no
// ledger interaction happens in this package (LSG-001).
//
// LSG stage 1 (log serialization) is split in this reproduction:
//   - a server storing its own generated record hash in its own
//     LogCacheQueue (LSG-004) is Tier A and implemented by LogReader below.
//   - broadcasting that hash to other servers and a receiver placing it
//     (LSG-002, LSG-003, LSG-005) is BLOCKED by GAP-05 (OPEN: payload fields
//     and out-of-order/missing handling are not decided) and is
//     intentionally NOT implemented here. Do not invent a wire format for
//     it.
//
// LSG stage 2 (state generation, LSG-006..012) is Tier A and is implemented
// in stage2.go, built against an already-populated psi however it got there
// (a real deployment would populate it via the still-blocked stage 1
// broadcast; tests populate it directly through logcache.Store.SetCurrent,
// which is the in-process multi-node test harness GAP-54 calls for).
package lsg

import (
	"bufio"
	"io"
	"sync"

	"delia/hash"
	"delia/logcache"
)

// BatchHandler is invoked synchronously (GO-003/GO-004: no goroutine
// parallelizes a protocol step) whenever the reader's own batch fills, with
// the completed batch's digests in record order.
type BatchHandler func(batch []hash.Digest)

// LogReader is DEV-08's "log reader": there is no separate log collector,
// preprocessor or verification module (SYS-007). "Enable" starts this
// reader; "close" stops it, coupled with Init and Close respectively
// (neither of which this package implements — that coupling belongs to the
// HMSC closure client, hmsc/closure, once built).
type LogReader struct {
	self   logcache.ServerID
	cache  *logcache.Store
	onFull BatchHandler

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// NewLogReader creates a reader that hashes records for self (LSG-004: a
// server stores its own generated hash in its own LogCacheQueue) and reports
// completed batches to onFull.
func NewLogReader(self logcache.ServerID, cache *logcache.Store, onFull BatchHandler) *LogReader {
	return &LogReader{self: self, cache: cache, onFull: onFull}
}

// Start begins reading newline-delimited records from src sequentially,
// hashing each one (GAP-03(a): SHA-256 over the UTF-8 line, excluding
// line-ending bytes — bufio.Scanner's default split strips them) and storing
// it in the local server's own LogCacheQueue. It runs until Stop is called
// or src is exhausted. Start returns immediately; reading happens on its own
// goroutine (I/O concurrency to model an independently-running server, not
// algorithm parallelism — GAP-54).
func (r *LogReader) Start(src io.Reader) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.mu.Unlock()

	go r.run(src)
}

func (r *LogReader) run(src io.Reader) {
	defer close(r.doneCh)
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		select {
		case <-r.stopCh:
			return
		default:
		}
		line := scanner.Bytes()
		digest := logcache.RecordHash(line)
		full, batch := r.cache.Append(r.self, digest)
		if full && r.onFull != nil {
			r.onFull(batch)
		}
	}
}

// Stop stops the reader (DEV-08: "close" is coupled with Close, stopping
// this reader). It blocks until the read loop has exited.
func (r *LogReader) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stopCh)
	doneCh := r.doneCh
	r.mu.Unlock()
	<-doneCh
}
