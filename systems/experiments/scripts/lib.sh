#!/usr/bin/env bash
# Shared helpers for the local experiment scripts. Source this file; do not run it.
#
# Everything here targets the local Docker stack in systems/fair-persistence
# (PostgreSQL on 127.0.0.1:55432, Cassandra on 127.0.0.1:19042). The endpoints
# are fixed on purpose: these scripts cannot be pointed at a remote database.

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXP_DIR="$(cd "$SCRIPTS_DIR/.." && pwd)"   # systems/experiments: results/, datasets/, postprocess.py, *.sql
SYSTEMS_DIR="$(cd "$EXP_DIR/.." && pwd)"
REPO_DIR="$(cd "$SYSTEMS_DIR/.." && pwd)"

# Git Bash on Windows rewrites arguments that look like POSIX paths (the
# container paths passed to docker exec); that must stay off.
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'

die() { echo "error: $*" >&2; exit 1; }
note() { echo "[$(date +%H:%M:%S)] $*" >&2; }

# Native path for Go, Docker and Python (C:/... on Windows, unchanged elsewhere).
winpath() {
    if command -v cygpath >/dev/null 2>&1; then cygpath -m "$1"; else printf '%s' "$1"; fi
}

# ------------------------------------------------------------ fixed protocol
REGIONS=4
SHARDS_PER_REGION=1
SEGMENT_LEAVES=8192
HPP_CONCURRENCY=8
EVSEB_DHT_CONCURRENCY=8
DHT_CONCURRENCY=8
DHT_PEERS=1
DEFAULT_PER_REGION=2500000
# SHA-256 of the existing 10,000,000-record subset used by the earlier results.
EXPECTED_10M_SHA256=a8fa53db3327ce5b21cc61b249276817d81b2ee29a6facf64f48619d215dba82
EXISTING_10M_FILE="$SYSTEMS_DIR/fair-results-10m-localization/spark_stratified_10000000.jsonl"
SOURCE_DATASET="$REPO_DIR/dataset/combined/spark_uniform_combined.jsonl"
PREVIOUS_SUMMARY="$SYSTEMS_DIR/fair-results-10m-localization/latency_summary.csv"
POSTPROCESS="$(winpath "$EXP_DIR/postprocess.py")"

# ------------------------------------------------------------------ settings
COUNT=30
WARMUP=5
BENCHTIME=250ms
PER_REGION=$DEFAULT_PER_REGION
DATASET=""
OUTPUT=""
SYSTEMS="hmf-audit,dht-backed,blas,evseb"
COMPARE_PREVIOUS=1

usage_common() {
    cat >&2 <<EOF
options:
  --count N         measured repetitions per case (default 30)
  --warmup N        warm-up operations per case / rounds (default 5)
  --bench-time T    go benchmark timing window (default 250ms)
  --per-region N    records per region in the subset (default 2500000 = the existing 10M file)
  --dataset FILE    subset file (default: the existing 10M file, or a generated one for other sizes)
  --output DIR      output directory (default: systems/experiments/results/spark-<records>)
  --systems LIST    experiment 1 only: comma list of hmf-audit,dht-backed,blas,evseb
  --no-compare      run_all only: skip the comparison with the earlier 10M results
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
        --count) COUNT="$2"; shift 2 ;;
        --warmup) WARMUP="$2"; shift 2 ;;
        --bench-time) BENCHTIME="$2"; shift 2 ;;
        --per-region) PER_REGION="$2"; shift 2 ;;
        --dataset) DATASET="$2"; shift 2 ;;
        --output) OUTPUT="$2"; shift 2 ;;
        --systems) SYSTEMS="$2"; shift 2 ;;
        --no-compare) COMPARE_PREVIOUS=0; shift ;;
        -h | --help) usage_common; exit 0 ;;
        *) usage_common; die "unknown option $1" ;;
        esac
    done
    [[ "$COUNT" =~ ^[1-9][0-9]*$ ]] || die "--count must be a positive integer"
    [[ "$WARMUP" =~ ^[0-9]+$ ]] || die "--warmup must be a non-negative integer"
    [[ "$PER_REGION" =~ ^[1-9][0-9]*$ ]] || die "--per-region must be a positive integer"
    RECORDS=$((PER_REGION * REGIONS))
    if [[ -z "$DATASET" ]]; then
        if [[ "$PER_REGION" == "$DEFAULT_PER_REGION" ]]; then
            DATASET="$EXISTING_10M_FILE"
        else
            DATASET="$EXP_DIR/datasets/spark_stratified_${RECORDS}.jsonl"
        fi
    fi
    [[ -n "$OUTPUT" ]] || OUTPUT="$EXP_DIR/results/spark-${RECORDS}"
    mkdir -p "$OUTPUT"
    OUTPUT="$(cd "$OUTPUT" && pwd)"
    # q = 2^14 needs two full 8,192-leaf segments in R0 and a scattered window
    # across R1..R3 wider than three segments.
    (( PER_REGION >= 16384 )) || die "--per-region must be at least 16384 (clustered-2 at q=16384 needs two full segments in R0)"
    (( PER_REGION * (REGIONS - 1) >= 3 * SEGMENT_LEAVES )) || die "subset too small for the scattered placement"
}

