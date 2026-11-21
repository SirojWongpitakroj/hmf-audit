#!/usr/bin/env bash
# Experiment 2: tamper detection and localization latency of HMF-Audit and
# EVSEB on the native query set (native-queries/README.md). Each batch is an HMF-Audit
# query of R0_queries.json (clustered and scattered, up to five per q = 1..4096); EVSEB
# is given the same records. In both, the middle record of the batch (index q/2 of the
# ascending ordinal list) is tampered in PostgreSQL. Raw output, trial CSV, summary CSV
# and Figure 2 are written to <output>/exp2_localization/.
#
#   exp2_localization.sh [options]     (see --help)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# --q-list 1,16,64,256,1024,4096: time only these q values; the post-processing then expects
# exactly those (FAIR_Q_LIST). The default is every q of the query set.
Q_LIST=""
common=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --q-list) Q_LIST="$2"; shift 2 ;;
    *) common+=("$1"); shift ;;
    esac
done
[[ -z "$Q_LIST" || "$Q_LIST" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || die "--q-list must be a comma-separated list of positive integers"

begin_experiment exp2_localization ${common[@]+"${common[@]}"}
use_native_queries

hmf_pattern='^BenchmarkFairHMFLocalizationAfterMismatch$'
evseb_pattern='^BenchmarkFairEVBESLocalizationAfterMismatch$'
if [[ -n "$Q_LIST" ]]; then
    # Go splits -bench at '/' into one regular expression per level of the benchmark name
    # (shape / q / set).
    q_level="^q=(${Q_LIST//,/|})\$"
    hmf_pattern="${hmf_pattern%\$}\$/^(clustered|scattered)\$/$q_level/^set=[0-9]+\$"
    evseb_pattern="${evseb_pattern%\$}\$/^(clustered|scattered)\$/$q_level/^set=[0-9]+\$"
    FAIR_Q_LIST="$Q_LIST"
    export FAIR_Q_LIST
    note "experiment 2 limited to q = $Q_LIST"
fi

write_environment exp2_localization "hmf-audit,evseb" "$RESULTS"
run_benchmark hmf-audit ./benchmarks/latency "$hmf_pattern" "$RESULTS/hmf-audit.txt"
run_benchmark EVBES ./internal/integrity "$evseb_pattern" "$RESULTS/evseb.txt"

"$PYTHON" "$POSTPROCESS" exp2 --results-dir "$(winpath "$RESULTS")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME"
note "experiment 2 finished: $RESULTS"
