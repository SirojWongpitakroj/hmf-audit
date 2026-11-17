package canonicalingestion

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type metricsContextKey struct{}
type queryStartKey struct{}
type commitQueryKey struct{}

func WithIngestMetrics(ctx context.Context, m *IngestMetrics) context.Context {
	return context.WithValue(ctx, metricsContextKey{}, m)
}
func metricsInContext(ctx context.Context) *IngestMetrics {
	m, _ := ctx.Value(metricsContextKey{}).(*IngestMetrics)
	return m
}

type sqlMetricsTracer struct{ m *IngestMetrics }

func (t sqlMetricsTracer) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	q := data.SQL
	ctx = context.WithValue(ctx, commitQueryKey{}, strings.EqualFold(strings.TrimSpace(q), "commit"))
	switch {
	case strings.HasPrefix(strings.TrimSpace(q), "SELECT"):
		t.m.Access.SQLReadCalls.Add(1)
	case strings.HasPrefix(strings.TrimSpace(q), "INSERT"), strings.HasPrefix(strings.TrimSpace(q), "UPDATE"), strings.HasPrefix(strings.TrimSpace(q), "DELETE"):
		t.m.Access.SQLWriteCalls.Add(1)
	}
	if strings.HasPrefix(strings.TrimSpace(q), "SELECT") {
		if strings.Contains(q, "blas.durable_files") {
			t.m.Access.FileMetadataQueries.Add(1)
		}
		if strings.Contains(q, "blas.durable_keywords") || strings.Contains(q, "keywords::text") {
			t.m.Access.KeywordMetadataQueries.Add(1)
		}
		if strings.Contains(q, "blas.durable_objects") && (strings.Contains(q, "ts_min") || strings.Contains(q, "ts_max")) {
			t.m.Access.TimestampQueries.Add(1)
		}
	}
	n := t.m.InFlight.Add(1)
	peak(&t.m.PeakInFlight, n)
	return context.WithValue(ctx, queryStartKey{}, time.Now())
}
func (t sqlMetricsTracer) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	t.m.InFlight.Add(-1)
	if start, ok := ctx.Value(queryStartKey{}).(time.Time); ok {
		elapsed := time.Since(start)
		if commit, _ := ctx.Value(commitQueryKey{}).(bool); commit {
			t.m.Access.TransactionCommitCalls.Add(1)
			t.m.Access.TransactionCommitNanos.Add(uint64(elapsed))
			if data.Err != nil {
				t.m.Access.TransactionCommitFailures.Add(1)
			}
		}
		t.m.SQL.Observe(elapsed, data.Err)
		t.m.Access.SQLStatementNanos.Add(uint64(elapsed))
	}
}
