package durable

import (
	"fmt"
	"strings"
	"time"
)

// This is the complete tree set built by closeBlock and aggregation. Both
// optional trees are created together, under the fingerprinted Aggregation flag.
func segmentGenerationTrees(segment, height uint64, aggregation bool) []string {
	base := fmt.Sprintf("segment/%d/%d/", segment, height)
	trees := []string{base + "lg", base + "fk", base + "min", base + "max"}
	if aggregation {
		trees = append(trees, base+"generator", base+"extended-lg")
	}
	return trees
}

func generationCleanupPredicate(aggregation bool) string {
	if aggregation {
		return "scope=$1 AND tree IN ($2,$3,$4,$5,$6,$7)"
	}
	return "scope=$1 AND tree IN ($2,$3,$4,$5)"
}

// cleanupGeneration runs on the existing ordered SQL transaction, at exactly
// the former prefix-delete barrier. The existing (scope,tree,...) keys support
// equality lookup independently of text collation. No page reader changes.
func (w *writer) cleanupGeneration(segment, height uint64) error {
	trees := segmentGenerationTrees(segment, height, w.a.Options.Aggregation)
	if w.a.Options.GeneratorMode != "" && w.a.Options.Aggregation {
		trees = segmentGenerationTrees(segment, height, false)
		base := fmt.Sprintf("segment/%d/%d/", segment, height)
		trees = append(trees, base+"extended-lg")
		lids, err := w.objectLIDsThrough("block", segment, height%w.a.Options.SegmentSize)
		if err != nil {
			return err
		}
		for _, lid := range lids {
			trees = append(trees, base+"generator/"+lid)
		}
	}
	args := make([]any, 1, len(trees)+1)
	args[0] = w.a.Options.RunID
	for _, tree := range trees {
		args = append(args, tree)
	}
	predicate := generationCleanupPredicate(w.a.Options.Aggregation)
	if w.a.Options.GeneratorMode != "" && w.a.Options.Aggregation {
		params := make([]string, len(trees))
		for i := range params {
			params[i] = fmt.Sprintf("$%d", i+2)
		}
		predicate = "scope=$1 AND tree IN (" + strings.Join(params, ",") + ")"
	}
	if w.a.Metrics != nil {
		m := &w.a.Metrics.Access
		m.CleanupCalls.Add(1)
		m.CleanupTreesRequested.Add(uint64(len(trees)))
		m.CleanupSegment.Store(segment)
		m.CleanupGeneration.Store(height)
		started := time.Now()
		defer func() { m.CleanupNanos.Add(uint64(time.Since(started))) }()
	}
	for _, table := range []string{"index_pages", "index_leaves"} {
		started := time.Now()
		result, err := w.tx.ExecContext(w.ctx, "DELETE FROM blas."+table+" WHERE "+predicate, args...)
		if w.a.Metrics != nil {
			m := &w.a.Metrics.Access
			m.CleanupSQLCalls.Add(1)
			elapsed := uint64(time.Since(started))
			if table == "index_pages" {
				m.CleanupPagesNanos.Add(elapsed)
			} else {
				m.CleanupLeavesNanos.Add(elapsed)
			}
			if err == nil {
				// PostgreSQL RowsAffected is available without an extra query.
				if rows, e := result.RowsAffected(); e == nil {
					m.CleanupRowsDeleted.Add(uint64(rows))
					if table == "index_pages" {
						m.CleanupPageRowsDeleted.Add(uint64(rows))
					} else {
						m.CleanupLeafRowsDeleted.Add(uint64(rows))
					}
				}
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
