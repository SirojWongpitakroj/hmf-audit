#!/usr/bin/env bash
# Experiment 1: verification latency of HMF-Audit, DHT-backed, BLAS and EVSEB
# on clustered-2 and scattered requests of q = 1..16384 records, over honest
# storage. Raw `go test -bench` output, trial CSV, summary CSV and Figure 1
# are written to <output>/exp1_verification/.
#
#   exp1_verification.sh [options]     (see --help)
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

begin_experiment exp1_verification "$@"

declare -A MODULE=([hmf-audit]=hmf-audit [dht-backed]=dht-backed [blas]=blas [evseb]=EVBES)
declare -A PACKAGE=([hmf-audit]=./benchmarks/latency [dht-backed]=./benchmark [blas]=./eval [evseb]=./internal/integrity)
declare -A PATTERN=(
    [hmf-audit]='^BenchmarkFairHMFVerificationCore$'
    [dht-backed]='^BenchmarkFairDHTVerificationRequest$'
    [blas]='^BenchmarkFairBLASVerificationRequest$'
    [evseb]='^BenchmarkFairEVBESVerificationRequest$'
)

write_environment exp1_verification "$SYSTEMS" "$RESULTS"
IFS=',' read -r -a selected <<<"$SYSTEMS"
for system in "${selected[@]}"; do
    [[ -n "${MODULE[$system]:-}" ]] || die "unknown system '$system'"
    run_benchmark "${MODULE[$system]}" "${PACKAGE[$system]}" "${PATTERN[$system]}" "$RESULTS/$system.txt"
done

"$PYTHON" "$POSTPROCESS" exp1 --results-dir "$(winpath "$RESULTS")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME" --systems "$SYSTEMS"
note "experiment 1 finished: $RESULTS"
