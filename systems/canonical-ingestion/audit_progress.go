package canonicalingestion

import (
	"context"
	"fmt"
	"io"
)

type auditProgressKey struct{}
type auditProgress struct {
	out   io.Writer
	label string
}

// WithAuditProgress enables concise per-measurement output. Library callers
// remain silent by default; commands use stderr to keep stdout JSON valid.
func WithAuditProgress(ctx context.Context, out io.Writer, label string) context.Context {
	return context.WithValue(ctx, auditProgressKey{}, auditProgress{out, label})
}

// Called only after measurement, memory sampling and correctness checks finish.
func writeAuditProgress(ctx context.Context, report AuditReport, metrics AuditMetrics) error {
	progress, ok := ctx.Value(auditProgressKey{}).(auditProgress)
	if !ok || progress.out == nil {
		return nil
	}
	prefix := report.Scheme
	if progress.label != "" {
		prefix = progress.label + " scheme=" + report.Scheme
	}
	_, err := fmt.Fprintf(progress.out, "%s measured=%d/%d n=%d end_to_end_ms=%.3f\n", prefix, len(report.Samples), report.Runs, report.AuditSize, float64(metrics.EndToEnd)/1e6)
	return err
}
