package diskindex

import (
	contract "canonicalingestion"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// WriteBatch belongs to exactly one SQL transaction. It is never a shared read
// cache. Finish normally flushes; the ordered writer may hold it across builders
// proven not to read pending pages, then flush at the next dependency barrier.
type WriteBatch struct {
	// HoldFinish combines compatible builders. Hard row/byte limits still flush;
	// the writer must explicitly flush before a page reader or a commit boundary.
	HoldFinish                     bool
	q                              SQL
	rows, bytes, maxRows, maxBytes int
	pages, leaves                  [][]any
	metrics                        *contract.IngestMetrics
	err                            error
}

func (b *WriteBatch) FinishTree(ctx context.Context) error {
	if b.HoldFinish {
		if b.metrics != nil {
			b.metrics.Access.IndexFinishFlushesHeld.Add(1)
		}
		return b.err
	}
	return b.flush(ctx, "finish")
}

func NewWriteBatch(q SQL, records, bytes int, metrics *contract.IngestMetrics) (*WriteBatch, error) {
	if q == nil || records < 1 || bytes < 1 {
		return nil, fmt.Errorf("diskindex: invalid write batch")
	}
	return &WriteBatch{q: q, maxRows: records, maxBytes: bytes, metrics: metrics}, nil
}
func (b *WriteBatch) append(ctx context.Context, page bool, args []any, cost int) error {
	if b.err != nil {
		return b.err
	}
	if cost > b.maxBytes {
		return fmt.Errorf("diskindex: one row exceeds write batch budget")
	}
	if b.rows >= b.maxRows || b.bytes > b.maxBytes-cost {
		if err := b.flush(ctx, "capacity"); err != nil {
			return err
		}
	}
	if page {
		b.pages = append(b.pages, args)
	} else {
		b.leaves = append(b.leaves, args)
	}
	b.rows++
	b.bytes += cost
	if b.metrics != nil {
		b.metrics.Access.ObserveIndexBatch(int64(b.rows), int64(b.bytes))
	}
	return nil
}
func (b *WriteBatch) page(ctx context.Context, scope, tree, page string, payload []byte) error {
	b.observeRow(tree, true, len(payload))
	return b.append(ctx, true, []any{scope, tree, page, slices.Clone(payload)}, len(scope)+len(tree)+len(page)+len(payload)+256)
}
func (b *WriteBatch) leaf(ctx context.Context, scope, tree string, pos int64, keyword []byte, value int64) error {
	b.observeRow(tree, false, len(keyword))
	return b.append(ctx, false, []any{scope, tree, pos, slices.Clone(keyword), value}, len(scope)+len(tree)+len(keyword)+256)
}
func (b *WriteBatch) Flush(ctx context.Context) error {
	return b.flush(ctx, "barrier")
}

func (b *WriteBatch) observeRow(tree string, page bool, bytes int) {
	if b.metrics == nil {
		return
	}
	m := &b.metrics.Access
	if page {
		m.IndexPageRows.Add(1)
	} else {
		m.IndexLeafRows.Add(1)
	}
	m.IndexPayloadBytes.Add(uint64(bytes))
	switch {
	case strings.HasPrefix(tree, "file/"):
		m.FileIndexRows.Add(1)
	case strings.HasPrefix(tree, "block/"):
		m.BlockIndexRows.Add(1)
	case strings.HasPrefix(tree, "segment/"):
		m.SegmentIndexRows.Add(1)
	}
}

func (b *WriteBatch) flush(ctx context.Context, reason string) error {
	if b.err != nil {
		return b.err
	}
	if b.rows == 0 {
		return nil
	}
	if b.metrics != nil {
		started := time.Now()
		before := b.metrics.SQLBatches.Load()
		b.metrics.Access.IndexFlushes.Add(1)
		switch reason {
		case "finish":
			b.metrics.Access.IndexFinishFlushes.Add(1)
		case "capacity":
			b.metrics.Access.IndexCapacityFlushes.Add(1)
		default:
			b.metrics.Access.IndexBarrierFlushes.Add(1)
		}
		defer func() {
			b.metrics.Access.IndexFlushNanos.Add(uint64(time.Since(started)))
			b.metrics.Access.IndexInsertCalls.Add(b.metrics.SQLBatches.Load() - before)
		}()
	}
	b.err = contract.InsertRows(ctx, b.q, `INSERT INTO blas.index_pages(scope,tree,page,payload) VALUES `, "", b.pages, b.maxRows, b.maxBytes, b.metrics)
	if b.err != nil {
		return b.err
	}
	b.err = contract.InsertRows(ctx, b.q, `INSERT INTO blas.index_leaves(scope,tree,pos,keyword,value) VALUES `, "", b.leaves, b.maxRows, b.maxBytes, b.metrics)
	if b.err != nil {
		return b.err
	}
	b.pages = nil
	b.leaves = nil
	b.rows = 0
	b.bytes = 0
	return nil
}
