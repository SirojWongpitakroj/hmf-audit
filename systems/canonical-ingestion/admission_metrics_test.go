package canonicalingestion

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestAdmissionMetricsReportedIndependently(t *testing.T) {
	m := &IngestMetrics{}
	m.SQLTraced.Store(true)
	m.SQL.Observe(9*time.Millisecond, nil)
	m.Admission.Observe(time.Millisecond, nil)
	m.Admission.Observe(3*time.Millisecond, context.Canceled)
	m.AdmissionBatch.Observe(4*time.Millisecond, nil)
	m.BatchAdmittedRecords.Add(64)
	// Existing tracer ownership must not suppress the admission-only metric or
	// double-count SQL calls through the manual fallback.
	m.ObserveSQL(time.Second, nil)
	var output bytes.Buffer
	e := DefaultExecution()
	e.MetricsInterval = 0
	stop := m.Reporter(context.Background(), &output, e, nil)
	stop()
	var sample struct {
		Admission, AdmissionBatch, PostgreSQL OperationSnapshot
		BatchAdmittedRecords                  uint64
	}
	if err := json.Unmarshal(output.Bytes(), &sample); err != nil {
		t.Fatal(err)
	}
	if sample.Admission.Count != 2 || sample.Admission.Failures != 1 || sample.Admission.MeanMilliseconds != 2 || sample.Admission.P50Milliseconds == 0 || sample.Admission.P95Milliseconds == 0 {
		t.Fatalf("admission distribution missing: %+v", sample.Admission)
	}
	if sample.AdmissionBatch.Count != 1 || sample.AdmissionBatch.MeanMilliseconds != 4 || sample.BatchAdmittedRecords != 64 {
		t.Fatal("batch admission metrics lost units or record count")
	}
	if sample.PostgreSQL.Count != 1 || sample.PostgreSQL.MeanMilliseconds != 9 {
		t.Fatalf("admission timing contaminated aggregate SQL: %+v", sample.PostgreSQL)
	}
}