find_python() {
    local candidate
    for candidate in "${PYTHON:-}" python3 python; do
        [[ -n "$candidate" ]] || continue
        if "$candidate" -c 'import sys; sys.exit(0 if sys.version_info >= (3, 9) else 1)' >/dev/null 2>&1; then
            PYTHON="$candidate"
            return
        fi
    done
    die "python 3.9 or newer is required"
}

sha256_of() {
    if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | cut -d' ' -f1; else shasum -a 256 "$1" | cut -d' ' -f1; fi
}

# prepare_dataset: make sure the subset exists, has the expected records and,
# for the existing 10M file, the expected SHA-256. Sets DATASET_SHA256.
prepare_dataset() {
    if [[ ! -f "$DATASET" ]]; then
        [[ -f "$SOURCE_DATASET" ]] || die "dataset $DATASET is missing and the source $SOURCE_DATASET is not available to regenerate it"
        note "generating the deterministic subset ($PER_REGION per region) -> $DATASET"
        mkdir -p "$(dirname "$DATASET")"
        # Write to a temporary name and rename: an interrupted run must not leave
        # a partial file that later runs mistake for the subset.
        rm -f "$DATASET.partial"
        (cd "$REPO_DIR" && go run dataset/scripts/select_fair_subset.go -input "$(winpath "$SOURCE_DATASET")" \
            -output "$(winpath "$DATASET.partial")" -regions "$REGIONS" -per-region "$PER_REGION") >&2
        mv "$DATASET.partial" "$DATASET"
    fi
    if [[ "${FAIR_DATASET_FILE:-}" == "$DATASET" && -n "${FAIR_DATASET_SHA256:-}" ]]; then
        DATASET_SHA256="$FAIR_DATASET_SHA256"
    else
        note "hashing $DATASET"
        DATASET_SHA256="$(sha256_of "$DATASET")"
        local lines
        lines="$(wc -l <"$DATASET" | tr -d ' ')"
        [[ "$lines" == "$RECORDS" ]] || die "$DATASET has $lines records, expected $RECORDS"
    fi
    if [[ "$DATASET" == "$EXISTING_10M_FILE" && "$DATASET_SHA256" != "$EXPECTED_10M_SHA256" ]]; then
        die "the existing 10M file has sha256 $DATASET_SHA256, expected $EXPECTED_10M_SHA256; it is not the dataset the earlier results used"
    fi
    export FAIR_DATASET_FILE="$DATASET" FAIR_DATASET_SHA256="$DATASET_SHA256"
    note "dataset ok: $RECORDS records, sha256 $DATASET_SHA256"
}

compose() { docker compose -f "$(winpath "$SYSTEMS_DIR/fair-persistence/docker-compose.yml")" "$@"; }

