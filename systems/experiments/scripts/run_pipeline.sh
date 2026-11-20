#!/usr/bin/env bash
# One command for the whole 10M-record study, from the public logs to the figures:
#
#   1. preflight   - tools, Python + matplotlib, Docker (fail now, not after a day)
#   2. dataset     - download the Loghub Spark logs (33,236,604 lines) and deterministically
#                    derive the 10,000,000-record subset (dataset/scripts/prepare_spark_10m.py).
#                    Already-present, verified data is not downloaded or rebuilt again.
#                    With --records 100000 the combined file is prepared and the 100k subset
#                    is cut from it by the experiment scripts.
#   3. ingest      - load the subset into PostgreSQL + Cassandra (ingest.sh)
#   4. experiments - exp1 verification, exp2 localization, exp3 ablation (run_all.sh)
#   5. figures     - re-plot Figures 1-3 with Python from the raw results, check that all
#                    three PNGs exist and copy them to <output>/figures/
#
#   run_pipeline.sh [--records 10000000|100000] [--systems LIST] [--count N]
#                   [--warmup N] [--bench-time T] [--skip-ingest] [--no-compare]
#
# --records defaults to 10000000. 100000 runs the identical pipeline on the
# deterministic 100k subset (a dry run before committing to the 10M run).
#
# The default systems for experiment 1 are hmf-audit,blas,evseb: dht-backed is
# rejected by its own memory bounds at q = 4096 (see README.md, "Known
# limitations"), which would stop the run. Add it back with
# --systems hmf-audit,dht-backed,blas,evseb if that has been resolved.
#
# NOTE: the experiment scripts re-seed the databases when each one starts (the
# fixtures truncate first), so step 3 is repeated work at 10M. It is on by
# default because it was asked for; --skip-ingest drops it and the results are
# identical.
#
# Output: systems/experiments/results/spark-<records>/ and a log of this run in
# systems/experiments/results/pipeline-<records>-<timestamp>.log
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

SKIP_INGEST=0
records=10000000
pipeline_systems="hmf-audit,dht-backed,blas,evseb"
forward=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --records) [[ $# -ge 2 ]] || die "--records needs a value"; records="$2"; shift 2 ;;
    --skip-ingest) SKIP_INGEST=1; shift ;;
    --no-compare) forward+=(--no-compare); shift ;;
    --systems) [[ $# -ge 2 ]] || die "--systems needs a value"; pipeline_systems="$2"; shift 2 ;;
    --count | --warmup | --bench-time)
        [[ $# -ge 2 ]] || die "$1 needs a value"; forward+=("$1" "$2"); shift 2 ;;
    -h | --help)
        sed -n '2,/^set -euo/p' "${BASH_SOURCE[0]}" | sed '$d' | sed 's/^# \{0,1\}//' >&2
        exit 0 ;;
    *) die "unknown option $1 (see --help)" ;;
    esac
done

# Exact decimal text only (1e5, 100,000, 10M are rejected), as in ingest.sh.
[[ "$records" == 10000000 || "$records" == 100000 ]] || die "--records must be 10000000 or 100000, got '$records'"
forward+=(--per-region $((records / REGIONS)))

# Settings for the final plotting step (also validates the numbers).
parse_args --systems "$pipeline_systems" ${forward[@]+"${forward[@]}"}
(( RECORDS == records )) || die "internal: expected $records records, got $RECORDS"

mkdir -p "$EXP_DIR/results"
LOG="$EXP_DIR/results/pipeline-${records}-$(date +%Y%m%d-%H%M%S).log"
exec > >(tee -a "$LOG") 2>&1
note "log of this run: $LOG"
started=$(date +%s)

# ---------------------------------------------------------------- 1. preflight
note "=== 1/5 preflight ==="
find_python
"$PYTHON" -c 'import matplotlib' >/dev/null 2>&1 \
    || die "python module 'matplotlib' is missing; install it first ($PYTHON -m pip install matplotlib) - the figures are the last step of a very long run"
command -v go >/dev/null || die "go is not installed"
command -v docker >/dev/null || die "docker is not installed"
docker info >/dev/null 2>&1 || die "the docker daemon is not running"
if (( records == 10000000 )); then
    note "this run needs roughly 35 GB free for Docker plus about 25 GB for the dataset (README.md, 'Cost') and takes well over a day"
fi

# ------------------------------------------------------------------ 2. dataset
# Download Loghub Spark and derive the subset. For 10M the result is the exact
# file the pipeline expects (SHA-256 pinned); for 100k only the combined file is
# prepared and prepare_dataset cuts the 100k subset from it.
if (( records == 10000000 )); then stop_after=subset; else stop_after=combined; fi
note "=== 2/5 dataset: download Loghub Spark, derive the subset ($stop_after) ==="
"$PYTHON" "$(winpath "$REPO_DIR/dataset/scripts/prepare_spark_10m.py")" --stop-after "$stop_after"
prepare_dataset          # file exists, has the expected records, SHA-256 matches (10M)

# ------------------------------------------------------------------ 3. ingest
if [[ $SKIP_INGEST -eq 1 ]]; then
    note "=== 3/5 ingest skipped (--skip-ingest); the experiments seed the data themselves ==="
else
    note "=== 3/5 ingest $records records ==="
    bash "$SCRIPTS_DIR/ingest.sh" --records "$records" --systems "$pipeline_systems"
fi

# ------------------------------------------------------------- 4. experiments
note "=== 4/5 experiments 1, 2, 3 ==="
bash "$SCRIPTS_DIR/run_all.sh" --systems "$pipeline_systems" ${forward[@]+"${forward[@]}"}

# ------------------------------------------------------------------ 5. figures
note "=== 5/5 figures ==="
R1="$OUTPUT/exp1_verification"
R2="$OUTPUT/exp2_localization"
R3="$OUTPUT/exp3_ablation"
"$PYTHON" "$POSTPROCESS" exp1 --results-dir "$(winpath "$R1")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME" --systems "$pipeline_systems" \
    --query-dir "$(winpath "$EXP_DIR/native-queries/$records")" --dht-max-q "${FAIR_DHT_MAX_Q:-4096}"
"$PYTHON" "$POSTPROCESS" exp2 --results-dir "$(winpath "$R2")" --dataset "$(winpath "$DATASET")" \
    --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME"
"$PYTHON" "$POSTPROCESS" exp3 --results-dir "$(winpath "$R3")" --count "$COUNT"

mkdir -p "$OUTPUT/figures"
for figure in "$R1/figure1_verification_latency.png" "$R2/figure2_localization_latency.png" \
    "$R3/figure3_ablation_latency.png"; do
    [[ -s "$figure" ]] || die "expected figure was not produced: $figure"
    cp -f "$figure" "$OUTPUT/figures/"
done

note "pipeline finished in $(( ($(date +%s) - started) / 60 )) min"
note "figures: $OUTPUT/figures/"
ls -l "$OUTPUT/figures" >&2
