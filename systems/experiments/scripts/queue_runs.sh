#!/usr/bin/env bash
# Run the queued experiments one after another, starting when the process given as the first
# argument (the running shard sweep) has ended. Never two at once: they share the database VMs
# and the runner's CPU, and a timed run must not compete with another job.
#
#   queue_runs.sh <pid to wait for>
#
# Jobs, in order (all on the skewed 10M dataset, query set native-queries/10000000-skewed):
#   1. DHT-backed, q = 4096 only, 3 warm-up + 10 measured runs
#   2. EVSEB,      q = 4096 only, 3 warm-up + 10 measured runs
#   3. BLAS,       every q (1..4096), 3 warm-up + 20 measured runs
#   4. HMF-Audit,  every q (1..4096), 3 warm-up + 20 measured runs, with the shard count in
#                  results/queue-hmf-shards.txt (read when the job starts; default 4)
#   5. Experiment 2 (tamper localization, HMF-Audit and EVSEB, clustered-2 and scattered),
#                  q = 1, 16, 64, 256, 1024, 4096, 3 warm-up + 20 measured runs, same shard count
#   6. Experiment 3 (HMF-Audit ablation, native queries q = 256, 1024, 4096),
#                  3 warm-up + 20 measured rounds per variant, same shard count
# A failed job is logged and the next one still runs. Each job writes its own log and results folder;
# the queue log is results/queue-<time>.log.
set -uo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
systems="$(cd "$here/../.." && pwd)"
results="$systems/experiments/results"
dataset="$systems/fair-results-10m-skewed/spark_skewed_stratified_10000000.jsonl"
queue_log="$results/queue-$(date +%Y%m%d-%H%M%S).log"
log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*" | tee -a "$queue_log"; }

export FAIR_CLOUD=1 FAIR_SERVICES_READY=1 FAIR_QUERY_SET=10000000-skewed FAIR_DHT_MAX_Q=4096
export PYTHON="${PYTHON:-$systems/../.venv/bin/python}"

if [[ -n "${1:-}" ]]; then
    log "waiting for process $1 (the shard sweep) to end"
    while kill -0 "$1" 2>/dev/null; do sleep 30; done
    log "process $1 ended"
fi

job() {
    local name="$1"
    shift
    log "start job $name: $*"
    (cd "$here" && "$@") >"$results/queue-job-$name.log" 2>&1
    log "job $name exit status $?"
}

job dht-4096 bash ./exp1_verification.sh --systems dht-backed --q-list 4096 --count 10 --warmup 3 \
    --dataset "$dataset" --output "$results/spark-10000000-skewed-dht-4096"
job evseb-4096 bash ./exp1_verification.sh --systems evseb --q-list 4096 --count 10 --warmup 3 \
    --dataset "$dataset" --output "$results/spark-10000000-skewed-evseb-4096"

# BLAS needs its schema on 10.148.0.5; without it the job would fail at once, so say so plainly.
if bash "$systems/cloud/check.sh" pre 2>&1 | grep -q 'BLAS PostgreSQL lacks schema blas'; then
    log "BLOCKED job blas: the BLAS PostgreSQL VM has no schema 'blas' (apply cloud/schema/postgres-blas.sql, then rerun this job)"
else
    job blas bash ./exp1_verification.sh --systems blas --count 20 --warmup 3 \
        --dataset "$dataset" --output "$results/spark-10000000-skewed-blas"
fi

shards="$(cat "$results/queue-hmf-shards.txt" 2>/dev/null || echo 4)"
log "HMF-Audit job uses $shards shard(s) per region"
FAIR_SHARDS_PER_REGION="$shards" job hmf-audit bash ./exp1_verification.sh --systems hmf-audit --count 20 --warmup 3 \
    --dataset "$dataset" --output "$results/spark-10000000-skewed-hmf"
FAIR_SHARDS_PER_REGION="$shards" job exp2 bash ./exp2_localization.sh --q-list 1,16,64,256,1024,4096 \
    --count 20 --warmup 3 --dataset "$dataset" --output "$results/spark-10000000-skewed-exp2"
FAIR_SHARDS_PER_REGION="$shards" job exp3 bash ./exp3_ablation.sh --count 20 --warmup 3 \
    --dataset "$dataset" --output "$results/spark-10000000-skewed-exp3"
log "queue finished"
