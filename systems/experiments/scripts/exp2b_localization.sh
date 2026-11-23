#!/usr/bin/env bash
# Experiment 2B: ACHP corruption-density and locality evaluation. At a fixed audit size of
# q = 4096, m = 1, 4, 16, 64 requested records are corrupted, clustered (one segment) or
# scattered (m distinct segments), and post-mismatch localization is timed for ACHP and for
# the fixed-depth strategy on exactly the same corruption. Five fixed query sets, 3 warm-up
# and 20 measured repetitions per query set and configuration (100 per configuration).
#
# Manifests, raw rows, summary and run configuration are written to
# <output>/exp2_localization/exp2b/{manifests,results}/. Experiment 2 (2A) writes to
# <output>/exp2_localization/ directly and is not touched.
#
#   exp2b_localization.sh --dry-run [--output DIR]     manifests at full scale + an in-memory smoke run;
#                                                        no dataset, database or cloud needed
#   exp2b_localization.sh [options]                     the real measurement (cloud: FAIR_CLOUD=1)
#
# extra option:  --seed N   master seed of the manifests (default 20261123). Manifests already in
#                the output directory are reused, never silently redrawn.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

DRY_RUN=0
SEED=""
common=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --dry-run) DRY_RUN=1; shift ;;
    --seed) SEED="$2"; shift 2 ;;
    --count | --warmup | --bench-time) die "Experiment 2B fixes its protocol (3 warm-ups, 20 repetitions); $1 does not apply" ;;
    *) common+=("$1"); shift ;;
    esac
done
[[ -z "$SEED" || "$SEED" =~ ^[0-9]+$ ]] || die "--seed must be a non-negative integer"

# FAIR_HMF_AUDIT_DIR points at another checkout of the HMF-Audit repository (for example a worktree).
HMF_AUDIT_DIR="${FAIR_HMF_AUDIT_DIR:-$SYSTEMS_DIR/hmf-audit}"
[[ -f "$HMF_AUDIT_DIR/benchmarks/exp2b/run.go" ]] \
    || die "$HMF_AUDIT_DIR has no benchmarks/exp2b: check out the HMF-Audit commit that contains Experiment 2B (native-queries-exp1)"
git_commit="$(git -C "$HMF_AUDIT_DIR" rev-parse HEAD 2>/dev/null || echo unknown)"
export FAIR_GIT_COMMIT="$git_commit"
if [[ -n "$SEED" ]]; then export FAIR_EXP2B_SEED="$SEED"; fi

if [[ "$DRY_RUN" == 1 ]]; then
    # No dataset hashing and no services: the dry run only needs the layout geometry.
    parse_args ${common[@]+"${common[@]}"}
    command -v go >/dev/null || die "go is not installed"
    DRY_DIR="$OUTPUT/exp2_localization/exp2b-dry-run"
    mkdir -p "$DRY_DIR"
    note "Experiment 2B dry run -> $DRY_DIR (layout: $REGIONS regions x $PER_REGION records, $SHARDS_PER_REGION shards, $SEGMENT_LEAVES-leaf segments)"
    (cd "$HMF_AUDIT_DIR" && CGO_ENABLED=0 FAIR_EXP2B_DRY_RUN=1 FAIR_EXP2B_OUT="$(winpath "$DRY_DIR")" \
        FAIR_EXP2B_REGIONS="$(printf '%s,' $(for _ in $(seq "$REGIONS"); do echo "$PER_REGION"; done) | sed 's/,$//')" \
        FAIR_HMF_SHARDS_PER_REGION="$SHARDS_PER_REGION" FAIR_HMF_SEGMENT_LEAVES="$SEGMENT_LEAVES" \
        go test ./benchmarks/latency -run '^TestExp2BDryRun$' -v -count 1 -timeout 0) 2>&1 | tee "$DRY_DIR/dry_run.log"
    [[ ${PIPESTATUS[0]} -eq 0 ]] || die "the dry run failed; see $DRY_DIR/dry_run.log"
    note "dry run finished: $DRY_DIR"
    exit 0
fi

begin_experiment exp2_localization ${common[@]+"${common[@]}"}
EXP2B_DIR="$RESULTS/exp2b"
mkdir -p "$EXP2B_DIR/manifests" "$EXP2B_DIR/results"

write_environment exp2_localization "hmf-audit" "$EXP2B_DIR/results"
note "experiment 2B: q=4096, m in {1,4,16,64}, clustered/scattered, 5 query sets, 3 warm-up + 20 measured per set (HMF-Audit commit $git_commit)"
set +e
(apply_endpoints hmf-audit; cd "$HMF_AUDIT_DIR" && FAIR_EXP2B=1 FAIR_EXP2B_OUT="$(winpath "$EXP2B_DIR")" \
\
    go test ./benchmarks/latency -run '^TestExp2BCloud$' -v -count 1 -timeout 0) 2>&1 | tee "$EXP2B_DIR/results/exp2b_run.log"
status=${PIPESTATUS[0]}
set -e
[[ $status -eq 0 ]] || die "experiment 2B failed (exit $status); see $EXP2B_DIR/results/exp2b_run.log (rows measured so far are in raw.csv)"
for file in manifests/queries.json manifests/corruption.json results/raw.csv results/summary.csv results/run_config.json; do
    [[ -s "$EXP2B_DIR/$file" ]] || die "experiment 2B did not produce $file"
done
note "experiment 2B finished: $EXP2B_DIR (no figure is generated)"
