#!/usr/bin/env bash
# Reruns jobs of queue_runs.sh that failed, after the whole queue has ended:
#
#   queue_followup.sh <pid of queue_runs.sh>
#
# EVSEB q = 4096 (3 warm-up + 10 measured runs) failed in its first attempt on a Cassandra write
# timeout while seeding; the seeding writes now retry transient timeouts. The rerun writes to a new
# folder, so the failed attempt's output is kept.
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
systems="$(cd "$here/../.." && pwd)"
results="$systems/experiments/results"
dataset="$systems/fair-results-10m-skewed/spark_skewed_stratified_10000000.jsonl"
log="$results/queue-followup-$(date +%Y%m%d-%H%M%S).log"
say() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$log"; }
export FAIR_CLOUD=1 FAIR_SERVICES_READY=1 FAIR_QUERY_SET=10000000-skewed FAIR_DHT_MAX_Q=4096
export PYTHON="${PYTHON:-$systems/../.venv/bin/python}"
if [[ -n "${1:-}" ]]; then
    say "waiting for process $1 (the main queue) to end"
    while kill -0 "$1" 2>/dev/null; do sleep 30; done
fi
say "start job evseb-4096-r2"
(cd "$here" && bash ./exp1_verification.sh --systems evseb --q-list 4096 --count 10 --warmup 3 \
    --dataset "$dataset" --output "$results/spark-10000000-skewed-evseb-4096-r2") >"$results/queue-job-evseb-4096-r2.log" 2>&1
say "job evseb-4096-r2 exit status $?"
