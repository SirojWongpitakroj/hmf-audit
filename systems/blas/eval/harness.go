package eval

import (
	"time"

	"blas/chain"
	"blas/query"
	"blas/verify"
)

// RunResult is one query's measured outcome (R-EVAL-017): processing time
// at the full node/SP, verification time at the auditor, and VO size.
// Timing is wall-clock (time.Now()), not OS-level CPU time — Go's standard
// library has no portable per-call CPU-time accounting, and this harness
// runs strictly single-goroutine/sequential (G8), so wall-clock time here
// is not confounded by concurrent work. R-EVAL-017's own "composition of
// query time, CPU vs wall clock" point is GAP-43, unresolved by
// DECISIONS.md; this is the harness's documented convention, not a claim
// that it resolves the gap.
type RunResult struct {
	QueryType     QueryType
	ProcessTime   time.Duration
	VerifyTime    time.Duration
	VOSizeBytes   int
	Accepted      bool
	ResultEntries int
}

// RunOnce executes one audited query end-to-end and measures it.
func RunOnce(fn *query.FullNode, sp *query.SP, auditor *verify.Auditor, keywordUniverse map[uint64][]string, q query.Q, qt QueryType) RunResult {
	return RunOnceWithOptions(fn, sp, auditor, keywordUniverse, q, qt, verify.Options{})
}

// RunOnceWithOptions is RunOnce with explicit Optimization 1/2 switches
// (R-OPT-015/T-053), used by the ablation harness (RunAblation) to
// reproduce the All/No-aggre/No-multi-win variants (VIII-C, Fig. 7 — F7 in
// DECISIONS.md: "No-multi-win" disables Optimization 1's segmentation,
// leaving Optimization 2 aggregation on).
func RunOnceWithOptions(fn *query.FullNode, sp *query.SP, auditor *verify.Auditor, keywordUniverse map[uint64][]string, q query.Q, qt QueryType, opts verify.Options) RunResult {
	qon, qoff := query.Split(q)

	t0 := time.Now()
	var s1 query.Stage1Result
	var s2 query.Stage2Result
	var bindings map[uint64]query.AggregatedLGBinding
	var err error
	if opts.UseSegmentScan {
		s1, err = fn.Stage1Optimized(qon)
	} else {
		s1, err = fn.Stage1(qon)
	}
	if err == nil {
		// Branch on UseAggregation too (not just UseSegmentScan): the
		// aggregated path returns AggregatedResultTxs/bindings instead of
		// ResultProofs, and VOSizeFromStage below must measure whichever
		// evidence path the query actually took, or the ablation
		// comparison it feeds is meaningless (both variants would report
		// the same, non-aggregated, VO shape).
		if opts.UseAggregation {
			s2, bindings, err = fn.Stage2Aggregated(qon, s1)
		} else {
			s2, err = fn.Stage2(qon, s1.CandidateHeights)
		}
	}
	processTime := time.Since(t0)
	if err != nil {
		return RunResult{QueryType: qt, ProcessTime: processTime, Accepted: false}
	}

	// Plan Q_off outside both timers. This preliminary auditor pass is needed
	// only to choose files; VerifyResponse repeats and times every check.
	response := verify.Response{
		Stage1: s1, Stage2: s2, Bindings: bindings,
	}
	refs, ok := auditor.RequestedFiles(keywordUniverse, q, response, opts)
	if !ok {
		return RunResult{QueryType: qt, ProcessTime: processTime, Accepted: false}
	}
	spStart := time.Now()
	response.Files, err = sp.AnswerQOff(refs, qoff)
	processTime += time.Since(spStart)
	if err != nil {
		return RunResult{QueryType: qt, ProcessTime: processTime, Accepted: false}
	}
	t1 := time.Now()
	res := auditor.VerifyResponse(keywordUniverse, q, response, opts)
	verifyTime := time.Since(t1)

	voSize := VOSizeFromStage(s1, s2, bindings)

	return RunResult{
		QueryType: qt, ProcessTime: processTime, VerifyTime: verifyTime,
		VOSizeBytes: voSize, Accepted: res.Accept, ResultEntries: len(res.Entries),
	}
}

// VOSizeFromStage sizes just the on-chain (Stage 1 + Stage 2) portion of a
// query answer — the part directly comparable across block counts without
// depending on how many files happened to pass the bm_EW filter. bindings
// must be nil when s2 came from the plain (non-aggregated) Stage2 path (see
// VOSize's doc comment).
func VOSizeFromStage(s1 query.Stage1Result, s2 query.Stage2Result, bindings map[uint64]query.AggregatedLGBinding) int {
	return VOSize(s1, s2, nil, bindings)
}

