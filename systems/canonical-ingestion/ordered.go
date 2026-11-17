package canonicalingestion

import (
	"context"
	"fmt"
	"sync"
)

// Ordered prepares a bounded batch concurrently and consumes it in input order.
// Reservation includes both input and maximum output ownership. Credits are
// released only by ordered consumption, never by worker completion: a slow head
// therefore cannot create an unbounded reorder map. Workers never advance a
// source cursor. A callback with external effects must use its native durable
// recovery protocol (as DHT proof publication does with its SQL outbox).
// The caller must bound the input slice separately (normally EffectiveLimits).
func Ordered[T, R any](ctx context.Context, e Execution, m *IngestMetrics, items []T,
	reserve func(T) int, prepare func(context.Context, T) (R, int, error), consume func(int, R) error) error {
	if err := e.Validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type task struct{ i, bytes int }
	type result struct {
		task
		value R
		err   error
	}
	jobs := make(chan task, min(e.InputQueueRecords, len(items)))
	results := make(chan result, min(e.PreparedQueueRecords, len(items)))
	var wg sync.WaitGroup
	for range min(e.CPUWorkers, len(items)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case t, ok := <-jobs:
					if !ok {
						return
					}
					v, n, err := prepare(ctx, items[t.i])
					if n < 0 || n > t.bytes {
						err = fmt.Errorf("prepared record %d exceeds reserved byte budget", t.i)
					}
					select {
					case results <- result{t, v, err}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	defer func() {
		cancel()
		close(jobs)
		wg.Wait()
		if m != nil {
			m.Pipeline(0, 0, 0, 0)
		}
	}()
	window := min(e.ReorderMaxRecords, len(items))
	byteLimit := e.PreparationBytes()
	pending := make(map[int]result, window)
	next, sent, heldBytes := 0, 0, 0
	for next < len(items) {
		var send chan task
		var t task
		if sent < len(items) && sent-next < window {
			n := reserve(items[sent])
			if n <= 0 || n > byteLimit {
				return fmt.Errorf("record %d cannot fit preparation byte budget", sent)
			}
			if heldBytes <= byteLimit-n {
				send, t = jobs, task{sent, n}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case send <- t:
			sent++
			heldBytes += t.bytes
		case r := <-results:
			if r.err != nil {
				return fmt.Errorf("prepare record %d: %w", r.i, r.err)
			}
			pending[r.i] = r
			for {
				r, ok := pending[next]
				if !ok {
					break
				}
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := consume(next, r.value); err != nil {
					return err
				}
				delete(pending, next)
				heldBytes -= r.bytes
				next++
			}
		}
		if m != nil {
			m.Pipeline(int64(len(jobs)), int64(len(results)), int64(len(pending)), int64(heldBytes))
		}
	}
	if m != nil {
		m.Pipeline(0, 0, 0, 0)
	}
	return nil
}

// PrepareSlice retains at most one explicit byte-bounded batch. Unlike a
// streaming consumer it keeps outputs, so the whole batch is checked against
// the byte budget before any workers start.
func PrepareSlice[T, R any](ctx context.Context, e *Execution, m *IngestMetrics, items []T, reserve func(T) int, prepare func(context.Context, T) (R, int, error)) ([]R, error) {
	if e == nil {
		out := make([]R, len(items))
		for i, v := range items {
			r, _, err := prepare(ctx, v)
			if err != nil {
				return nil, err
			}
			out[i] = r
		}
		return out, nil
	}
	if err := e.Validate(); err != nil {
		return nil, err
	}
	if len(items) > e.BatchMaxRecords {
		return nil, fmt.Errorf("preparation batch record limit exceeded")
	}
	total := 0
	for _, v := range items {
		n := reserve(v)
		if n <= 0 || n > e.PreparationBytes()-total {
			return nil, fmt.Errorf("preparation batch byte limit exceeded")
		}
		total += n
	}
	out := make([]R, len(items))
	err := Ordered(ctx, *e, m, items, reserve, prepare, func(i int, r R) error { out[i] = r; return nil })
	if err != nil {
		return nil, err
	}
	return out, nil
}
