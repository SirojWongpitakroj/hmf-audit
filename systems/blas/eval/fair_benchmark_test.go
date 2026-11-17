package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"math/bits"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"

	"blas/chain"
	"blas/iomf"
	"blas/query"
	"blas/verify"
	"github.com/jackc/pgx/v5"
)

var fairBLASResult verify.Result

var fairBLASBatchSizes = []int{1, 16, 64, 256, 1024, 4096, 16384}
var fairBLASPlacements = []string{"clustered-2", "scattered"}

type fairBLASDatasetRecord struct {
	SourceID string `json:"source_id"`
	RawLog   string `json:"raw_log"`
	RegionID string `json:"region_id"`
	TenantID string `json:"tenant_id"`
}

// legacyFairBLASAuditorVerificationCore is retained only while older result
// fixtures remain readable. The reportable benchmark is the persistence-aware
// full-request benchmark in fair_persistence_request_benchmark_test.go.
func legacyFairBLASAuditorVerificationCore(b *testing.B) {
	b.StopTimer()
	if os.Getenv("FAIR_PERSISTENCE") != "1" {
		b.Fatal("FAIR_PERSISTENCE=1 is required; use the shared runner")
	}
	ctx := context.Background()
	database, err := pgx.Connect(ctx, os.Getenv("FAIR_POSTGRES_DSN"))
	if err != nil {
		b.Fatalf("connect PostgreSQL: %v", err)
	}
	b.Cleanup(func() { _ = database.Close(context.Background()) })
	if _, err := database.Exec(ctx, "TRUNCATE TABLE blas.verification_evidence"); err != nil {
		b.Fatalf("reset BLAS evidence: %v", err)
	}
	cfg := Config{
		NumGenerators: 1, EntriesPerFile: 128, FilesPerBlock: 8,
		Fanout: 4, SegmentSize: 8, Seed: 1, UseAggregation: true,
	}
	records := loadFairBLASRecords(b)
	perGenerator := [][]Record{records}
	c, sp, registry, err := BuildChain(cfg, perGenerator)
	if err != nil {
		b.Fatal(err)
	}
	fullNode := query.NewFullNode(c)
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for index, block := range c.Blocks() {
		headers[index] = block.Header
	}
	auditor := verify.NewAuditor(headers, cfg.SegmentSize)
	if _, ok := registry[0]; !ok {
		b.Fatal("shared dataset did not produce BLAS log generator 0")
	}
	options := verify.Options{UseSegmentScan: true, UseAggregation: true}

	for _, placement := range fairBLASPlacements {
		for _, batchSize := range fairBLASBatchSizes {
			expression := iomf.Keyword(fairBLASBatchKeyword(placement, batchSize))
			request := query.Q{LG: 0, FK: expression, EK: expression}
			response, ok := auditor.PrepareResponse(fullNode, sp, registry, request, options)
			if !ok {
				b.Fatalf("prepare BLAS response for %s q=%d", placement, batchSize)
			}
			preparedResult := auditor.VerifyResponse(registry, request, response, options)
			if !preparedResult.Accept || len(preparedResult.Entries) != batchSize {
				b.Fatalf("BLAS %s q=%d: accepted=%v entries=%d",
					placement, batchSize, preparedResult.Accept, len(preparedResult.Entries))
			}
			scenario := fmt.Sprintf("%s/q=%d", placement, batchSize)
			encoded, err := encodeFairBLASResponse(response)
			if err != nil {
				b.Fatalf("encode BLAS evidence: %v", err)
			}
			if _, err := database.Exec(ctx, `INSERT INTO blas.verification_evidence (scenario, payload)
				VALUES ($1, $2) ON CONFLICT (scenario) DO UPDATE SET payload = EXCLUDED.payload`, scenario, encoded); err != nil {
				b.Fatalf("persist BLAS evidence: %v", err)
			}

			b.Run(fmt.Sprintf("%s/q=%d", placement, batchSize), func(b *testing.B) {
				b.StopTimer()
				warmup := fairBLASWarmupCount(b)
				for range warmup {
					persisted, err := loadFairBLASResponse(ctx, database, scenario)
					if err != nil {
						b.Fatal(err)
					}
					result := auditor.VerifyResponse(registry, request, persisted, options)
					if !result.Accept || len(result.Entries) != batchSize {
						b.Fatalf("BLAS warm-up failed: accepted=%v entries=%d",
							result.Accept, len(result.Entries))
					}
					fairBLASResult = result
				}
				b.ResetTimer()
				b.ReportMetric(float64(batchSize), "records/op")
				b.ReportMetric(float64(len(records)), "dataset-records")
				b.ReportMetric(float64(len(c.Blocks())), "blocks")
				b.ReportMetric(float64(len(response.Files)), "response-files")
				b.ReportMetric(float64(VOSize(
					response.Stage1, response.Stage2, response.Files, response.Bindings,
				)), "evidence-bytes/op")
				b.ReportMetric(float64(len(encoded)), "persisted-bytes/op")
				b.ReportMetric(1, "postgres-reads/op")
				b.ReportMetric(float64(warmup), "warmup-ops")
				b.StartTimer()
				for range b.N {
					persisted, err := loadFairBLASResponse(ctx, database, scenario)
					if err != nil {
						b.Fatal(err)
					}
					result := auditor.VerifyResponse(registry, request, persisted, options)
					if !result.Accept || len(result.Entries) != batchSize {
						b.Fatalf("BLAS response failed: accepted=%v entries=%d",
							result.Accept, len(result.Entries))
					}
					fairBLASResult = result
				}
			})
		}
	}
}

