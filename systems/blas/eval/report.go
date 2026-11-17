package eval

import (
	"fmt"
	"strings"
)

// FormatReportTable renders a set of AggregateReport values in the same
// row-per-scenario layout as Tables II–VI (R-EVAL-019): one row per query
// type/scenario, columns for the measured metrics. This does NOT attempt a
// literal multi-block-count sweep matching Tables II/III's column layout
// (128/512/1024/4096/8192 blocks) — the real dataset available in this
// environment (see testdata/README.md) is a bounded sample, not the
// paper-scale corpus those block counts assume, and printing paper numbers
// next to results from a dataset two orders of magnitude smaller would
// invite exactly the misread-as-a-comparison risk already flagged in
// TRACKER.md's M12 notes. This renders whatever reports are actually run,
// at whatever scale was actually used, with H3's own bar (correctness,
// trend/scaling agreement) in mind rather than a numeric-parity claim.
func FormatReportTable(reports []AggregateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %8s %14s %14s %12s %10s\n",
		"Scenario", "Blocks", "AvgProcess", "AvgVerify", "AvgVO(KB)", "Accepted")
	fmt.Fprintf(&b, "%-16s %8s %14s %14s %12s %10s\n",
		strings.Repeat("-", 16), strings.Repeat("-", 8), strings.Repeat("-", 14),
		strings.Repeat("-", 14), strings.Repeat("-", 12), strings.Repeat("-", 10))
	for _, r := range reports {
		fmt.Fprintf(&b, "%-16s %8d %14s %14s %12.3f %10v\n",
			r.QueryType.String(), r.NumBlocks, r.AvgProcessTime, r.AvgVerifyTime, r.AvgVOSizeKB, r.AcceptedAll)
	}
	return b.String()
}

// FormatAblationTable renders a Fig. 7-style ablation report: one row per
// variant, for one fixed query type/block count (same scope limitation as
// FormatReportTable — see its doc comment).
func FormatAblationTable(qt QueryType, reports map[AblationVariant]AggregateReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Ablation (%s):\n", qt)
	fmt.Fprintf(&b, "%-14s %8s %14s %12s %10s\n", "Variant", "Blocks", "AvgProcess", "AvgVO(KB)", "Accepted")
	for _, v := range []AblationVariant{AblationAll, AblationNoAggregate, AblationNoMultiWin} {
		r, ok := reports[v]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, "%-14s %8d %14s %12.3f %10v\n", string(v), r.NumBlocks, r.AvgProcessTime, r.AvgVOSizeKB, r.AcceptedAll)
	}
	return b.String()
}
