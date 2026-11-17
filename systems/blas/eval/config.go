// Package eval is the evaluation harness (§12). This is the ONLY package
// permitted to hold §12 experiment constants (R-GO-027) — nothing here may
// leak into a protocol package (`bhash`, `bitmap`, `omt`, `iimt`, `rimbt`,
// `iomf`, `alis`, `chain`, `query`, `verify`, `optimize`), and every value
// below is a configuration input, not a hard-coded protocol rule.
//
// Scope limitation (recorded honestly, not glossed over): R-EVAL-001/H1
// call for the actual LogHub Android-v1 and Spark datasets. Neither dataset
// file is present in this repository or reachable from this offline
// environment, so this harness cannot literally ingest them. Instead,
// `dataset.go` provides a seeded synthetic generator that produces data in
// the shape §12 describes (timestamped entries with keyword sets,
// distributed across generators), so the harness plumbing itself — packing,
// chain construction, seeded query generation, metrics — can be exercised
// end-to-end and is ready to accept real LogHub data through the same
// `Record` shape once it is available. This is not a numeric reproduction
// of Tables II–VI; see H3's own framing (correctness mandatory, trend/
// scaling agreement is the bar, not exact numbers) and R-EVAL-021.
package eval

// Config holds every §12 evaluation parameter. Nothing here is a protocol
// default — each field must be set explicitly by the caller (typically a
// `_test.go` file or a future `cmd/` driver), matching R-GO-027/R-GO-028.
type Config struct {
	// Dataset shape (R-EVAL-001..004).
	NumGenerators  int // paper: 4096
	EntriesPerFile int // paper: 2048
	FilesPerBlock  int // paper: 256

	// Tree/index configuration (R-EVAL-008, VIII-B).
	Fanout      int    // paper: 4
	SegmentSize uint64 // paper: 1024 (block-level index size)

	// Workload (R-EVAL-010..016).
	BlockCounts   []int // paper: 128, 512, 1024, 4096, 8192
	QueriesPerRun int   // paper: 10
	RangeWidthPct []int // paper: 10, 20, 30, 40, 50 (percent of the dataset's timeline)
	KeywordCounts []int // paper: 2..10 (Fig. 6)

	// Randomness (R-EVAL-021/R-GO-028): every random choice must be
	// seeded and recorded, never crypto/rand or unseeded math/rand.
	Seed int64 // paper/DECISIONS.md H1/H2: seed 1

	// UseAggregation controls whether BuildChain constructs the chain with
	// Optimization 2's per-segment aggregation structures (chain.
	// NewChainWithOptions). This must be true at CONSTRUCTION time for any
	// ablation variant that exercises aggregation at query time (R-EVAL-016,
	// T-071) — query-time Options only choose which already-built evidence
	// path to use, they cannot conjure the aggregated trees after the fact.
	UseAggregation bool
}

// PaperStyleConfig returns the paper's own §12/VIII-B configuration values,
// as a starting point a caller may scale down for test runtime (R-GO-027:
// even this is a configuration value the eval package holds, not something
// baked into any protocol package).
func PaperStyleConfig() Config {
	return Config{
		NumGenerators:  4096,
		EntriesPerFile: 2048,
		FilesPerBlock:  256,
		Fanout:         4,
		SegmentSize:    1024,
		BlockCounts:    []int{128, 512, 1024, 4096, 8192},
		QueriesPerRun:  10,
		RangeWidthPct:  []int{10, 20, 30, 40, 50},
		KeywordCounts:  []int{2, 3, 4, 5, 6, 7, 8, 9, 10},
		Seed:           1,
	}
}
