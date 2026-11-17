package testfixture

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"
	"time"

	contract "canonicalingestion"
)

type LocalAuditResult struct {
	Scheme, Mode                      string
	Stored, Selected                  int
	Warmups, Runs                     int
	LatencyMS                         contract.AuditSummary
	PeakRSS, PeakHeap, AllocatedBytes uint64
	Metrics                           contract.AuditMetrics
	Samples                           []contract.AuditMetrics
	GCCycles, GCPauseNanos            uint64
}

// MeasureAudit uses only a small already-built fixture. It is not Experiment
// 1B and never creates/ingests additional observations or contacts remote hosts.
func MeasureAudit(t *testing.T, scheme, mode string, stored, selected int, run func() (contract.AuditMetrics, error)) LocalAuditResult {
	t.Helper()
	if stored > 10000 || selected > 10000 {
		t.Fatal("local audit cap exceeded")
	}
	runtime.GC()
	debug.FreeOSMemory()
	if _, err := run(); err != nil {
		t.Fatal(err)
	}
	r := LocalAuditResult{Scheme: scheme, Mode: mode, Stored: stored, Selected: selected, Warmups: 1, Runs: 5}
	var times []float64
	for range r.Runs {
		before := contract.ReadMemory()
		monitor := contract.StartMemoryMonitor(5 * time.Millisecond)
		m, err := run()
		peak := monitor.Stop()
		after := contract.ReadMemory()
		if err != nil {
			t.Fatal(err)
		}
		times = append(times, float64(m.EndToEnd)/1e6)
		r.Metrics = m
		r.Samples = append(r.Samples, m)
		r.GCCycles += after.GCCount - before.GCCount
		r.GCPauseNanos += after.GCPauseNanos - before.GCPauseNanos
		r.PeakRSS = max(r.PeakRSS, peak, before.RSSBytes, after.RSSBytes)
		r.PeakHeap = max(r.PeakHeap, monitor.PeakHeap(), after.HeapAllocBytes)
		r.AllocatedBytes += after.TotalAllocBytes - before.TotalAllocBytes
	}
	r.LatencyMS = contract.SummarizeAudit(times)
	r.AllocatedBytes /= uint64(r.Runs)
	b, _ := json.MarshalIndent(r, "", "  ")
	if dir := os.Getenv("FAIR_AUDIT_RESULTS"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, scheme+"-"+mode+".json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%s %s n=%d/%d mean_ms=%.3f p95_ms=%.3f reads=%d tx_queries=%d allocated=%d evidence=%d RSS=%d", scheme, mode, selected, stored, r.LatencyMS.Mean, r.LatencyMS.P95, r.Metrics.StorageReads, r.Metrics.TransactionQueries, r.AllocatedBytes, r.Metrics.EvidenceBytes, r.PeakRSS)
	return r
}
