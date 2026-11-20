package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"blas/alis"
	"blas/query"
	"blas/verify"
	"github.com/jackc/pgx/v5"
)

func TestCloudDemoBLASVerification(t *testing.T) {
	if os.Getenv("DEMO_CLOUD_BLAS") != "1" {
		t.Skip("run from the cloud dashboard")
	}
	emit := func(kind string, fields map[string]any) {
		fields["type"], fields["scheme"] = kind, "BLAS"
		payload, _ := json.Marshal(fields)
		fmt.Printf("DEMO_EVENT %s\n", payload)
	}
	if os.Getenv("DEMO_BLAS_SHAPE") == "" {
		runCloudBLASAttributes(t, emit)
		return
	}
	q, qErr := strconv.Atoi(os.Getenv("DEMO_BLAS_Q"))
	set, setErr := strconv.Atoi(os.Getenv("DEMO_BLAS_SET"))
	repeats, repeatsErr := strconv.Atoi(os.Getenv("DEMO_QUERY_REPEATS"))
	if qErr != nil || setErr != nil || repeatsErr != nil || repeats < 1 || repeats > 20 {
		t.Fatal("invalid BLAS case or repeat count")
	}
	emit("scheme", map[string]any{"message": "Rebuilding the BLAS reference chain from the pinned Spark source"})
	ctx := context.Background()
	state, err := loadFairBLAS(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for _, extra := range state.workers[1:] {
			_ = extra.Close(ctx)
		}
		_ = state.database.Close(ctx)
	}()
	scenarios, err := fairBLASNativeScenarios(fairNativeQueriesDir(), state.vocabulary)
	if err != nil {
		t.Fatal(err)
	}
	shape := os.Getenv("DEMO_BLAS_SHAPE")
	index := slices.IndexFunc(scenarios, func(s fairBLASScenario) bool {
		return s.placement == shape && s.batchSize == q && s.set == set
	})
	if index < 0 {
		t.Fatal("unknown native BLAS case")
	}
	scenario := scenarios[index]
	options := verify.Options{UseSegmentScan: true, UseAggregation: true}
	timings := make([]float64, 0, repeats)
	for run := 0; run <= repeats; run++ {
		message := "Verifying the native BLAS query (warm-up)"
		if run > 0 {
			message = fmt.Sprintf("Timed run %d/%d", run, repeats)
		}
		emit("trial", map[string]any{"current": run, "total": repeats, "message": message})
		start := time.Now()
		result, err := executeFairBLASRequest(ctx, state.workers, state.fullNode, state.registry, state.auditor, scenario, options)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		validateFairBLASRequestResult(t, scenario, result)
		if run == repeats && os.Getenv("DEMO_BATCH_OUT") != "" {
			ordinals := make([]int, 0, q)
			for _, file := range result.response.Files {
				for _, proof := range file.VO.ResultProofs {
					entry, err := alis.DecodeLogEntry(proof.Object)
					if err != nil {
						t.Fatal(err)
					}
					ordinals = append(ordinals, int(file.Ref.FID*128+entry.ESN))
				}
			}
			slices.Sort(ordinals)
			writeCloudBLASBatch(t, ordinals)
		}
		if run > 0 {
			timings = append(timings, float64(elapsed)/float64(time.Millisecond))
		}
	}
	slices.Sort(timings)
	median := (timings[(repeats-1)/2] + timings[repeats/2]) / 2
	emit("result", map[string]any{"status": "VALID", "milliseconds": median,
		"message": fmt.Sprintf("Verified %d records for BLAS %s query set %d", q, shape, set)})
}

