#!/usr/bin/env bash
# Runner: runs the three experiment scripts one after another against the
# local Docker stack, then (for the existing 10M subset) compares the new
# results with the earlier 10M results.
#
#   run_all.sh [options]               (see --help)
#
# Each experiment re-seeds its own fixtures, exactly as the original fair
# benchmark does. A failing experiment stops the run.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

parse_args "$@"
find_python
command -v go >/dev/null || die "go is not installed"
command -v docker >/dev/null || die "docker is not installed"
prepare_dataset
start_services

# Hand the verified dataset and the running services to the three scripts.
forward=(--count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME" --per-region "$PER_REGION"
    --dataset "$DATASET" --output "$OUTPUT")

started=$(date +%s)
for script in exp1_verification exp2_localization exp3_ablation; do
    note "=== $script ==="
    if [[ "$script" == exp1_verification ]]; then
        bash "$SCRIPTS_DIR/$script.sh" "${forward[@]}" --systems "$SYSTEMS"
    else
        bash "$SCRIPTS_DIR/$script.sh" "${forward[@]}"
    fi
done

if [[ "$COMPARE_PREVIOUS" == 1 && "$DATASET" == "$EXISTING_10M_FILE" && -f "$PREVIOUS_SUMMARY" ]]; then
    note "=== comparison with the earlier 10M results (HMF-Audit and EVSEB points) ==="
    for experiment in exp1_verification exp2_localization; do
        echo "--- $experiment"
        "$PYTHON" "$POSTPROCESS" compare --old "$(winpath "$PREVIOUS_SUMMARY")" \
            --new "$(winpath "$OUTPUT/$experiment/latency_summary.csv")" || true
    done
fi
note "all three experiments finished in $(( $(date +%s) - started )) s; results in $OUTPUT"
