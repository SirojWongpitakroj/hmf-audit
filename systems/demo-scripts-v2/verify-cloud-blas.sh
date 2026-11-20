#!/usr/bin/env bash
set -euo pipefail
export FAIR_CLOUD=1
. "$(dirname "${BASH_SOURCE[0]}")/../experiments/scripts/lib.sh"

unset DEMO_BLAS_SHAPE
case $# in
    4)
        export DEMO_BLAS_SHAPE="$1" DEMO_BLAS_Q="$2" DEMO_BLAS_SET="$3" DEMO_QUERY_REPEATS="$4" ;;
    7)
        export DEMO_QUERY_TENANT="$1" DEMO_QUERY_SERVICE="$2" DEMO_QUERY_TYPE="$3"
        export DEMO_QUERY_REGION="$4" DEMO_QUERY_START="$5" DEMO_QUERY_END="$6" DEMO_QUERY_REPEATS="$7" ;;
    *) die "expected a BLAS native case or attribute query" ;;
esac
export DEMO_CLOUD_BLAS=1 FAIR_DATASET_PATH="$EXISTING_10M_FILE"
export FAIR_REGION0_RECORDS="$DEFAULT_PER_REGION" FAIR_REGION1_RECORDS="$DEFAULT_PER_REGION"
export FAIR_NATIVE_QUERIES_DIR="$EXP_DIR/native-queries"
if [[ -z "${DEMO_BATCH_IN:-}" ]]; then
    printf '%s  %s\n' "$EXPECTED_10M_SHA256" "$FAIR_DATASET_PATH" | sha256sum --check --status ||
        die "the source does not match the pinned 10-million-record cloud ingest"
fi
apply_endpoints blas
cd "$SYSTEMS_DIR/blas"
exec go test -v -run '^TestCloudDemoBLASVerification$' -count=1 -timeout=0 ./eval
