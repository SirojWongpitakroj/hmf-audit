// publication-benchmark is an opt-in cloud diagnostic. It never runs from an
// ingestion command and never writes to production canonical or native tables.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	contract "canonicalingestion"
	"github.com/jackc/pgx/v5"
)

type reporter struct {
	file     *os.File
	password string
}

func (r *reporter) emit(kind string, value any) error {
	b, err := json.Marshal(map[string]any{"type": kind, "time": time.Now().UTC(), "data": value})
	if err != nil {
		return err
	}
	if r.password != "" {
		quoted, _ := json.Marshal(r.password)
		b = []byte(strings.ReplaceAll(string(b), string(quoted[1:len(quoted)-1]), "[redacted]"))
	}
	if _, err = r.file.Write(append(b, '\n')); err != nil {
		return err
	}
	return r.file.Sync()
}

type trialResult struct {
	Mode                                                                                                                             string
	Trial                                                                                                                            int
	Warmup                                                                                                                           bool
	Records, Transactions                                                                                                            int
	Seconds, RecordsPerSecond, MillisecondsPerRecord                                                                                 float64
	InsertExecutions, CopyCalls, DataRows, MinRowsPerWrite, MaxRowsPerWrite                                                          int64
	MeanRowsPerWrite                                                                                                                 float64
	DataWriteMilliseconds, MeanDataWriteMilliseconds                                                                                 float64
	ValidationMilliseconds, BeginLockMilliseconds, BuildRowsMilliseconds, InsertMilliseconds, UpdateMilliseconds, CommitMilliseconds float64
	AppendP50Milliseconds, AppendP95Milliseconds, AppendP99Milliseconds                                                              float64
	CommitP50Milliseconds, CommitP95Milliseconds, CommitP99Milliseconds                                                              float64
	Resources                                                                                                                        resources
	GlobalWALDelta                                                                                                                   walSample
	CursorAndBytesVerified                                                                                                           bool
	Error                                                                                                                            string `json:"error,omitempty"`
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	i := max(0, min(len(sorted)-1, int(math.Ceil(float64(len(sorted))*p))-1))
	return sorted[i]
}
func milliseconds(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func runTrial(ctx context.Context, admin, observer *pgx.Conn, cfg *pgx.ConnConfig, schema string, mode variant, s sample, trial int, warmup bool, interval time.Duration) (r trialResult) {
	r.Mode = mode.Name
	r.Trial = trial
	r.Warmup = warmup
	r.Records = len(s.entries)
	if err := resetScratch(ctx, admin, schema); err != nil {
		r.Error = err.Error()
		return r
	}
	db, stats := benchmarkDB(cfg, schema, mode, "")
	defer db.Close()
	var pid uint32
	if err := db.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&pid); err != nil {
		r.Error = err.Error()
		return r
	}
	var synchronous string
	if err := db.QueryRowContext(ctx, "SHOW synchronous_commit").Scan(&synchronous); err != nil {
		r.Error = err.Error()
		return r
	}
	if synchronous != "on" {
		r.Error = "benchmark writer does not have synchronous_commit=on"
		return r
	}
	before := readWAL(ctx, admin)
	stopResources := startResources(ctx, observer, pid, interval)
	store := contract.Store{DB: db}
	var appendTimes, commitTimes []float64
	started := time.Now()
	for _, batch := range s.batches {
		begin := time.Now()
		result, err := store.AppendWithResult(ctx, "bench", batch, publicationLimits)
		appendTimes = append(appendTimes, milliseconds(time.Since(begin)))
		t := result.Timings
		r.ValidationMilliseconds += milliseconds(t.Validation)
		r.BeginLockMilliseconds += milliseconds(t.BeginLock)
		r.BuildRowsMilliseconds += milliseconds(t.BuildRows)
		r.InsertMilliseconds += milliseconds(t.Insert)
		r.UpdateMilliseconds += milliseconds(t.Update)
		r.CommitMilliseconds += milliseconds(t.Commit)
		commitTimes = append(commitTimes, milliseconds(t.Commit))
		if err != nil {
			r.Error = err.Error()
			break
		}
		r.Transactions++
		if result.Committed != batch[len(batch)-1].Ordinal || result.PrefixHash != s.batchPrefixes[r.Transactions-1] {
			r.Error = "acknowledged cursor/hash differs from batch boundary"
			break
		}
	}
	r.Seconds = time.Since(started).Seconds()
	r.Resources = stopResources()
	r.GlobalWALDelta = walDelta(before, readWAL(ctx, admin))
	observed := stats.snapshot()
	r.DataWriteMilliseconds = float64(observed.Nanos) / 1e6
	r.DataRows = observed.Rows
	r.MinRowsPerWrite = observed.MinRows
	r.MaxRowsPerWrite = observed.MaxRows
	if observed.Calls > 0 {
		r.MeanRowsPerWrite = float64(observed.Rows) / float64(observed.Calls)
		r.MeanDataWriteMilliseconds = r.DataWriteMilliseconds / float64(observed.Calls)
	}
	if mode.Copy {
		r.CopyCalls = observed.Calls
	} else {
		r.InsertExecutions = observed.Calls
	}
	if r.Error == "" {
		r.RecordsPerSecond = float64(r.Records) / r.Seconds
		r.MillisecondsPerRecord = r.Seconds * 1000 / float64(r.Records)
		if err := verifyRows(ctx, admin, schema, s.entries); err != nil {
			r.Error = err.Error()
		} else {
			r.CursorAndBytesVerified = true
		}
	}
	r.AppendP50Milliseconds = percentile(appendTimes, .50)
	r.AppendP95Milliseconds = percentile(appendTimes, .95)
	r.AppendP99Milliseconds = percentile(appendTimes, .99)
	r.CommitP50Milliseconds = percentile(commitTimes, .50)
	r.CommitP95Milliseconds = percentile(commitTimes, .95)
	r.CommitP99Milliseconds = percentile(commitTimes, .99)
	return r
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	f := flag.NewFlagSet("publication-benchmark", flag.ContinueOnError)
	env := f.String("env-file", "../MVA-PHV/.env.local", "canonical stream deployment profile (exported variables take precedence)")
	stream := f.String("stream", "canonical-v1", "existing published stream to read; never modified")
	records := f.Int("records", 10000, "distinct published observations to sample (1000..10000)")
	after := f.Int64("after", -1, "source ordinal before sample; -1 selects most recent committed sample")
	trials := f.Int("trials", 3, "measured repetitions per method")
	warmups := f.Int("warmups", 1, "unmeasured complete trials per method")
	selected := f.String("methods", "current,values-100,values-250,values-500,values-1000,copy", "comma-separated methods")
	out := f.String("out-dir", "", "output directory; default ../results/publication-cloud-TIMESTAMP")
	keep := f.Bool("keep-schema", false, "retain only generated benchmark schema after run for inspection")
	explain := f.Bool("explain", true, "capture EXPLAIN ANALYZE BUFFERS WAL of a scratch INSERT, then roll back")
	interval := f.Duration("sample-interval", 100*time.Millisecond, "memory and writer wait sampling interval")
	timeout := f.Duration("timeout", 30*time.Minute, "whole-run timeout including checks and cleanup")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() != 0 || *records < 1000 || *records > maxSampleRecords || *trials < 1 || *trials > 20 || *warmups < 0 || *warmups > 5 || *after < -1 || *interval < 20*time.Millisecond || *timeout <= 0 {
		return errors.New("invalid benchmark arguments; use --help (sample maximum 10000, transactions remain <=1000)")
	}
	var modes []variant
	seen := map[string]bool{}
	for _, name := range strings.Split(*selected, ",") {
		found := false
		for _, v := range variants {
			if name == v.Name && !seen[name] {
				modes = append(modes, v)
				seen[name] = true
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown or repeated method %q", name)
		}
	}
	if !seen["current"] {
		return errors.New("methods must include current for paired comparison")
	}
	if err := contract.LoadEnv(*env); err != nil {
		return err
	}
	dsn, err := contract.PostgresDSN("CANONICAL_STREAM", "BASELINES_POSTGRES")
	if err != nil {
		return err
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return errors.New("invalid canonical PostgreSQL connection configuration (DSN omitted)")
	}
	cfg = cfg.Copy()
	cfg.RuntimeParams["application_name"] = "mva_publication_benchmark"
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, *timeout)
	defer stop()
	if *out == "" {
		*out = filepath.Join("..", "results", "publication-cloud-"+time.Now().UTC().Format("20060102T150405.000000000Z"))
	}
	if err = os.MkdirAll(*out, 0700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(*out, "report.jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	r := &reporter{file: file, password: cfg.Password}
	// All database errors pass through the redacting reporter. Console failures
	// below use generic text and never echo connection strings or record payloads.
	reportFailure := func(stage string, err error) error {
		_ = r.emit("error", map[string]any{"stage": stage, "error": err.Error()})
		return fmt.Errorf("%s failed; see %s", stage, filepath.Join(*out, "report.jsonl"))
	}
	admin, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return reportFailure("database connection", err)
	}
	defer admin.Close(context.Background())
	settings, err := validateDurability(ctx, admin)
	if err != nil {
		return reportFailure("durability validation", err)
	}
	readCfg := cfg.Copy()
	readCfg.RuntimeParams["default_transaction_read_only"] = "on"
	readCfg.RuntimeParams["application_name"] = "mva_publication_benchmark_read"
	source, err := pgx.ConnectConfig(ctx, readCfg)
	if err != nil {
		return reportFailure("read-only source connection", err)
	}
	defer source.Close(context.Background())
	observer, err := pgx.ConnectConfig(ctx, readCfg)
	if err != nil {
		return reportFailure("read-only observer connection", err)
	}
	defer observer.Close(context.Background())
	s, err := loadSample(ctx, source, *stream, *after, *records)
	if err != nil {
		return reportFailure("load bounded source sample", err)
	}
	tableStats, statsErr := sourceTableStats(ctx, source)
	var random [8]byte
	if _, err = rand.Read(random[:]); err != nil {
		return err
	}
	schema := "mva_pub_bench_" + hex.EncodeToString(random[:])
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		return reportFailure("create isolated benchmark schema (database CREATE privilege required)", err)
	}
	defer func() {
		if *keep {
			fmt.Printf("Retained benchmark schema: %s\n", schema)
			return
		}
		cleanupCtx, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		// schema is internally generated, never accepted from flags or files.
		_, e := admin.Exec(cleanupCtx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		if e != nil {
			_ = r.emit("cleanup_error", map[string]any{"schema": schema, "error": e.Error()})
			fmt.Fprintf(os.Stderr, "Benchmark schema cleanup failed: %s; see report\n", schema)
		} else {
			_ = r.emit("cleanup", map[string]any{"schema": schema, "dropped": true})
		}
	}()
	if err = cloneTables(ctx, admin, schema); err != nil {
		return reportFailure("clone canonical table definitions", err)
	}
	header := map[string]any{"schema": schema, "source": s.info, "settings": settings, "methods": modes, "go_version": runtime.Version(), "goos": runtime.GOOS, "gomaxprocs": runtime.GOMAXPROCS(0), "transaction_max_records": publicationLimits.MaxRecords, "transaction_max_bytes": publicationLimits.MaxBatchBytes, "transactions_per_trial": len(s.batches), "trials": *trials, "warmups": *warmups, "source_table_stats": tableStats, "source_writes": false, "native_ingestion": false, "infrastructure_caches_flushed": false, "limitations": []string{"Scratch tables start empty: production index cardinality, bloat and concurrent contention are not reproduced.", "WAL statistics are global, delayed and may include unrelated ingestion; they are not attributable WAL/record.", "Values variants rebuild bounded INSERT SQL inside the benchmark adapter; current uses the exact existing driver path.", "COPY is benchmark-only on the active writer transaction; no production COPY option is enabled.", "Memory includes the <=64 MiB immutable input sample. RSS/heap peaks are sampled, not exact high-water marks.", "Failure checks cover publication transactions/connection loss, not MVA native checkpoint crashes."}}
	if statsErr != nil {
		header["source_table_stats_error"] = statsErr.Error()
	}
	if err = r.emit("configuration", header); err != nil {
		return err
	}
	fmt.Printf("Read %d existing observations (%d..%d). Production tables will not be modified.\n", s.info.Records, s.info.OriginalFirst, s.info.OriginalLast)
	fmt.Printf("Scratch schema %s; each trial uses %d ordered transactions of at most 1000 records.\n", schema, len(s.batches))
	fmt.Printf("Methods: %s. Results: %s\n", *selected, *out)
	eligible := map[string]bool{}
	failed := false
	for _, mode := range modes {
		checks := correctness(ctx, admin, cfg, schema, mode, s.entries)
		pass := true
		for _, c := range checks {
			pass = pass && c.Pass
		}
		if err = r.emit("correctness", map[string]any{"mode": mode.Name, "pass": pass, "checks": checks}); err != nil {
			return err
		}
		eligible[mode.Name] = pass
		failed = failed || !pass
		fmt.Printf("Checks %-12s %s\n", mode.Name, map[bool]string{true: "PASS", false: "FAIL (timings skipped)"}[pass])
	}
	var results []trialResult
	for warm := 0; warm < *warmups; warm++ {
		for _, mode := range modes {
			if !eligible[mode.Name] {
				continue
			}
			result := runTrial(ctx, admin, observer, cfg, schema, mode, s, warm+1, true, *interval)
			if err = r.emit("warmup", result); err != nil {
				return err
			}
			if result.Error != "" {
				failed = true
				eligible[mode.Name] = false
				fmt.Printf("Warmup %-12s FAILED; see report\n", mode.Name)
			} else {
				fmt.Printf("Warmup %-12s %.3fs\n", mode.Name, result.Seconds)
			}
		}
	}
	fmt.Println("Method       Trial   seconds     records/s   INSERTs   COPYs   rows/write   verified")
	for trial := 0; trial < *trials; trial++ {
		// Rotate order across paired rounds; do not systematically give COPY the
		// warmest infrastructure or current the first-run penalty.
		for offset := 0; offset < len(modes); offset++ {
			mode := modes[(offset+trial)%len(modes)]
			if !eligible[mode.Name] {
				continue
			}
			result := runTrial(ctx, admin, observer, cfg, schema, mode, s, trial+1, false, *interval)
			results = append(results, result)
			if err = r.emit("trial", result); err != nil {
				return err
			}
			if result.Error != "" {
				failed = true
				fmt.Printf("%-12s %3d FAILED; see report\n", mode.Name, trial+1)
			} else {
				fmt.Printf("%-12s %3d   %8.3f   %11.1f   %7d   %5d   %10.1f   %t\n", mode.Name, trial+1, result.Seconds, result.RecordsPerSecond, result.InsertExecutions, result.CopyCalls, result.MeanRowsPerWrite, result.CursorAndBytesVerified)
			}
		}
	}
	if *explain && ctx.Err() == nil {
		var rows [][]any
		var prefix [32]byte
		for _, e := range s.batches[0] {
			ph := sha256.Sum256(e.Record)
			prefix = prefixHash(prefix, e)
			rows = append(rows, []any{"bench", int64(e.Ordinal), e.SourceID, []byte(e.Record), bytes32(ph), bytes32(prefix)})
		}
		plan, explainErr := explainInsert(ctx, admin, schema, rows)
		if explainErr != nil {
			_ = r.emit("explain_unavailable", map[string]string{"error": explainErr.Error()})
		} else {
			if err = r.emit("explain_insert", plan); err != nil {
				return err
			}
		}
	}
	if err = writeCSV(filepath.Join(*out, "trials.csv"), results); err != nil {
		return err
	}
	summary := summarize(results, modes)
	if err = r.emit("summary", map[string]any{"methods": summary, "all_checks_and_trials_pass": !failed, "production_modified": false, "production_optimization_enabled": false}); err != nil {
		return err
	}
	for _, line := range summary {
		fmt.Println(line)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("benchmark interrupted; partial results saved in %s", *out)
	}
	if failed {
		return fmt.Errorf("some benchmark checks/trials failed; review %s", *out)
	}
	fmt.Println("Complete. Production ingestion is unchanged; results do not automatically enable COPY.")
	return nil
}

func bytes32(v [32]byte) []byte { return append([]byte(nil), v[:]...) }
func writeCSV(path string, results []trialResult) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write([]string{"method", "trial", "records", "transactions", "seconds", "records_per_second", "insert_executions", "copy_calls", "mean_rows_per_write", "insert_ms", "commit_ms", "append_p95_ms", "sampled_peak_rss_bytes", "sampled_peak_heap_bytes", "allocated_bytes", "gc_cycles", "gc_pause_ms", "verified", "error"}); err != nil {
		return err
	}
	for _, r := range results {
		row := []string{r.Mode, fmt.Sprint(r.Trial), fmt.Sprint(r.Records), fmt.Sprint(r.Transactions), fmt.Sprint(r.Seconds), fmt.Sprint(r.RecordsPerSecond), fmt.Sprint(r.InsertExecutions), fmt.Sprint(r.CopyCalls), fmt.Sprint(r.MeanRowsPerWrite), fmt.Sprint(r.InsertMilliseconds), fmt.Sprint(r.CommitMilliseconds), fmt.Sprint(r.AppendP95Milliseconds), fmt.Sprint(r.Resources.SampledPeakRSSBytes), fmt.Sprint(r.Resources.SampledPeakHeapBytes), fmt.Sprint(r.Resources.TotalAllocatedBytes), fmt.Sprint(r.Resources.GCCycles), fmt.Sprint(r.Resources.GCPauseMilliseconds), fmt.Sprint(r.CursorAndBytesVerified), r.Error}
		if err = w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
func summarize(results []trialResult, modes []variant) []string {
	var out []string
	for _, mode := range modes {
		var times, rates []float64
		for _, r := range results {
			if r.Mode == mode.Name && r.Error == "" {
				times = append(times, r.Seconds)
				rates = append(rates, r.RecordsPerSecond)
			}
		}
		if len(times) > 0 {
			out = append(out, fmt.Sprintf("%-12s median %.3fs, %.1f records/s (%d verified trials)", mode.Name, percentile(times, .50), percentile(rates, .50), len(times)))
		}
	}
	return out
}
