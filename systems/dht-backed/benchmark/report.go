package benchmark

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"dhtbacked/mktree"
)

// Paper reference values (Sec. V.C.1-3). Per Q34 and §0.4.1 these are
// INFORMATIONAL reference values from a Python implementation on different
// hardware. They are reported beside the measurement; they are never asserted,
// and a shortfall is a measured shortfall, not a defect.
const (
	PaperSingleRecordLatencySeconds = 0.000035
	PaperThroughputLogsPerSecond    = 34482.76
	PaperCommitTimeSeconds          = 0.0053
)

// Report holds every measured result of one evaluation run.
type Report struct {
	StartedAt time.Time
	GOOS      string
	GOARCH    string
	GoVersion string
	NumCPU    int
	EpochSize uint64
	Stride    uint64
	Window    uint64

	SingleRecordLatency *Summary
	Throughput          *Summary
	CommitTime          *Summary
	BatchScaling        map[string][]BatchPoint
	Stride_             map[string][]StridePoint
	WindowSweep         map[string][]WindowPoint
	OnChain             []OnChainPoint
	Notes               []string
}

// NewReport captures the environment of the run (R-ENV-06, Q35).
func NewReport(epochSize, stride, window uint64) *Report {
	return &Report{
		StartedAt:    time.Now().UTC(),
		GOOS:         runtime.GOOS,
		GOARCH:       runtime.GOARCH,
		GoVersion:    runtime.Version(),
		NumCPU:       runtime.NumCPU(),
		EpochSize:    epochSize,
		Stride:       stride,
		Window:       window,
		BatchScaling: map[string][]BatchPoint{},
		Stride_:      map[string][]StridePoint{},
		WindowSweep:  map[string][]WindowPoint{},
	}
}

// Note records a free-text observation, such as an experiment that could not
// run and why (Q32: failures are recorded explicitly).
func (r *Report) Note(format string, args ...any) {
	r.Notes = append(r.Notes, fmt.Sprintf(format, args...))
}

