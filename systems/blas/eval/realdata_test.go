package eval

import (
	"strings"
	"testing"
)

const androidSamplePath = "testdata/android_v1_sample.log"

func TestLoadAndroidV1ParsesRealData(t *testing.T) {
	records, err := LoadAndroidV1(androidSamplePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) < 15000 {
		t.Fatalf("expected at least 15000 parsed records out of 20000 raw lines, got %d", len(records))
	}
	for _, r := range records {
		if len(r.Keywords) != 2 {
			t.Fatalf("expected exactly 2 keywords (level, component) per record, got %v", r.Keywords)
		}
	}
}

func TestLoadAndroidV1Deterministic(t *testing.T) {
	a, err := LoadAndroidV1(androidSamplePath)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadAndroidV1(androidSamplePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != len(b) {
		t.Fatalf("length mismatch across identical loads: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Timestamp != b[i].Timestamp {
			t.Fatalf("record %d timestamp mismatch across identical loads", i)
		}
	}
}

func TestLoadAndroidV1MissingFile(t *testing.T) {
	if _, err := LoadAndroidV1("testdata/does-not-exist.log"); err == nil {
		t.Fatal("expected an error for a missing file")
	}
}

// TestRealDatasetEndToEnd runs the full H1/H4/H5 pipeline — deterministic
// generator assignment (seed 1), stable timestamp sort, file/block packing,
// and a full audited query — against the REAL LogHub Android_v1 sample,
// not the synthetic generator. This is the harness's actual, mandatory
// correctness bar (H3) exercised on real data.
func TestRealDatasetEndToEnd(t *testing.T) {
	records, err := LoadAndroidV1(androidSamplePath)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		NumGenerators:  50, // scaled down from 4096 for test runtime; still >1 generator, exercising real assignment
		EntriesPerFile: 200,
		FilesPerBlock:  3,
		Fanout:         4,
		SegmentSize:    4,
		QueriesPerRun:  4,
		Seed:           1, // H1/H2: seed 1
	}

	perGen := AssignToGenerators(cfg, records)
	nonEmpty := 0
	for _, recs := range perGen {
		if len(recs) > 0 {
			nonEmpty++
			for i := 1; i < len(recs); i++ {
				if recs[i].Timestamp < recs[i-1].Timestamp {
					t.Fatalf("generator's records not stable-sorted by timestamp (H1/H4)")
				}
			}
		}
	}
	if nonEmpty == 0 {
		t.Fatal("expected at least one non-empty generator")
	}

	c, sp, registry, err := BuildChain(cfg, perGen)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks()) == 0 {
		t.Fatal("expected at least one block from the real dataset")
	}

	var lids []uint64
	for lid, recs := range perGen {
		if len(recs) > 0 {
			lids = append(lids, uint64(lid))
		}
	}
	// Derive a real keyword vocabulary from the actual parsed records
	// (level/component tokens), not the synthetic 10-word vocabulary.
	seen := map[string]bool{}
	var vocab []string
	for _, r := range records {
		for _, w := range r.Keywords {
			if !seen[w] {
				seen[w] = true
				vocab = append(vocab, w)
			}
		}
		if len(vocab) >= 20 {
			break
		}
	}
	minTS, maxTS := records[0].Timestamp, records[0].Timestamp
	for _, r := range records {
		if r.Timestamp < minTS {
			minTS = r.Timestamp
		}
		if r.Timestamp > maxTS {
			maxTS = r.Timestamp
		}
	}

	for _, qt := range []QueryType{QAnd, QOr, QNotAnd, QAndRange} {
		report := RunWorkloadPoint(cfg, c, sp, registry, qt, lids, vocab, minTS, maxTS, 20)
		if !report.AcceptedAll {
			t.Fatalf("query type %s over real data: expected every query to be accepted", qt)
		}
	}
}