# start_services: bring up the local PostgreSQL and Cassandra and create every
# schema and keyspace the four schemes need. Idempotent.
start_services() {
    if [[ "${FAIR_SERVICES_READY:-}" == 1 ]]; then return; fi
    note "starting the local PostgreSQL and Cassandra containers"
    compose up -d --wait postgres cassandra
    compose exec -T postgres psql -v ON_ERROR_STOP=1 -q -U fair -d fair_benchmark \
        -f /docker-entrypoint-initdb.d/001-fair-benchmark.sql
    compose exec -T postgres psql -v ON_ERROR_STOP=1 -q -U fair -d fair_benchmark <"$EXP_DIR/init_extra.sql"
    compose run --rm -T cassandra-init
    compose run --rm -T evseb-cassandra-init
    compose exec -T cassandra cqlsh <"$EXP_DIR/init_dht.cql"
    export FAIR_SERVICES_READY=1
}

# export_fair_env: the variables the (unchanged) fair fixtures read.
export_fair_env() {
    export CGO_ENABLED=0 GOMAXPROCS=1
    export FAIR_PERSISTENCE=1
    export FAIR_DATASET_PATH="$(winpath "$DATASET")"
    export FAIR_WARMUP="$WARMUP"
    export FAIR_POSTGRES_DSN="postgres://fair:fairdev@127.0.0.1:55432/fair_benchmark?sslmode=disable"
    export FAIR_DHT_POSTGRES_DSN="postgres://fair:fairdev@127.0.0.1:55432/fair_benchmark?sslmode=disable&search_path=dht"
    export FAIR_CASSANDRA_HOST=127.0.0.1 FAIR_CASSANDRA_PORT=19042
    export FAIR_HMF_SHARDS_PER_REGION="$SHARDS_PER_REGION" FAIR_HMF_SEGMENT_LEAVES="$SEGMENT_LEAVES"
    export FAIR_HMF_HPP_CONCURRENCY="$HPP_CONCURRENCY"
    export FAIR_REGION0_RECORDS="$PER_REGION" FAIR_REGION1_RECORDS="$PER_REGION"
    export FAIR_EVSEB_DHT_CONCURRENCY="$EVSEB_DHT_CONCURRENCY" FAIR_DHT_CONCURRENCY="$DHT_CONCURRENCY"
    export FAIR_DHT_PEERS="$DHT_PEERS"
}

# write_environment <experiment> <systems> <output dir>
write_environment() {
    "$PYTHON" "$POSTPROCESS" env --out "$(winpath "$3")" --experiment "$1" --systems "$2" \
        --dataset "$(winpath "$DATASET")" --dataset-sha256 "$DATASET_SHA256" \
        --source-dataset "$(winpath "$SOURCE_DATASET")" --per-region "$PER_REGION" --regions "$REGIONS" \
        --count "$COUNT" --warmup "$WARMUP" --bench-time "$BENCHTIME" \
        --shards-per-region "$SHARDS_PER_REGION" --segment-leaves "$SEGMENT_LEAVES"
}

# run_benchmark <module dir> <package> <benchmark regexp> <result file>
run_benchmark() {
    local dir="$1" package="$2" pattern="$3" result="$4" status
    note "benchmark $pattern ($dir): count $COUNT, window $BENCHTIME, warm-up $WARMUP, GOMAXPROCS=1"
    set +e
    (cd "$SYSTEMS_DIR/$dir" && go test -run '^$' -bench "$pattern" -benchmem -count "$COUNT" \
        -benchtime "$BENCHTIME" -cpu 1 -timeout 0 "$package") 2>&1 | tee "$result"
    status=${PIPESTATUS[0]}
    set -e
    [[ $status -eq 0 ]] || die "$dir benchmark failed (exit $status); see $result"
}

# begin_experiment <name> [args...]: common start-up of every experiment script.
begin_experiment() {
    EXPERIMENT="$1"
    shift
    parse_args "$@"
    find_python
    command -v go >/dev/null || die "go is not installed"
    command -v docker >/dev/null || die "docker is not installed"
    prepare_dataset
    start_services
    export_fair_env
    RESULTS="$OUTPUT/$EXPERIMENT"
    mkdir -p "$RESULTS"
    note "experiment $EXPERIMENT -> $RESULTS"
}
