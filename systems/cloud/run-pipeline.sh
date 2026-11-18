#!/usr/bin/env bash
# Start the whole 10M pipeline on the cloud VMs, detached from your SSH session,
# so a dropped connection does not kill a run that takes more than a day.
#
#   bash systems/cloud/run-pipeline.sh                      # same options as run_pipeline.sh
#   bash systems/cloud/run-pipeline.sh --records 100000     # dry run on the 100k subset first
#
# Follow it with:  tail -f systems/experiments/results/pipeline-*.log
# (tmux works just as well: `FAIR_CLOUD=1 systems/experiments/scripts/run_pipeline.sh`.)
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
results="$here/../experiments/results"
mkdir -p "$results"

export FAIR_CLOUD=1
# Fail in the foreground, with a readable message, before detaching.
bash "$here/check.sh" pre

out="$results/cloud-run-$(date +%Y%m%d-%H%M%S).out"
nohup setsid "$here/../experiments/scripts/run_pipeline.sh" "$@" >"$out" 2>&1 </dev/null &
echo "started pid $!; console output: $out" >&2
echo "the run's own log: $results/pipeline-<records>-<timestamp>.log" >&2
