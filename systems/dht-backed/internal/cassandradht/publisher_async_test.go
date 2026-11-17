package cassandradht

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"dhtbacked/internal/hashing"
)

type blockingProofWriter struct {
	entered      chan []byte
	release      chan struct{}
	mu           sync.Mutex
	active, peak int
	values       map[hashing.Digest][]byte
	fail         error
}

func (s *blockingProofWriter) PutProof(ctx context.Context, k hashing.Digest, b []byte) error {
	s.mu.Lock()
	s.active++
	s.peak = max(s.peak, s.active)
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.active--; s.mu.Unlock() }()
	select {
	case s.entered <- append([]byte(nil), b...):
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	if old, ok := s.values[k]; ok && !bytes.Equal(old, b) {
		return errors.New("conflicting replay")
	}
	s.values[k] = append([]byte(nil), b...)
	return nil
}
func awaitProofEvent[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("proof worker/barrier hung")
		var zero T
		return zero
	}
}
func asyncFixture(t *testing.T, workers, requests, budget int) (*Publisher, *blockingProofWriter) {
	t.Helper()
	s := &blockingProofWriter{entered: make(chan []byte, 32), release: make(chan struct{}), values: make(map[hashing.Digest][]byte)}
	l := DefaultLimits()
	l.Workers = workers
	l.MaxRequests = requests
	l.QueueBytes = budget
	p, err := NewPublisher(context.Background(), s, l)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, s
}

func TestAsyncPublisherStartsBeforeFlushAndOwnsPayload(t *testing.T) {
	p, s := asyncFixture(t, 2, 4, 4096)
	b := []byte{1, 2, 3}
	key := hashing.H([]byte("one"))
	if err := p.Put(key, b); err != nil {
		t.Fatal(err)
	}
	b[0] = 99
	if got := awaitProofEvent(t, s.entered); !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatal("payload not owned", got)
	}
	flushed := make(chan error, 1)
	go func() { flushed <- p.Flush() }()
	select {
	case err := <-flushed:
		t.Fatal("Flush acknowledged blocked write", err)
	default:
	}
	close(s.release)
	if err := awaitProofEvent(t, flushed); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	outstanding, queued := p.count, p.bytes
	p.mu.Unlock()
	if outstanding != 0 || queued != 0 {
		t.Fatal("payloads retained after Flush", outstanding, queued)
	}
	if err := p.Flush(); err != nil {
		t.Fatal("empty Flush failed", err)
	}
	if err := p.Put(key, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := p.Flush(); err != nil {
		t.Fatal("new cycle or replay failed", err)
	}
}

func TestAsyncPublisherBoundedBackpressureAndClose(t *testing.T) {
	p, s := asyncFixture(t, 8, 1, 257)
	if err := p.Put(hashing.Digest{1}, []byte{1}); err != nil {
		t.Fatal(err)
	}
	awaitProofEvent(t, s.entered)
	started, put := make(chan struct{}), make(chan error, 1)
	go func() { close(started); put <- p.Put(hashing.Digest{2}, []byte{2}) }()
	<-started
	select {
	case err := <-put:
		t.Fatal("backpressure failed", err)
	default:
	}
	p.mu.Lock()
	n, b := p.count, p.bytes
	p.mu.Unlock()
	if n != 1 || b != 257 {
		t.Fatal("in-flight reservation not bounded", n, b)
	}
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	if err := awaitProofEvent(t, put); err == nil {
		t.Fatal("blocked producer survived Close")
	}
	awaitProofEvent(t, closed)
	if err := p.Flush(); err == nil {
		t.Fatal("closed publisher acknowledged Flush")
	}
	s.mu.Lock()
	active, peak := s.active, s.peak
	s.mu.Unlock()
	if active != 0 || peak > 1 {
		t.Fatal("worker/request bound or shutdown failed", active, peak)
	}
}

func TestAsyncPublisherFailurePreventsEpochAcknowledgement(t *testing.T) {
	p, s := asyncFixture(t, 2, 8, 4096)
	s.fail = errors.New("injected LWT timeout")
	for i := byte(0); i < 2; i++ {
		if err := p.Put(hashing.Digest{i}, []byte{i}); err != nil {
			t.Fatal(err)
		}
	}
	awaitProofEvent(t, s.entered)
	awaitProofEvent(t, s.entered)
	close(s.release)
	if err := p.Flush(); !errors.Is(err, s.fail) {
		t.Fatal("failed write acknowledged", err)
	}
	if err := p.Put(hashing.Digest{4}, []byte{4}); err == nil {
		t.Fatal("failure silently retried in same publisher")
	}
	p.mu.Lock()
	n, b := p.count, p.bytes
	p.mu.Unlock()
	if n != 0 || b != 0 {
		t.Fatal("failed queue was not released", n, b)
	}
	s.mu.Lock()
	active, peak := s.active, s.peak
	s.mu.Unlock()
	if active != 0 || peak > 2 {
		t.Fatal("worker bound/join failed", active, peak)
	}
}