func encodeFairBLASResponse(response verify.Response) ([]byte, error) {
	var output bytes.Buffer
	err := gob.NewEncoder(&output).Encode(response)
	return output.Bytes(), err
}

func loadFairBLASResponse(ctx context.Context, database *pgx.Conn,
	scenario string) (verify.Response, error) {
	var payload []byte
	if err := database.QueryRow(ctx,
		"SELECT payload FROM blas.verification_evidence WHERE scenario = $1", scenario).Scan(&payload); err != nil {
		return verify.Response{}, err
	}
	var response verify.Response
	if err := gob.NewDecoder(bytes.NewReader(payload)).Decode(&response); err != nil {
		return verify.Response{}, err
	}
	return response, nil
}

func fairBLASWarmupCount(b testing.TB) int {
	b.Helper()
	value := os.Getenv("FAIR_WARMUP")
	if value == "" {
		return 0
	}
	count, err := strconv.Atoi(value)
	if err != nil || count < 0 {
		b.Fatalf("invalid FAIR_WARMUP %q", value)
	}
	return count
}

// fairBLASVocabulary scans the shared region-grouped dataset once for its
// record count and its 16 most common tokens, which become the queryable
// keywords. It keeps counters rather than the records themselves, so the
// streaming build can make a second pass over the file.
func fairBLASVocabulary() ([]string, int, error) {
	path := os.Getenv("FAIR_DATASET_PATH")
	if path == "" {
		return nil, 0, fmt.Errorf("FAIR_DATASET_PATH is unset; run the shared benchmark runner")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	total, region := 0, -1
	documentFrequency := make(map[string]int)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		var input fairBLASDatasetRecord
		if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
			return nil, 0, fmt.Errorf("decode shared dataset record %d: %w", total, err)
		}
		if err := validateFairBLASRecord(input, total, &region); err != nil {
			return nil, 0, err
		}
		for token := range fairBLASTokens(input.RawLog) {
			documentFrequency[token]++
		}
		total++
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, fmt.Errorf("shared dataset is empty")
	}
	vocabulary := make([]string, 0, len(documentFrequency))
	for token := range documentFrequency {
		vocabulary = append(vocabulary, token)
	}
	sort.Slice(vocabulary, func(i, j int) bool {
		if documentFrequency[vocabulary[i]] != documentFrequency[vocabulary[j]] {
			return documentFrequency[vocabulary[i]] > documentFrequency[vocabulary[j]]
		}
		return vocabulary[i] < vocabulary[j]
	})
	if len(vocabulary) > 16 {
		vocabulary = vocabulary[:16]
	}
	return vocabulary, total, nil
}

// validateFairBLASRecord checks a dataset record and that regions stay
// grouped R0, R1, ... in order; region carries the current region across
// calls.
func validateFairBLASRecord(input fairBLASDatasetRecord, ordinal int, region *int) error {
	if input.SourceID == "" || input.RawLog == "" || input.TenantID == "" {
		return fmt.Errorf("invalid shared dataset record %d", ordinal)
	}
	if input.RegionID != fmt.Sprintf("R%d", *region) {
		if input.RegionID != fmt.Sprintf("R%d", *region+1) {
			return fmt.Errorf("shared dataset record %d has region %q; records must be grouped R0, R1, ... in order",
				ordinal, input.RegionID)
		}
		*region++
	}
	return nil
}

