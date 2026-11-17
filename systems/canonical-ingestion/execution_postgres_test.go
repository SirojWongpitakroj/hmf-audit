package canonicalingestion_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	contract "canonicalingestion"
	"canonicalingestion/testfixture"
)

func TestIngestionReusesBaselineSourcePoolAndReportsMetrics(t *testing.T) {
	_, dsn := testfixture.Database(t, "CANONICAL_TEST_POSTGRES_DSN")
	t.Setenv("BASELINES_POSTGRES_DSN", dsn)
	t.Setenv("CANONICAL_STREAM_DSN", "")
	for _, suffix := range []string{"HOST", "PORT", "DATABASE", "USER", "PASSWORD", "SSLMODE"} {
		t.Setenv("CANONICAL_STREAM_"+suffix, "")
	}
	e := contract.DefaultExecution()
	e.SQLPoolSize = 3
	e.MetricsInterval = 0
	m := &contract.IngestMetrics{}
	ctx := contract.WithIngestMetrics(context.Background(), m)
	source, native, closePools, err := contract.OpenIngestionDatabases(ctx, e.SQLPoolSize)
	if err != nil {
		t.Fatal(err)
	}
	defer closePools()
	if source != native {
		t.Fatal("same baseline/source database opened two pools")
	}
	var one int
	if err := source.QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatal(err)
	}
	var output bytes.Buffer
	stop := m.Reporter(ctx, &output, e, map[string]*sql.DB{"native": native})
	m.Committed(3, 300)
	m.TrackPendingBatchBytes(1024)
	m.TrackCache(2, 256)
	stop()
	var report struct {
		Records                               uint64 `json:"records_committed_this_process"`
		PostgreSQL                            struct{ Count uint64 }
		PeakPendingBatchBytes, PeakCacheBytes int64
		Databases                             map[string]sql.DBStats
	}
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal("invalid telemetry", err, output.String())
	}
	if report.Records != 3 || report.PostgreSQL.Count == 0 || report.PeakPendingBatchBytes != 1024 || report.PeakCacheBytes != 256 || report.Databases["native"].MaxOpenConnections != 3 {
		t.Fatal("missing/non-sensible telemetry", output.String())
	}
}
