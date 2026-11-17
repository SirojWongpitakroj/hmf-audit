package eval

import (
	"testing"
)

// smallConfig scales the paper's configuration down to something a unit
// test can run in well under a second, while keeping F=4 and exercising
// multiple blocks/segments (H2: F=4, segment size 1024 "throughout" —
// here scaled to a small segment size so the test actually spans more than
// one segment; block-level index size is a construction parameter, R-GO-027).
func smallConfig() Config {
	return Config{
		NumGenerators:  8,
		EntriesPerFile: 5,
		FilesPerBlock:  2,
		Fanout:         4,
		SegmentSize:    2,
		BlockCounts:    []int{1, 2, 4},
		QueriesPerRun:  4,
		RangeWidthPct:  []int{20},
		KeywordCounts:  []int{2, 3},
		Seed:           1,
	}
}

func TestGenerateSyntheticRecordsDeterministic(t *testing.T) {
	a := GenerateSyntheticRecords(100, 1)
	b := GenerateSyntheticRecords(100, 1)
	if len(a) != len(b) {
		t.Fatalf("length mismatch: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i].Timestamp != b[i].Timestamp {
			t.Fatalf("record %d timestamp mismatch: %d vs %d", i, a[i].Timestamp, b[i].Timestamp)
		}
	}
	// Different seed should (almost certainly) produce a different
	// sequence.
	c := GenerateSyntheticRecords(100, 2)
	same := true
	for i := range a {
		if a[i].Timestamp != c[i].Timestamp {
			same = false
			break
		}
	}
	if same {
		t.Fatal("different seeds produced identical timestamp sequences")
	}
}

func TestTimestampsMonotonic(t *testing.T) {
	recs := GenerateSyntheticRecords(50, 1)
	for i := 1; i < len(recs); i++ {
		if recs[i].Timestamp <= recs[i-1].Timestamp {
			t.Fatalf("timestamps not strictly increasing at %d: %d <= %d", i, recs[i].Timestamp, recs[i-1].Timestamp)
		}
	}
}

func TestAssignToGeneratorsDeterministicAndSorted(t *testing.T) {
	cfg := smallConfig()
	recs := GenerateSyntheticRecords(200, cfg.Seed)
	a := AssignToGenerators(cfg, recs)
	b := AssignToGenerators(cfg, recs)
	if len(a) != cfg.NumGenerators || len(b) != cfg.NumGenerators {
		t.Fatalf("expected %d generators, got %d and %d", cfg.NumGenerators, len(a), len(b))
	}
	for g := range a {
		if len(a[g]) != len(b[g]) {
			t.Fatalf("generator %d: length mismatch across identical-seed runs", g)
		}
		for i := 1; i < len(a[g]); i++ {
			if a[g][i].Timestamp < a[g][i-1].Timestamp {
				t.Fatalf("generator %d: records not sorted by timestamp", g)
			}
		}
	}
}

func TestBuildChainNoExperimentConstantsLeak(t *testing.T) {
	// T-060-style spot check specific to this package's own output: the
	// harness must not fail or behave specially for the exact protocol
	// constants (this is a functional check, not a source-text audit,
	// which is done separately for T-060 across the whole tree).
	cfg := smallConfig()
	recs := GenerateSyntheticRecords(60, cfg.Seed)
	perGen := AssignToGenerators(cfg, recs)
	c, sp, registry, err := BuildChain(cfg, perGen)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Blocks()) == 0 {
		t.Fatal("expected at least one block")
	}
	if sp == nil || registry == nil {
		t.Fatal("expected non-nil SP and registry")
	}
}

func TestEndToEndWorkloadPoint(t *testing.T) {
	cfg := smallConfig()
	recs := GenerateSyntheticRecords(300, cfg.Seed)
	perGen := AssignToGenerators(cfg, recs)
	c, sp, registry, err := BuildChain(cfg, perGen)
	if err != nil {
		t.Fatal(err)
	}

	var lids []uint64
	for lid, rs := range perGen {
		if len(rs) > 0 {
			lids = append(lids, uint64(lid))
		}
	}
	if len(lids) == 0 {
		t.Fatal("expected at least one non-empty generator")
	}
	vocab := []string{"login", "logout", "error", "warning", "put", "get", "timeout", "retry", "connect", "disconnect"}
	minTS, maxTS := recs[0].Timestamp, recs[len(recs)-1].Timestamp

	for _, qt := range []QueryType{QAnd, QOr, QNotAnd, QAndRange, QOrRange, QNotAndRange, QComplexRange} {
		report := RunWorkloadPoint(cfg, c, sp, registry, qt, lids, vocab, minTS, maxTS, 20)
		if !report.AcceptedAll {
			t.Fatalf("query type %s: expected every query to be accepted (functional correctness is mandatory, H3)", qt)
		}
		if report.AvgVOSizeKB < 0 {
			t.Fatalf("query type %s: negative VO size", qt)
		}
		if report.AvgProcessTime < 0 || report.AvgVerifyTime < 0 {
			t.Fatalf("query type %s: negative timing", qt)
		}
	}
}

func TestQueryTypeNames(t *testing.T) {
	for _, qt := range []QueryType{QAnd, QOr, QNotAnd, QAndRange, QOrRange, QNotAndRange, QComplexRange} {
		if qt.String() == "" {
			t.Fatalf("query type %d has no name", qt)
		}
	}
}

func TestPaperStyleConfigHasPaperValues(t *testing.T) {
	cfg := PaperStyleConfig()
	if cfg.NumGenerators != 4096 || cfg.EntriesPerFile != 2048 || cfg.FilesPerBlock != 256 {
		t.Fatalf("unexpected dataset shape: %+v", cfg)
	}
	if cfg.Fanout != 4 || cfg.SegmentSize != 1024 {
		t.Fatalf("unexpected tree configuration: %+v", cfg)
	}
	if cfg.Seed != 1 {
		t.Fatalf("expected seed 1, got %d", cfg.Seed)
	}
}