// TestAblationOnRealData: T-071 (mechanism only, see TRACKER.md's scope
// note — full Fig. 7 numeric reproduction needs paper-scale real data this
// environment does not have) — the three VIII-C ablation variants (All,
// No-aggre, No-multi-win) all run to completion, all accept, and all
// return the identical result set, against the real LogHub Android_v1
// sample.
func TestAblationOnRealData(t *testing.T) {
	records, err := LoadAndroidV1(androidSamplePath)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		NumGenerators:  20,
		EntriesPerFile: 100,
		FilesPerBlock:  2,
		Fanout:         4,
		SegmentSize:    3,
		QueriesPerRun:  3,
		Seed:           1,
		UseAggregation: true, // required at construction time for the aggregation-using variants
	}
	perGen := AssignToGenerators(cfg, records)
	c, sp, registry, err := BuildChain(cfg, perGen)
	if err != nil {
		t.Fatal(err)
	}

	var lids []uint64
	for lid, recs := range perGen {
		if len(recs) > 0 {
			lids = append(lids, uint64(lid))
		}
	}
	if len(lids) == 0 {
		t.Fatal("expected at least one non-empty generator")
	}
	seen := map[string]bool{}
	var vocab []string
	for _, r := range records {
		for _, w := range r.Keywords {
			if !seen[w] {
				seen[w] = true
				vocab = append(vocab, w)
			}
		}
	}
	minTS, maxTS := records[0].Timestamp, records[0].Timestamp
	for _, r := range records {
		if r.Timestamp < minTS {
			minTS = r.Timestamp
		}
		if r.Timestamp > maxTS {
			maxTS = r.Timestamp
		}
	}

	// R-EVAL-016: two-keyword AND Boolean range queries, selectivity 0.2.
	reports := RunAblation(cfg, c, sp, registry, QAndRange, lids, vocab, minTS, maxTS, 20)

	if len(reports) != 3 {
		t.Fatalf("expected 3 ablation variants, got %d", len(reports))
	}
	var first *AggregateReport
	for _, v := range []AblationVariant{AblationAll, AblationNoAggregate, AblationNoMultiWin} {
		r, ok := reports[v]
		if !ok {
			t.Fatalf("missing ablation variant %s", v)
		}
		if !r.AcceptedAll {
			t.Fatalf("variant %s: expected every query to be accepted", v)
		}
		if first == nil {
			first = &r
			continue
		}
		if r.AvgResultCount != first.AvgResultCount {
			t.Fatalf("variant %s: avg result count %.2f, want %.2f (all variants must agree on results, T-053)", v, r.AvgResultCount, first.AvgResultCount)
		}
	}

	// Regression: the harness's own VO-size accounting must actually take
	// the aggregated evidence path when Optimization 2 is on, not silently
	// fall back to measuring the plain per-block proofs regardless of
	// UseAggregation (a real bug caught by independent verification: All
	// and No-aggre were reporting byte-for-byte identical VO sizes here).
	// This dataset's blocks are deliberately small (FilesPerBlock=2,
	// SegmentSize=3), so the one-time per-segment LG binding proof
	// aggregation adds is not guaranteed to be smaller than the per-block
	// OMT proofs it replaces at this scale (see
	// TestAggregationReducesHarnessVOSize for a fixture sized so the net
	// saving actually shows up) — the safe, scale-independent assertion
	// here is only that the two variants are measuring genuinely different
	// evidence, not a specific direction.
	all, noAggre := reports[AblationAll], reports[AblationNoAggregate]
	if all.AvgVOSizeKB == noAggre.AvgVOSizeKB {
		t.Fatalf("expected AblationAll and AblationNoAggregate to report different VO sizes (aggregation changes the evidence shape), got identical All=No-aggre=%.6f KB", all.AvgVOSizeKB)
	}
}

// TestFormatReportTableOnRealData: M12 "Table/figure-layout reporting" —
// results from the real dataset laid out in the Table II/III row-per
// -scenario shape (R-EVAL-019, scope-limited per TRACKER.md: no
// paper-scale block-count sweep, since the real sample available here is
// far smaller than the paper's corpus).
func TestFormatReportTableOnRealData(t *testing.T) {
	records, err := LoadAndroidV1(androidSamplePath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		NumGenerators: 20, EntriesPerFile: 100, FilesPerBlock: 2,
		Fanout: 4, SegmentSize: 4, QueriesPerRun: 2, Seed: 1,
	}
	perGen := AssignToGenerators(cfg, records)
	c, sp, registry, err := BuildChain(cfg, perGen)
	if err != nil {
		t.Fatal(err)
	}
	var lids []uint64
	for lid, recs := range perGen {
		if len(recs) > 0 {
			lids = append(lids, uint64(lid))
		}
	}
	seen := map[string]bool{}
	var vocab []string
	for _, r := range records {
		for _, w := range r.Keywords {
			if !seen[w] {
				seen[w] = true
				vocab = append(vocab, w)
			}
		}
	}
	minTS, maxTS := records[0].Timestamp, records[len(records)-1].Timestamp

	var reports []AggregateReport
	for _, qt := range []QueryType{QAnd, QOr, QNotAnd} {
		reports = append(reports, RunWorkloadPoint(cfg, c, sp, registry, qt, lids, vocab, minTS, maxTS, 20))
	}
	table := FormatReportTable(reports)
	if !strings.Contains(table, "AND") || !strings.Contains(table, "OR") || !strings.Contains(table, "NOT-AND") {
		t.Fatalf("expected all three scenario names in the report table, got:\n%s", table)
	}
	t.Logf("\n%s", table)
}