// fairBLASKeywords are the vocabulary tokens a raw log contains, or "other"
// when it contains none.
func fairBLASKeywords(vocabulary []string, raw string) []string {
	tokens := fairBLASTokens(raw)
	keywords := make([]string, 0, len(vocabulary)+2)
	for _, token := range vocabulary {
		if tokens[token] {
			keywords = append(keywords, token)
		}
	}
	if len(keywords) == 0 {
		keywords = append(keywords, "other")
	}
	return keywords
}

// fairBLASBatchKeywords maps a record ordinal to the batch keywords it
// carries, giving every scenario's query exactly its batch size of hits.
func fairBLASBatchKeywords(total int) map[int][]string {
	assigned := make(map[int][]string)
	segmentLeaves := fairBLASSegmentLeavesFromEnvironment()
	for _, placement := range fairBLASPlacements {
		for _, batchSize := range fairBLASBatchSizes {
			keyword := fairBLASBatchKeyword(placement, batchSize)
			for _, index := range fairBLASBatchIndices(placement, batchSize, total, segmentLeaves) {
				assigned[index] = append(assigned[index], keyword)
			}
		}
	}
	return assigned
}

func loadFairBLASRecords(b *testing.B) []Record {
	b.Helper()
	vocabulary, total, err := fairBLASVocabulary()
	if err != nil {
		b.Fatal(err)
	}
	file, err := os.Open(os.Getenv("FAIR_DATASET_PATH"))
	if err != nil {
		b.Fatal(err)
	}
	defer file.Close()
	batchKeywords := fairBLASBatchKeywords(total)
	records := make([]Record, 0, total)
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4<<20)
	for scanner.Scan() {
		var input fairBLASDatasetRecord
		if err := json.Unmarshal(scanner.Bytes(), &input); err != nil {
			b.Fatalf("decode shared dataset record %d: %v", len(records), err)
		}
		ordinal := len(records)
		records = append(records, Record{
			Timestamp: fairBLASTimestamp(input.RawLog, ordinal),
			Keywords:  append(fairBLASKeywords(vocabulary, input.RawLog), batchKeywords[ordinal]...),
			Data:      []byte(input.RawLog),
		})
	}
	if err := scanner.Err(); err != nil {
		b.Fatal(err)
	}
	return records
}

func fairBLASBatchKeyword(placement string, count int) string {
	return fmt.Sprintf("fair-batch-%s-%d", placement, count)
}

func fairBLASSegmentLeavesFromEnvironment() int {
	value := os.Getenv("FAIR_HMF_SEGMENT_LEAVES")
	if value == "" {
		return 1 << 13
	}
	segmentLeaves, err := strconv.Atoi(value)
	if err != nil || segmentLeaves <= 0 {
		panic(fmt.Sprintf("invalid FAIR_HMF_SEGMENT_LEAVES %q", value))
	}
	return segmentLeaves
}

func fairBLASClusteredSegments(placement string) (int, bool) {
	switch placement {
	case "clustered-2":
		return 2, true
	case "clustered-3":
		return 3, true
	case "clustered-4":
		return 4, true
	default:
		return 0, false
	}
}

func fairBLASBatchIndices(placement string, count, total, segmentLeaves int) []int {
	if regionText := os.Getenv("FAIR_REGION0_RECORDS"); regionText != "" {
		region0, err := strconv.Atoi(regionText)
		if err != nil || region0 <= 0 || region0 > total {
			panic(fmt.Sprintf("invalid FAIR_REGION0_RECORDS %q", regionText))
		}
		region1Text := os.Getenv("FAIR_REGION1_RECORDS")
		region1, err := strconv.Atoi(region1Text)
		if err != nil || region1 <= 0 || region0+region1 > total {
			panic(fmt.Sprintf("invalid FAIR_REGION1_RECORDS %q", region1Text))
		}
		start, width := 0, 2*segmentLeaves
		switch placement {
		case "clustered-2":
		case "scattered":
			start, width = region0, total-region0
		default:
			panic("unknown fair BLAS placement: " + placement)
		}
		if count <= 0 || count > width || 2*segmentLeaves > region0 {
			panic(fmt.Sprintf("invalid %s q=%d for R0=%d", placement, count, region0))
		}
		bitCount := bits.Len(uint(width - 1))
		indices := make([]int, 0, count)
		for index := uint64(0); len(indices) < count; index++ {
			offset := int(bits.Reverse64(index) >> (64 - bitCount))
			if offset < width {
				indices = append(indices, start+offset)
			}
		}
		return indices
	}
	clusteredSegments, isClustered := fairBLASClusteredSegments(placement)
	clusteredWindow := clusteredSegments * segmentLeaves
	if count <= 0 || count > total {
		panic(fmt.Sprintf("invalid fair BLAS count %d for %d records", count, total))
	}
	if isClustered && count > 1 &&
		(count < 16 || clusteredWindow > total || count > clusteredWindow) {
		panic(fmt.Sprintf("invalid %s q=%d, segment leaves=%d, total=%d",
			placement, count, segmentLeaves, total))
	}
	indices := make([]int, count)
	for index := range count {
		switch {
		case isClustered && count > 1:
			indices[index] = index * (clusteredWindow - 1) / (count - 1)
		case isClustered:
		case placement == "scattered":
			if count > 1 {
				indices[index] = index * (total - 1) / (count - 1)
			}
		default:
			panic("unknown fair BLAS placement: " + placement)
		}
	}
	return indices
}

