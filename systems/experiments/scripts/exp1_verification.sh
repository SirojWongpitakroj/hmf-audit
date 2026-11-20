#!/usr/bin/env bash
# Experiment 1: verification latency of HMF-Audit, DHT-backed, BLAS and EVSEB
# on clustered-2 and scattered requests of q = 1..4096 records, over honest
# storage. Raw `go test -bench` output, trial CSV, summary CSV and Figure 1
# are written to <output>/exp1_verification/.
#
#   exp1_verification.sh [options]     (see --help)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

# --q-list 4096,1024: time only these q values (a partial run: raw results only, no figures).
Q_LIST=""
common=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --q-list) Q_LIST="$2"; shift 2 ;;
    *) common+=("$1"); shift ;;
    esac
done
[[ -z "$Q_LIST" || "$Q_LIST" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || die "--q-list must be a comma-separated list of positive integers"

begin_experiment exp1_verification ${common[@]+"${common[@]}"}

declare -A MODULE=([hmf-audit]=hmf-audit [dht-backed]=dht-backed [blas]=blas [evseb]=EVBES)
declare -A PACKAGE=([hmf-audit]=./benchmarks/latency [dht-backed]=./benchmark [blas]=./eval [evseb]=./internal/integrity)
declare -A PATTERN=(
    [hmf-audit]='^BenchmarkFairHMFVerificationCore$'
    [dht-backed]='^BenchmarkFairDHTVerificationRequest$'
    [blas]='^BenchmarkFairBLASVerificationRequest$'
    [evseb]='^BenchmarkFairEVBESVerificationRequest$'
)

use_native_queries
write_environment exp1_verification "$SYSTEMS" "$RESULTS"
IFS=',' read -r -a selected <<<"$SYSTEMS"
for system in "${selected[@]}"; do
    [[ -n "${MODULE[$system]:-}" ]] || die "unknown system '$system'"
    pattern="${PATTERN[$system]}"
    if [[ -n "$Q_LIST" ]]; then
        # Go splits -bench at '/' into one regular expression per level of the benchmark name.
        pattern="${pattern%\$}\$/^(clustered|scattered)\$/^q=(${Q_LIST//,/|})\$/^set=[0-9]+\$"
    fi
    run_benchmark "${MODULE[$system]}" "${PACKAGE[$system]}" "$pattern" "$RESULTS/$system.txt"
done

if [[ -n "$Q_LIST" ]]; then
    note "partial experiment 1 (q = $Q_LIST) finished: raw results in $RESULTS (no summary or figure for a partial run)"
    exit 0
fi

"$PYTHON" "$POSTPROCESS" exp1 --results-dir "$(winpath "$RESULTS")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME" --systems "$SYSTEMS"
note "experiment 1 finished: $RESULTS"
