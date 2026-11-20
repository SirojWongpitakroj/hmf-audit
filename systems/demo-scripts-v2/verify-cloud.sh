#!/usr/bin/env bash
set -euo pipefail
export FAIR_CLOUD=1
. "$(dirname "${BASH_SOURCE[0]}")/../experiments/scripts/lib.sh"

[[ $# == 7 ]] || die "expected tenant, service, log type, region, start, end and repeats"
export DEMO_QUERY_TENANT="$1" DEMO_QUERY_SERVICE="$2" DEMO_QUERY_TYPE="$3"
export DEMO_QUERY_REGION="$4" DEMO_QUERY_START="$5" DEMO_QUERY_END="$6" DEMO_QUERY_REPEATS="$7"
export DEMO_CLOUD_VERIFY=1 FAIR_DATASET_PATH="$EXISTING_10M_FILE"
apply_endpoints hmf-audit
cd "$SYSTEMS_DIR/hmf-audit"
exec go test -v -run '^TestCloudDemoVerification$' -count=1 -timeout=0 ./benchmarks/latency