func TestFairBLASBatchIndicesUseCommonRecordOrdinals(t *testing.T) {
	for _, test := range []struct {
		placement string
		last      int
	}{
		{"clustered-2", 15},
		{"clustered-3", 23},
		{"clustered-4", 31},
	} {
		got := fairBLASBatchIndices(test.placement, 16, 33, 8)
		if got[0] != 0 || got[len(got)-1] != test.last {
			t.Fatalf("%s = %v", test.placement, got)
		}
		if single := fairBLASBatchIndices(test.placement, 1, 33, 8); len(single) != 1 || single[0] != 0 {
			t.Fatalf("%s q=1 = %v", test.placement, single)
		}
	}
	if got := fmt.Sprint(fairBLASBatchIndices("scattered", 3, 33, 8)); got != "[0 16 32]" {
		t.Fatalf("scattered = %s", got)
	}
	maximum := fairBLASBatchIndices("clustered-2", 16384, 20_000, 8192)
	if len(maximum) != 16384 || maximum[0] != 0 || maximum[len(maximum)-1] != 16383 {
		t.Fatalf("unexpected q=16384 endpoints: first=%d last=%d",
			maximum[0], maximum[len(maximum)-1])
	}
}

func TestFairBLASVerificationSelectionMatchesLogicalALLWorkload(t *testing.T) {
	t.Setenv("FAIR_REGION0_RECORDS", "32768")
	t.Setenv("FAIR_REGION1_RECORDS", "32768")
	for _, placement := range fairBLASPlacements {
		for _, count := range []int{1, 16, 256, 16384} {
			indices := fairBLASBatchIndices(placement, count, 98304, 8192)
			if len(indices) != count {
				t.Fatalf("%s/q=%d returned %d indices", placement, count, len(indices))
			}
			if count >= 16 {
				wantPrefix := []int{0, 8192, 4096, 12288}
				if placement == "scattered" {
					wantPrefix = []int{32768, 65536, 49152, 81920}
				}
				for i, want := range wantPrefix {
					if indices[i] != want {
						t.Fatalf("%s/q=%d index %d = %d, want %d", placement, count, i, indices[i], want)
					}
				}
			}
			seen := make(map[int]bool, count)
			for _, index := range indices {
				if seen[index] || placement == "clustered-2" && (index < 0 || index >= 16384) ||
					placement == "scattered" && (index < 32768 || index >= 98304) {
					t.Fatalf("%s/q=%d invalid ordinal %d", placement, count, index)
				}
				seen[index] = true
			}
		}
	}
}

func fairBLASTokens(raw string) map[string]bool {
	result := make(map[string]bool)
	for _, token := range strings.FieldsFunc(strings.ToLower(raw), func(r rune) bool {
		return !unicode.IsLetter(r)
	}) {
		if len(token) >= 3 && len(token) <= 24 {
			result[token] = true
		}
	}
	return result
}

func fairBLASTimestamp(raw string, index int) int64 {
	fields := strings.Fields(raw)
	if len(fields) >= 3 {
		value := fields[1] + " " + fields[2]
		for _, layout := range []string{"2006-01-02 15:04:05.999999999", "2006-01-02 15:04:05"} {
			if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
				return parsed.UnixMilli()
			}
		}
	}
	return time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC).
		Add(time.Duration(index) * time.Millisecond).UnixMilli()
}
