#!/usr/bin/env bash
set -euo pipefail
export FAIR_CLOUD=1
. "$(dirname "${BASH_SOURCE[0]}")/../experiments/scripts/lib.sh"

scheme="$1"
trust="${FAIR_DASHBOARD_TRUST_DIR:-$SYSTEMS_DIR/demo-scripts-v2/site/.dashboard/trust}"
export DEMO_QUERY_REPEATS="$2"
printf 'DEMO_EVENT {"type":"scheme","scheme":"%s","message":"Received the shared verification batch"}\n' "$scheme"
if cmp -s "$DEMO_BATCH_IN" <(printf '[]'); then
    printf 'DEMO_EVENT {"type":"result","scheme":"%s","status":"VALID","message":"No records in the shared batch"}\n' "$scheme"
    exit 0
fi
case "$scheme" in
    DHT-backed)
        module=dht-backed
        package=./benchmark
        state="$trust/dht-backed.json" ;;
    EVSEB)
        module=EVBES
        package=./internal/integrity
        state="$trust/evseb.anchor" ;;
    *) die "unknown verification recipient" ;;
esac
if [[ ! -s "$state" ]]; then
    printf 'DEMO_EVENT {"type":"result","scheme":"%s","status":"UNAVAILABLE","message":"Batch received. No trusted state was saved for this ingest. The next cloud fixture build can save it."}\n' "$scheme"
    exit 1
fi
apply_endpoints "$module"
export DEMO_TRUST_STATE="$state"
cd "$SYSTEMS_DIR/$module"
if ! go test -v -run '^TestCloudDemoSharedBatch$' -count=1 -timeout=0 "$package"; then
    printf 'DEMO_EVENT {"type":"result","scheme":"%s","status":"ERROR","message":"Verification or localization could not finish. See the activity log."}\n' "$scheme"
    exit 1
fi
