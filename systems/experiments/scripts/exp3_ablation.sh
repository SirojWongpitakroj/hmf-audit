#!/usr/bin/env bash
# Experiment 3: HMF-Audit component ablation. Four variants (Full, no shared
# planning, no deduplication, no partition-aware retrieval) run over the same
# persisted fixture and the same requests, with the variant order rotated each
# round. --warmup warm-up rounds and --count measured rounds per variant and
# point. Raw CSV, summary CSV and the figure are written to
# <output>/exp3_ablation/.
#
#   exp3_ablation.sh [options]         (see --help)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

begin_experiment exp3_ablation "$@"
use_native_queries   # the ablation runs the native HMF-Audit queries, so the ALL tree must index the real attributes

write_environment exp3_ablation "hmf-audit" "$RESULTS"
note "ablation: $WARMUP warm-up + $COUNT measured rounds per variant (q = 256, 1024, 4096)"
set +e
(apply_endpoints hmf-audit; cd "$SYSTEMS_DIR/hmf-audit" && FAIR_ABLATION=1 FAIR_ABLATION_OUTPUT_DIR="$(winpath "$RESULTS")" \
    FAIR_ABLATION_WARMUP="$WARMUP" FAIR_ABLATION_MEASURED="$COUNT" \
    go test ./benchmarks/latency -run '^TestFairHMFAblation$' -v -count 1 -timeout 0) 2>&1 | tee "$RESULTS/ablation_run.log"
status=${PIPESTATUS[0]}
set -e
[[ $status -eq 0 ]] || die "the ablation failed (exit $status); see $RESULTS/ablation_run.log"

"$PYTHON" "$POSTPROCESS" exp3 --results-dir "$(winpath "$RESULTS")" --count "$COUNT"
note "experiment 3 finished: $RESULTS"
