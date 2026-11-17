#!/usr/bin/env bash
# Experiment 2: tamper detection and localization latency of HMF-Audit and
# EVSEB. One requested record (the middle one) is tampered in PostgreSQL;
# q = 1..16384 on clustered-2 and scattered requests. Raw output, trial CSV,
# summary CSV and Figure 2 are written to <output>/exp2_localization/.
#
#   exp2_localization.sh [options]     (see --help)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

begin_experiment exp2_localization "$@"

write_environment exp2_localization "hmf-audit,evseb" "$RESULTS"
run_benchmark hmf-audit ./benchmarks/latency '^BenchmarkFairHMFLocalizationAfterMismatch$' "$RESULTS/hmf-audit.txt"
run_benchmark EVBES ./internal/integrity '^BenchmarkFairEVBESLocalizationAfterMismatch$' "$RESULTS/evseb.txt"

"$PYTHON" "$POSTPROCESS" exp2 --results-dir "$(winpath "$RESULTS")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME"
note "experiment 2 finished: $RESULTS"
