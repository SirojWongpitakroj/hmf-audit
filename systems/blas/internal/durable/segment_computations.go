package durable

import "time"

func (w *writer) beginPositionSample() {
	w.positionStarted = time.Now()
	w.positionRecords = w.state.Records
	if w.a.Metrics != nil {
		w.positionSQL = w.a.Metrics.SQL.Count.Load()
	}
	if w.a.SegmentComputations != nil {
		w.positionHashes = w.a.SegmentComputations.Stats()
	}
}

func (w *writer) samplePosition(height uint64, indexTime time.Duration, completedBlock bool) {
	if w.a.Metrics == nil || w.positionStarted.IsZero() {
		return
	}
	records := w.state.Records - w.positionRecords
	if records == 0 && !completedBlock {
		return
	}
	now := time.Now()
	calls := w.a.Metrics.SQL.Count.Load()
	blocks := uint64(0)
	if completedBlock {
		blocks = 1
	}
	w.a.Metrics.Access.ObserveSegmentPosition(height%w.a.Options.SegmentSize+1, records, blocks, uint64(now.Sub(w.positionStarted)), uint64(indexTime), calls-w.positionSQL)
	if c := w.a.SegmentComputations; c != nil {
		current := c.Stats()
		before := w.positionHashes
		a := &w.a.Metrics.Access
		a.SegmentHashRequests.Add(current.Requests - before.Requests)
		a.SegmentHashHits.Add(current.Hits - before.Hits)
		a.SegmentHashComputations.Add(current.Computations - before.Computations)
		a.SegmentHashedBytes.Add(current.HashedBytes - before.HashedBytes)
		a.SegmentHashNanos.Add(current.HashNanos - before.HashNanos)
		a.ObserveSegmentHashCacheBytes(int64(current.Bytes))
		w.positionHashes = current
	}
	w.positionStarted = now
	w.positionRecords = w.state.Records
	w.positionSQL = calls
}
