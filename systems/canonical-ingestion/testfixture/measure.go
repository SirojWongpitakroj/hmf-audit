package testfixture

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
	"time"

	contract "canonicalingestion"
)

func ScaleRecords(t testing.TB) int {
	t.Helper()
	raw := os.Getenv("CONCURRENT_SCALE_N")
	if raw == "" {
		t.Skip("CONCURRENT_SCALE_N unset; explicit local performance/scale run")
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 10000 {
		t.Fatal("local scale fixtures are capped at 10,000 observations")
	}
	return n
}
func Execution(t testing.TB) contract.Execution {
	t.Helper()
	e := contract.DefaultExecution()
	if os.Getenv("STORAGE_ACCESS_OPTIMIZATIONS") == "false" {
		e.StorageAccessOptimizations = false
	}
	for name, dst := range map[string]*int{"CONCURRENT_CPU_WORKERS": &e.CPUWorkers, "CONCURRENT_IO_WORKERS": &e.IOWorkers, "CONCURRENT_BATCH_RECORDS": &e.BatchMaxRecords} {
		if raw := os.Getenv(name); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil {
				t.Fatal(err)
			}
			*dst = n
		}
	}
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
	return e
}
func Modes() []string {
	switch os.Getenv("CONCURRENT_SCALE_MODE") {
	case "old":
		return []string{"old"}
	case "new":
		return []string{"new"}
	default:
		return []string{"old", "new"}
	}
}

type Measurement struct {
	Allocations, AllocatedBytes, GCCycles, GCPauseNanos, PeakHeap uint64
	AllocationsPerRecord, AllocatedBytesPerRecord                 float64
	Scheme, Mode, Scope                                           string
	Records                                                       int
	Seconds, RecordsPerSecond, CPUPercentOneCore                  float64
	CPUAvailable                                                  bool
	PeakRSS                                                       uint64
	Memory                                                        contract.MemorySnapshot
	Execution                                                     contract.Execution
	Telemetry                                                     json.RawMessage
}

func Measure(t testing.TB, scheme, mode string, n int, e contract.Execution, m *contract.IngestMetrics, db *sql.DB, run func()) {
	t.Helper()
	runtime.GC()
	debug.FreeOSMemory()
	monitor := contract.StartMemoryMonitor(5 * time.Millisecond)
	cpu, cpuOK := contract.ReadProcessCPUSeconds()
	started := time.Now()
	var telemetry measureWriter
	e.MetricsInterval = 0
	stopMetrics := m.Reporter(context.Background(), &telemetry, e, map[string]*sql.DB{"native": db})
	var memoryBefore, memoryAfter runtime.MemStats
	runtime.ReadMemStats(&memoryBefore)
	run()
	runtime.ReadMemStats(&memoryAfter)
	elapsed := time.Since(started)
	after, afterOK := contract.ReadProcessCPUSeconds()
	stopMetrics()
	rss := monitor.Stop()
	result := Measurement{Scheme: scheme, Mode: mode, Scope: "native adapter ingestion including authentication; excludes canonical publication and database setup", Records: n, Seconds: elapsed.Seconds(), RecordsPerSecond: float64(n) / elapsed.Seconds(), CPUPercentOneCore: (after - cpu) / elapsed.Seconds() * 100, CPUAvailable: cpuOK && afterOK, PeakRSS: rss, Memory: contract.ReadMemory(), Execution: e, Telemetry: json.RawMessage(telemetry), Allocations: memoryAfter.Mallocs - memoryBefore.Mallocs, AllocatedBytes: memoryAfter.TotalAlloc - memoryBefore.TotalAlloc, GCCycles: uint64(memoryAfter.NumGC - memoryBefore.NumGC), GCPauseNanos: memoryAfter.PauseTotalNs - memoryBefore.PauseTotalNs, PeakHeap: monitor.PeakHeap()}
	result.AllocationsPerRecord = float64(result.Allocations) / float64(n)
	result.AllocatedBytesPerRecord = float64(result.AllocatedBytes) / float64(n)
	b, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("INGESTION_MEASUREMENT " + string(b))
	if path := os.Getenv("CONCURRENT_MEASUREMENT_FILE"); path != "" {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = fmt.Fprintln(f, string(b))
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
}

type measureWriter []byte

func (w *measureWriter) Write(b []byte) (int, error) { *w = append(*w, b...); return len(b), nil }