// The ingest-only run indexed legacy workload keywords, while Experiment 1
// indexes the native vocabulary. Authenticate the first file against each
// independent reconstruction to select the matching reference layout.
func cloudBLASBatchKeywords(ctx context.Context, database *pgx.Conn, cfg Config, vocabulary []string, total int) (map[int][]string, error) {
	file, err := os.Open(os.Getenv("FAIR_DATASET_PATH"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	native, err := alis.NewEntryFileBuilder(alis.NewLogGenerator(0), 0, cfg.Fanout)
	if err != nil {
		return nil, err
	}
	legacy, err := alis.NewEntryFileBuilder(alis.NewLogGenerator(0), 0, cfg.Fanout)
	if err != nil {
		return nil, err
	}
	synthetic := fairBLASBatchKeywordsForCounts(total, []int{1, 16, 64, 256, 1024, 4096, 16384})
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for ordinal := 0; ordinal < cfg.EntriesPerFile && scanner.Scan(); ordinal++ {
		var record fairBLASDatasetRecord
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, err
		}
		timestamp := fairBLASTimestamp(record.RawLog, ordinal)
		keywords := fairBLASKeywords(vocabulary, record.RawLog)
		if _, err := native.AddEntry(timestamp, keywords, []byte(record.RawLog)); err != nil {
			return nil, err
		}
		if _, err := legacy.AddEntry(timestamp, append(keywords, synthetic[ordinal]...), []byte(record.RawLog)); err != nil {
			return nil, err
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	answers, err := query.NewPostgresSP(ctx, database).AnswerQOff([]query.FileRef{{LID: 0, FID: 0}}, query.QOff{})
	if err != nil {
		return nil, err
	}
	for _, variant := range []struct {
		builder *alis.EntryFileBuilder
		legacy  bool
	}{{native, false}, {legacy, true}} {
		_, meta, err := variant.builder.Close()
		if err != nil {
			return nil, err
		}
		if alis.VerifyEntryQuery(meta.RHIIMT, meta.RHOMT, alis.EntryQuery{}, answers[0].VO) {
			if variant.legacy {
				return synthetic, nil
			}
			return nil, nil
		}
	}
	return nil, fmt.Errorf("BLAS first file does not match either source-derived layout")
}

// Run the source attribute selection against the stored BLAS files. Attributes
// are in the pinned Spark source; the benchmark BLAS index has only keywords
// and its own timestamps, so authenticate the covering files at Stage 3.
func runCloudBLASAttributes(t *testing.T, emit func(string, map[string]any)) {
	start, startErr := time.Parse(time.RFC3339Nano, os.Getenv("DEMO_QUERY_START"))
	end, endErr := time.Parse(time.RFC3339Nano, os.Getenv("DEMO_QUERY_END"))
	repeats, repeatErr := strconv.Atoi(os.Getenv("DEMO_QUERY_REPEATS"))
	if startErr != nil || endErr != nil || !start.Before(end) || repeatErr != nil || repeats < 1 || repeats > 20 {
		t.Fatal("invalid time range or repeat count")
	}
	emit("scheme", map[string]any{"message": "Selecting matching records from the pinned Spark source"})
	ordinals := cloudBLASSelection(t, start, end)
	const entriesPerFile = 128
	refs := make([]query.FileRef, 0)
	for _, ordinal := range ordinals {
		ref := query.FileRef{FID: uint64(ordinal / entriesPerFile)}
		if len(refs) == 0 || refs[len(refs)-1] != ref {
			refs = append(refs, ref)
		}
	}
	count := len(ordinals)
	if count == 0 {
		emit("result", map[string]any{"status": "VALID", "message": "No matching records in the pinned Spark source"})
		return
	}
	emit("phase", map[string]any{"message": fmt.Sprintf("Rebuilding BLAS roots for %d matching records in %d files", count, len(refs))})
	ctx := context.Background()
	state, err := loadFairBLAS(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	defer state.database.Close(ctx)
	provider := query.NewPostgresSP(ctx, state.database)
	timings := make([]float64, 0, repeats)
	for run := 0; run <= repeats; run++ {
		message := "Verifying the covering BLAS files (warm-up)"
		if run > 0 {
			message = fmt.Sprintf("Timed run %d/%d", run, repeats)
		}
		emit("trial", map[string]any{"current": run, "total": repeats, "message": message})
		start := time.Now()
		for batch := range slices.Chunk(refs, 64) {
			results, err := provider.AnswerQOff(batch, query.QOff{})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != len(batch) {
				t.Fatal("BLAS proof response is incomplete")
			}
			for index, result := range results {
				root, found := state.fileRoots[batch[index]]
				if !found || result.Ref != batch[index] || !alis.VerifyEntryQuery(root.RHIIMT, root.RHOMT, alis.EntryQuery{}, result.VO) {
					t.Fatalf("BLAS entry proof rejected for file %d", batch[index].FID)
				}
			}
		}
		if run > 0 {
			timings = append(timings, float64(time.Since(start))/float64(time.Millisecond))
		}
	}
	slices.Sort(timings)
	median := (timings[(repeats-1)/2] + timings[repeats/2]) / 2
	emit("result", map[string]any{"status": "VALID", "milliseconds": median,
		"message": fmt.Sprintf("Verified %d BLAS file proofs covering %d matching records", len(refs), count)})
}

func cloudBLASSelection(t *testing.T, start, end time.Time) []int {
	t.Helper()
	if path := os.Getenv("DEMO_BATCH_IN"); path != "" {
		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var ordinals []int
		if err := json.Unmarshal(payload, &ordinals); err != nil {
			t.Fatal(err)
		}
		for i, ordinal := range ordinals {
			if ordinal < 0 || ordinal >= 10_000_000 || (i > 0 && ordinal <= ordinals[i-1]) {
				t.Fatal("invalid shared cloud batch")
			}
		}
		return ordinals
	}
	file, err := os.Open(os.Getenv("FAIR_DATASET_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	tenant, service := os.Getenv("DEMO_QUERY_TENANT"), os.Getenv("DEMO_QUERY_SERVICE")
	logType, region := os.Getenv("DEMO_QUERY_TYPE"), os.Getenv("DEMO_QUERY_REGION")
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	ordinals := make([]int, 0)
	for ordinal := 0; scanner.Scan(); ordinal++ {
		var record struct {
			Tenant    string `json:"tenant_id"`
			Service   string `json:"service_id"`
			LogType   string `json:"log_type"`
			Region    string `json:"region_id"`
			EventTime string `json:"event_time"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			t.Fatalf("decode source record %d: %v", ordinal, err)
		}
		if record.Service != service || record.LogType != logType || record.Region != region ||
			(tenant != "*" && record.Tenant != tenant) {
			continue
		}
		when, err := time.Parse(time.RFC3339Nano, record.EventTime)
		if err != nil {
			t.Fatal(err)
		}
		if !when.Before(start) && when.Before(end) {
			ordinals = append(ordinals, ordinal)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	writeCloudBLASBatch(t, ordinals)
	return ordinals
}

func writeCloudBLASBatch(t *testing.T, ordinals []int) {
	t.Helper()
	if path := os.Getenv("DEMO_BATCH_OUT"); path != "" {
		payload, err := json.Marshal(ordinals)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