// Markdown renders the evaluation report.
func (r *Report) Markdown() string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("# Evaluation report — DHT-Backed Ancestor-Assisted Merkle Verification")
	w("")
	w("Generated %s.", r.StartedAt.Format(time.RFC3339))
	w("")
	w("## Pass/fail line (§0.4.1, Q34)")
	w("")
	w("Functional correctness is pass/fail and is covered by the Go test suite.")
	w("**The performance figures below are reported, not asserted.** The paper's")
	w("0.000035 s single-record latency, 34,482.76 logs/s throughput and 0.0053 s commit")
	w("time are informational reference values measured on a Python 3.10 implementation")
	w("on different hardware (Table 5, Sec. V.C). A shortfall against any of them is")
	w("recorded here as a measured shortfall; it is never treated as a defect and is")
	w("never engineered away with an optimization the paper does not state (§0.6).")
	w("")
	w("## Run environment (R-ENV-06, Q35)")
	w("")
	w("| Item | Value |")
	w("|---|---|")
	w("| Operating system | `%s` |", r.GOOS)
	w("| Architecture | `%s` |", r.GOARCH)
	w("| Go toolchain | `%s` |", r.GoVersion)
	w("| Logical CPUs visible | %d |", r.NumCPU)
	w("| GPU used | none — no operation in Sec. III uses a GPU (R-ENV-06) |")
	w("| Execution model | single goroutine; no parallelism anywhere (Q32, §0.6) |")
	w("")
	w("The paper's own reference hardware (Sec. V.C, p. 9) was an AMD Ryzen 5 5600X with")
	w("an NVIDIA RTX 3060 Ti and 16 GB RAM on Windows 10. It is recorded for context")
	w("only; this run used the machine described above.")
	w("")
	w("## Measurement protocol (Q39)")
	w("")
	w("- Timing uses Go's monotonic clock via `time.Now()` / `time.Since()`.")
	w("- `testing.B` is **not** the reproduction benchmark: its adaptive iteration model")
	w("  differs from the paper's fixed protocol.")
	w("- Every experiment runs **exactly %d independent repetitions**.", Repetitions)
	w("- Results are means with a 95%% confidence interval computed from the Student-t")
	w("  critical value for 49 degrees of freedom (t = %.6f), in ordinary Go code.", tCritical95[49])
	w("")
	w("## Batch sizes (Q30)")
	w("")
	w("**The paper does not specify any of these batch sizes.** They are the sizes the")
	w("Q30 decision fixes. A batch must lie within one epoch, and a locality comparison")
	w("within one tree, but Q30's n = 1024 exceeds the entire default 1,000-record epoch.")
	w("So, per the GAP-7 decision, the locality/batch-scaling and stride experiments run")
	w("in a **%d-record epoch**, the only Q23 evaluation epoch in which every Q30 size", LargeBatchEpochCapacity)
	w("fits in one tree. Its trees are padded to %d leaves, so **L = %d** there, not the",
		mktree.PaddedWidth(LargeBatchEpochCapacity), levelsFor(LargeBatchEpochCapacity))
	w("default L = %d. If any section still measured a different size, it says so.", levelsFor(r.EpochSize))
	w("")
	w("| Experiment | Size |")
	w("|---|---|")
	w("| Default commit/epoch benchmark | B = %d |", CommitBatch)
	w("| Single-record experiment | n = %d |", SingleRecordBatch)
	w("| Headline verification throughput | %d verification requests |", ThroughputRequests)
	w("| Locality/batch scaling | n in {64, 128, 256, 512, 1024, 2048, 4096} |")
	w("| Stride/window experiment | n = %d |", StrideWindowBatch)
	w("")
	w("Configuration for this run: epoch capacity %d, stride s = %d, window W = %d. The",
		r.EpochSize, r.Stride, r.Window)
	w("locality/batch-scaling and stride experiments use a %d-record epoch (GAP-7).", LargeBatchEpochCapacity)
	w("")

	w("## Headline measurements")
	w("")
	w("| Metric | Measured mean (95%% CI) | Paper reference (Python, other hardware) | Relation |")
	w("|---|---|---|---|")
	if r.SingleRecordLatency != nil {
		w("| Single-record verification latency (R-EVAL-03) | %.9f s ± %.9f | %.6f s | %s |",
			r.SingleRecordLatency.Mean, r.SingleRecordLatency.CI95, PaperSingleRecordLatencySeconds,
			relationLowerIsBetter(r.SingleRecordLatency.Mean, PaperSingleRecordLatencySeconds))
	}
	if r.Throughput != nil {
		w("| Verification throughput (R-EVAL-04) | %.2f verifications/s ± %.2f | %.2f logs/s | %s |",
			r.Throughput.Mean, r.Throughput.CI95, PaperThroughputLogsPerSecond,
			relationHigherIsBetter(r.Throughput.Mean, PaperThroughputLogsPerSecond))
	}
	if r.CommitTime != nil {
		w("| Commit time for B = %d (R-EVAL-05) | %.6f s ± %.6f | %.4f s | %s |",
			CommitBatch, r.CommitTime.Mean, r.CommitTime.CI95, PaperCommitTimeSeconds,
			relationLowerIsBetter(r.CommitTime.Mean, PaperCommitTimeSeconds))
	}
	w("")
	w("Any row marked \"measured shortfall\" is exactly that: a measurement, recorded as")
	w("required by §0.4.1. It is not a defect and is not grounds for adding an unstated")
	w("optimization.")
	w("")
	w("**What these figures include.** Two costs the specification requires sit inside every")
	w("timed verification, and together they account for nearly all of it:")
	w("")
	w("1. **One JSON-RPC `getRoot` call** to the external Hardhat chain per verification, because")
	w("   Phase 4 step 4 compares the recomputed root-of-roots with the on-chain anchor (Q26,")
	w("   R-VER-05). This network round trip is the largest single cost. Its latency depends on the")
	w("   external Hardhat process and drifts between runs, so a 95%% CI computed within one run")
	w("   understates run-to-run variation for any figure that includes it.")
	w("2. **An Ed25519 signature check on every DHT proof request.** Q13 sends the authorization")
	w("   token with each request and requires the DHT to verify the CA signature every time. One")
	w("   single-record verification at s = %d makes %d such requests: L - s sibling records, one", r.Stride, verificationRequests(r))
	w("   ancestor certificate, and one Root Record per log type for anchor reconciliation (Q26).")
	w("")
	w("Neither is cached. Q32 authorizes only a request-local cache of `P_j,k` records; caching the")
	w("anchor or a verified token across requests would be an unstated optimization (§0.6).")
	w("")
	w("**What these figures exclude: PostgreSQL I/O.** Records are read from the in-memory")
	w("store (`storage.Memory`), so every latency, throughput and batch figure in this report")
	w("measures the cryptographic and proof work plus the anchor call, and **excludes any")
	w("PostgreSQL round trip** for record retrieval (GAP-2 decision). They are not end-to-end")
	w("figures against the reproduced data lake.")
	w("")

	if len(r.OnChain) > 0 {
		w("## Constant on-chain storage (R-EVAL-07, R-EPO-05)")
		w("")
		w("The on-chain payload is `(epoch, R_agg)` only — no client timestamp and no CA")
		w("signature (Q15). One `bytes32` is stored per epoch.")
		w("")
		w("| Epoch size (records) | Anchored epoch | On-chain bytes written |")
		w("|---|---|---|")
		for _, p := range r.OnChain {
			w("| %d | %d | %d |", p.EpochSize, p.Epoch, p.BytesOnChain)
		}
		w("")
	}

	if len(r.BatchScaling) > 0 {
		w("## Locality and batch scaling (R-EVAL-06, R-VER-10)")
		w("")
		w("| Distribution | n | Subtree-local mean (s) | Scattered mean (s) | g local | g scattered | Same outcome |")
		w("|---|---|---|---|---|---|---|")
		degenerate := false
		for _, dist := range Distributions {
			for _, p := range r.BatchScaling[string(dist)] {
				if p.ScatteredIsLocal {
					degenerate = true
					w("| %s | %d | %.6f ± %.6f | — identical to local † | %d | — | %t |",
						dist, p.N, p.Local.Mean, p.Local.CI95, p.LocalAncestors, p.Identical)
					continue
				}
				w("| %s | %d | %.6f ± %.6f | %.6f ± %.6f | %d | %d | %t |",
					dist, p.N, p.Local.Mean, p.Local.CI95, p.Scattered.Mean, p.Scattered.CI95,
					p.LocalAncestors, p.ScatteredAncestors, p.Identical)
			}
		}
		w("")
		if degenerate {
			w("† The largest tree held fewer than 2n leaves, so the scattering step rounded")
			w("down to 1 and the \"scattered\" batch was the local batch itself. Those rows are")
			w("not locality comparisons and no scattered figure is reported for them.")
			w("")
		}
		var skipped []string
		for _, dist := range Distributions {
			pts := r.BatchScaling[string(dist)]
			if len(pts) == 0 {
				// No points means the experiment failed, not that sizes did not
				// fit; that failure is already recorded in the notes, so it
				// must not be misattributed to tree size here.
				continue
			}
			ran := map[int]bool{}
			for _, p := range pts {
				ran[p.N] = true
			}
			var miss []string
			for _, n := range ScalingBatchSizes {
				if !ran[n] {
					miss = append(miss, fmt.Sprint(n))
				}
			}
			if len(miss) > 0 {
				skipped = append(skipped, fmt.Sprintf("%s: n = %s", dist, strings.Join(miss, ", ")))
			}
		}
		if len(skipped) > 0 {
			w("**Q30 sizes not measured.** Q30 fixes n in %v, but a batch must lie within one", ScalingBatchSizes)
			w("tree, and at epoch capacity %d no single tree held that many leaves:", LargeBatchEpochCapacity)
			w("")
			for _, s := range skipped {
				w("- %s", s)
			}
			w("")
		}
		w("Correctness is identical for subtree-local and scattered batches; only timing")
		w("differs (R-VER-10 acceptance).")
		w("")
	}

	if len(r.Stride_) > 0 {
		w("## Stride sensitivity (R-EVAL-06, Q25)")
		w("")
		w("Levels are indexed as the paper formally defines them: the root is j = 0 and")
		w("the leaves are j = L, so a **larger s places checkpoints closer to the leaves**")
		w("and increases their number. Sec. V.C.5's contrary causal explanation is")
		w("deliberately not reproduced; the trend is reported against Theorem G.1's bound")
		w("H(S, s) <= n(L - s) + gs instead.")
		w("")
		w("| Distribution | s | n measured | Batch mean (s) | g | Hash recomputations | Theorem G.1 bound | Within bound |")
		w("|---|---|---|---|---|---|---|---|")
		short := false
		for _, dist := range Distributions {
			for _, p := range r.Stride_[string(dist)] {
				if p.N != StrideWindowBatch {
					short = true
				}
				w("| %s | %d | %d | %.6f ± %.6f | %d | %d | %d | %t |",
					p.Distribution, p.Stride, p.N, p.Batch.Mean, p.Batch.CI95,
					p.Ancestors, p.HashCount, p.Bound, p.HashCount <= p.Bound)
			}
		}
		w("")
		if short {
			w("**n differs from Q30's n = %d.** The batch is every record of the largest", StrideWindowBatch)
			w("log type, capped at %d. At epoch capacity %d the largest tree held fewer", StrideWindowBatch, LargeBatchEpochCapacity)
			w("than %d leaves, so the \"n measured\" column", StrideWindowBatch)
			w("gives the true size. It is constant across s within a distribution, so the stride")
			w("trend within each distribution is valid; it differs between distributions, so")
			w("absolute times should not be compared across distributions. See GAP-7 in TRACKER.md.")
			w("")
		}
	}

	if len(r.WindowSweep) > 0 {
		w("## Window sensitivity (R-EVAL-06, Q21, Q54)")
		w("")
		w("Windows are count-based and non-overlapping (tumbling). At the default")
		w("configuration (epoch 1,000, W = 1,024) there is exactly one window, windowID 0,")
		w("which never fills — the specified outcome under Q54, not a defect.")
		w("")
		w("| Distribution | W | Windows in the largest tree | Micro-root check mean (s) |")
		w("|---|---|---|---|")
		for _, dist := range Distributions {
			for _, p := range r.WindowSweep[string(dist)] {
				w("| %s | %d | %d | %.6f ± %.6f |", p.Distribution, p.Window, p.Windows,
					p.MicroRoot.Mean, p.MicroRoot.CI95)
			}
		}
		w("")
	}

	w("## Functional comparison (R-EVAL-09, Table 2)")
	w("")
	w("| Feature | This work (reproduced) |")
	w("|---|---|")
	w("| Real-time / windowed verification | yes — provisional micro-roots (Phase 5(b)) |")
	w("| Log-specific | yes — one MK-Tree per log type per epoch |")
	w("| Trust model | blockchain anchor + CA-issued tokens |")
	w("| Authenticated data structure | MK-Tree + ancestor certificates + DHT |")
	w("| Proof reuse | yes — ancestor certificate reuse in batch verification |")
	w("| On-chain footprint | root-of-roots per epoch (one bytes32) |")
	w("")
	w("## Baselines (R-EVAL-10, R-EVAL-11, Q31)")
	w("")
	w("**Schemes [8], [12], [16] and [26] are NOT implemented and NOT reproduced.**")
	w("DELIA and BLAS are out of scope (§2.3). The figures below are reproduced here")
	w("solely as **values reported by the original authors** in the paper under")
	w("reproduction; nothing in this project measured them, and no comparison against")
	w("them is a testable claim.")
	w("")
	w("| Scheme | Single-record latency (s) | Throughput (logs/s) | Commit time (s) | Status |")
	w("|---|---|---|---|---|")
	w("| [12] | 0.0016 | 6410.25 | 0.0700 | reported by the original authors; NOT reproduced |")
	w("| [8] | 0.001984 | 1579.07 | 0.3530 | reported by the original authors; NOT reproduced |")
	w("| [26] | 0.015607 | 64.07 | 0.0906 | reported by the original authors; NOT reproduced |")
	w("| [16] | 0.2720 | 21052.63 | 1.2800 | reported by the original authors; NOT reproduced |")
	w("")

	if len(r.Notes) > 0 {
		w("## Recorded notes and failures")
		w("")
		for _, n := range r.Notes {
			w("- %s", n)
		}
		w("")
	}
	return b.String()
}

func relationLowerIsBetter(measured, reference float64) string {
	if measured <= reference {
		return fmt.Sprintf("measured %.2fx faster than the reference value", reference/measured)
	}
	return fmt.Sprintf("measured shortfall: %.2fx slower than the reference value", measured/reference)
}

func relationHigherIsBetter(measured, reference float64) string {
	if measured >= reference {
		return fmt.Sprintf("measured %.2fx higher than the reference value", measured/reference)
	}
	return fmt.Sprintf("measured shortfall: %.2fx lower than the reference value", reference/measured)
}

// verificationRequests is the number of token-gated DHT requests one
// single-record verification makes at the report's configuration: L - s
// sibling records, one ancestor certificate, and one Root Record per log type
// for anchor reconciliation (Q26). Each one re-checks the token's Ed25519
// signature (Q13).
func verificationRequests(r *Report) uint64 {
	return (levelsFor(r.EpochSize) - r.Stride) + 1 + uint64(len(mktree.DefaultConfig().LogTypes))
}
