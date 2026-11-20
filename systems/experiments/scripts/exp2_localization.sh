#!/usr/bin/env bash
# Experiment 2: tamper detection and localization latency of HMF-Audit and
# EVSEB. One requested record (the middle one) is tampered in PostgreSQL;
# q = 1..4096 on clustered-2 and scattered requests. Raw output, trial CSV,
# summary CSV and Figure 2 are written to <output>/exp2_localization/.
#
#   exp2_localization.sh [options]     (see --help)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# --q-list 1,16,64,256,1024,4096: time only these q values. The default is every q of the fixed
# placements (the benchmarks go up to 16384); a list also caps what the post-processing expects.
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

hmf_pattern='^BenchmarkFairHMFLocalizationAfterMismatch$'
evseb_pattern='^BenchmarkFairEVBESLocalizationAfterMismatch$'
if [[ -n "$Q_LIST" ]]; then
    # Go splits -bench at '/' into one regular expression per level of the benchmark name
    # (placement / q / [tamper count]).
    q_level="^q=(${Q_LIST//,/|})\$"
    hmf_pattern="${hmf_pattern%\$}\$/^(clustered-2|scattered)\$/$q_level"
    evseb_pattern="${evseb_pattern%\$}\$/^(clustered-2|scattered)\$/$q_level"
    FAIR_MAX_REQUEST_Q="$(tr ',' '\n' <<<"$Q_LIST" | sort -n | tail -1)"
    export FAIR_MAX_REQUEST_Q
    note "experiment 2 limited to q = $Q_LIST"
fi

write_environment exp2_localization "hmf-audit,evseb" "$RESULTS"
run_benchmark hmf-audit ./benchmarks/latency "$hmf_pattern" "$RESULTS/hmf-audit.txt"
run_benchmark EVBES ./internal/integrity "$evseb_pattern" "$RESULTS/evseb.txt"

"$PYTHON" "$POSTPROCESS" exp2 --results-dir "$(winpath "$RESULTS")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME"
note "experiment 2 finished: $RESULTS"
