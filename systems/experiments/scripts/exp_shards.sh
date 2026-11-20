#!/usr/bin/env bash
# Shard-count study for HMF-Audit, real implementation: for every shard count S the HMF
# fixture is seeded with S shards per region (Cassandra + PostgreSQL, truncate and reload)
# and the native verification benchmark is timed on the cells chosen with --q-list. Raw
# `go test -bench` output goes to <output>/exp_shards/shards-<S>/hmf-audit.txt; the summary
# (p50 per cell and S, best S, geometric-mean ratio) to <output>/exp_shards/.
#
#   exp_shards.sh [--shards 1,2,4,8] [--q-list 64,256,1024,4096] [common options]
#
# Common options are those of the other experiments (--dataset, --output, --count, --warmup,
# --bench-time, --per-region; see lib.sh). Needs the native query set of the dataset (FAIR_QUERY_SET
# names its directory under native-queries/ when it is not the default <records>).
#
# Each S re-seeds the fixture, so a sweep costs (setup + run) x the number of shard counts: at 10M
# records the setup alone was about 35 minutes. Run the cheap structural study first:
#   FAIR_QUERY_DIR=... go test ./benchmarks/latency -run TestFairHMFShardPlan -v   (in systems/hmf-audit)
# The order in which the shard counts run can bias the result (cache state of the database VMs). If two
# shard counts are close, repeat the sweep with the list reversed into another --output and give both
# folders to shard_sweep_summary.py.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SHARD_LIST=1,2,4,8
Q_LIST=64,256,1024,4096
common=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --shards) SHARD_LIST="$2"; shift 2 ;;
    --q-list) Q_LIST="$2"; shift 2 ;;
    *) common+=("$1"); shift ;;
    esac
done
[[ "$SHARD_LIST" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || die "--shards must be a comma-separated list of positive integers"
[[ "$Q_LIST" =~ ^[1-9][0-9]*(,[1-9][0-9]*)*$ ]] || die "--q-list must be a comma-separated list of positive integers"

begin_experiment exp_shards ${common[@]+"${common[@]}"}

# Go splits -bench at '/' into one regular expression per level of the benchmark name.
pattern="^BenchmarkFairHMFVerificationCore\$/^(clustered|scattered)\$/^q=(${Q_LIST//,/|})\$/^set=[0-9]+\$"
IFS=',' read -r -a shard_counts <<<"$SHARD_LIST"
for shards in "${shard_counts[@]}"; do
    SHARDS_PER_REGION="$shards"
    export_fair_env              # exports FAIR_HMF_SHARDS_PER_REGION from SHARDS_PER_REGION (and clears FAIR_QUERY_DIR)
    use_native_queries
    dir="$RESULTS/shards-$shards"
    mkdir -p "$dir"
    note "shard study: $shards shard(s) per region"
    write_environment exp_shards hmf-audit "$dir"
    run_benchmark hmf-audit ./benchmarks/latency "$pattern" "$dir/hmf-audit.txt"
done

"$PYTHON" "$EXP_DIR/shard_sweep_summary.py" "$(winpath "$RESULTS")" || note "summary failed; the raw results are in $RESULTS"
note "shard study finished: $RESULTS"