// AggregateReport is the averaged outcome for one workload point (R-EVAL-010:
// "generate 10 random queries and report the average").
type AggregateReport struct {
	QueryType      QueryType
	NumBlocks      int
	AvgProcessTime time.Duration
	AvgVerifyTime  time.Duration
	AvgVOSizeKB    float64
	AcceptedAll    bool
	AvgResultCount float64
}

// RunWorkloadPoint runs cfg.QueriesPerRun queries of qt against the given
// chain and reports the average (R-EVAL-010).
func RunWorkloadPoint(cfg Config, c *chain.Chain, sp *query.SP, registry map[uint64][]string, qt QueryType, lids []uint64, vocab []string, minTS, maxTS int64, rangeWidthPct int) AggregateReport {
	return RunWorkloadPointWithOptions(cfg, c, sp, registry, qt, lids, vocab, minTS, maxTS, rangeWidthPct, verify.Options{})
}

// RunWorkloadPointWithOptions is RunWorkloadPoint with explicit
// Optimization 1/2 switches, used by RunAblation.
func RunWorkloadPointWithOptions(cfg Config, c *chain.Chain, sp *query.SP, registry map[uint64][]string, qt QueryType, lids []uint64, vocab []string, minTS, maxTS int64, rangeWidthPct int, opts verify.Options) AggregateReport {
	fn := query.NewFullNode(c)
	headers := make([]chain.BlockHeader, len(c.Blocks()))
	for i, b := range c.Blocks() {
		headers[i] = b.Header
	}
	auditor := verify.NewAuditor(headers, cfg.SegmentSize)

	queries := GenerateQueries(qt, cfg.QueriesPerRun, cfg.Seed, lids, vocab, minTS, maxTS, rangeWidthPct)

	var totalProcess, totalVerify time.Duration
	var totalVO int
	var totalEntries int
	acceptedAll := true
	for _, q := range queries {
		r := RunOnceWithOptions(fn, sp, auditor, registry, q, qt, opts)
		totalProcess += r.ProcessTime
		totalVerify += r.VerifyTime
		totalVO += r.VOSizeBytes
		totalEntries += r.ResultEntries
		if !r.Accepted {
			acceptedAll = false
		}
	}
	n := len(queries)
	if n == 0 {
		n = 1
	}
	return AggregateReport{
		QueryType: qt, NumBlocks: len(c.Blocks()),
		AvgProcessTime: totalProcess / time.Duration(n),
		AvgVerifyTime:  totalVerify / time.Duration(n),
		AvgVOSizeKB:    float64(totalVO) / float64(n) / float64(KB),
		AcceptedAll:    acceptedAll,
		AvgResultCount: float64(totalEntries) / float64(n),
	}
}

// AblationVariant names the three VIII-C/Fig. 7 ablation configurations
// (R-OPT-015/T-053/T-071). F7 in DECISIONS.md: "multi-window" is Section
// VI's block-level segmentation/partition processing, so "No-multi-win"
// disables Optimization 1's segment scan while leaving Optimization 2 on;
// "No-aggre" disables Optimization 2 while leaving Optimization 1 on;
// "All" enables both.
type AblationVariant string

const (
	AblationAll         AblationVariant = "All"
	AblationNoAggregate AblationVariant = "No-aggre"
	AblationNoMultiWin  AblationVariant = "No-multi-win"
)

func ablationOptions(v AblationVariant) verify.Options {
	switch v {
	case AblationAll:
		return verify.Options{UseSegmentScan: true, UseAggregation: true}
	case AblationNoAggregate:
		return verify.Options{UseSegmentScan: true, UseAggregation: false}
	case AblationNoMultiWin:
		return verify.Options{UseSegmentScan: false, UseAggregation: true}
	default:
		return verify.Options{}
	}
}

// RunAblation runs the same query type/workload under all three VIII-C
// variants against a chain built WITH Optimization 2 enabled
// (chain.NewChainWithOptions(..., true) — aggregation structures must
// exist at construction time regardless of which variant's query-time
// switches are exercised) and reports each variant's AggregateReport
// (R-EVAL-016/T-071). Because query-time switches never change RESULTS
// (only which evidence path is taken), every variant is expected to accept
// with identical result counts — RunAblation itself does not assert this;
// callers (tests) do, since asserting it here would silently hide a
// genuine cross-variant divergence bug instead of surfacing it as a test
// failure.
func RunAblation(cfg Config, c *chain.Chain, sp *query.SP, registry map[uint64][]string, qt QueryType, lids []uint64, vocab []string, minTS, maxTS int64, rangeWidthPct int) map[AblationVariant]AggregateReport {
	out := map[AblationVariant]AggregateReport{}
	for _, v := range []AblationVariant{AblationAll, AblationNoAggregate, AblationNoMultiWin} {
		out[v] = RunWorkloadPointWithOptions(cfg, c, sp, registry, qt, lids, vocab, minTS, maxTS, rangeWidthPct, ablationOptions(v))
	}
	return out
}
