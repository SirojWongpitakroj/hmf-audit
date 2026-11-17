package canonicalingestion

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"testing"
	"time"
)

func TestAuditProgressFormatAndQuietDefault(t *testing.T) {
	for _, scheme := range []string{"BLAS", "DELIA", "DHT-backed"} {
		var out bytes.Buffer
		ctx := WithAuditProgress(context.Background(), &out, "EXP1B_1M")
		report := AuditReport{Scheme: scheme, Runs: 30, AuditSize: 1000, Samples: make([]AuditSample, 2)}
		metrics := AuditMetrics{EndToEnd: 10051414 * time.Microsecond}
		if err := writeAuditProgress(ctx, report, metrics); err != nil {
			t.Fatal(err)
		}
		want := "EXP1B_1M scheme=" + scheme + " measured=2/30 n=1000 end_to_end_ms=10051.414\n"
		if out.String() != want {
			t.Fatalf("got %q want %q", out.String(), want)
		}
		out.Reset()
		if err := writeAuditProgress(context.Background(), report, metrics); err != nil || out.Len() != 0 {
			t.Fatal("library unexpectedly logged", err)
		}
	}
	f := BindAuditFlags(flag.NewFlagSet("audit", flag.ContinueOnError))
	if !*f.Progress {
		t.Fatal("command progress is not enabled by default")
	}
}

type brokenAuditWriter struct{}

func (brokenAuditWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestAuditProgressPropagatesOutputErrors(t *testing.T) {
	err := writeAuditProgress(WithAuditProgress(context.Background(), brokenAuditWriter{}, ""), AuditReport{Scheme: "BLAS"}, AuditMetrics{})
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal("output error lost", err)
	}
}
