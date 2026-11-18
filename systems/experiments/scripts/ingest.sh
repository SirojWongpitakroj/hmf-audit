#!/usr/bin/env bash
# Ingest the deterministic subset into the local PostgreSQL and Cassandra for
# all four schemes. Exactly two sizes are accepted; any other number is
# rejected before anything is touched.
#
#   ingest.sh --records 100000        generated from the source dataset (deterministic)
#   ingest.sh --records 10000000      the existing 10M file (SHA-256 checked)
#   ingest.sh --records N --systems hmf-audit,evseb     (subset of the four schemes)
#
# Ingestion reuses the unchanged fair fixtures: each scheme's fixture seeds the
# database when its benchmark process starts, so this runs one minimal
# benchmark case (q=1, one execution, no warm-up) per scheme and exits. The
# data stays in the database afterwards. NOTE: the experiment scripts re-seed
# at their own start (the fixtures truncate first), so ingest.sh is for
# loading and inspecting the data, not a prerequisite of the experiments.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

ALLOWED_RECORDS=(100000 10000000)
records=""
ingest_systems="hmf-audit,dht-backed,blas,evseb"
while [[ $# -gt 0 ]]; do
    case "$1" in
    --records) [[ $# -ge 2 ]] || die "--records needs a value"; records="$2"; shift 2 ;;
    --records=*) records="${1#--records=}"; shift ;;
    --systems) [[ $# -ge 2 ]] || die "--systems needs a value"; ingest_systems="$2"; shift 2 ;;
    -h | --help)
        echo "usage: ingest.sh --records 100000|10000000 [--systems hmf-audit,dht-backed,blas,evseb]" >&2
        exit 0 ;;
    *) die "unknown option $1 (usage: ingest.sh --records 100000|10000000)" ;;
    esac
done

# Exact match on the canonical decimal text: 1e5, 100,000, 0100000, 100000.0
# and 10M are all rejected.
accepted=0
for allowed in "${ALLOWED_RECORDS[@]}"; do
    [[ "$records" == "$allowed" ]] && accepted=1
done
if [[ $accepted -ne 1 ]]; then
    echo "error: rejected --records '${records}': only ${ALLOWED_RECORDS[0]} (100k) and ${ALLOWED_RECORDS[1]} (10 million) are allowed" >&2
    exit 2
fi

declare -A MODULE=([hmf-audit]=hmf-audit [dht-backed]=dht-backed [blas]=blas [evseb]=EVBES)
declare -A PACKAGE=([hmf-audit]=./benchmarks/latency [dht-backed]=./benchmark [blas]=./eval [evseb]=./internal/integrity)
declare -A PATTERN=(
    [hmf-audit]='^BenchmarkFairHMFVerificationCore$/^clustered-2$/^q=1$'
    [dht-backed]='^BenchmarkFairDHTVerificationRequest$/^clustered-2$/^q=1$'
    [blas]='^BenchmarkFairBLASVerificationRequest$/^clustered-2$/^q=1$'
    [evseb]='^BenchmarkFairEVBESVerificationRequest$/^clustered-2$/^q=1$'
)
IFS=',' read -r -a selected <<<"$ingest_systems"
for system in "${selected[@]}"; do
    [[ -n "${MODULE[$system]:-}" ]] || die "unknown system '$system'"
done

# One execution of the smallest case; the seeding is what we want.
begin_experiment ingest --per-region $((records / REGIONS)) --count 1 --warmup 0 --bench-time 1x
write_environment ingest "$ingest_systems" "$RESULTS"

started=$(date +%s)
for system in "${selected[@]}"; do
    note "ingesting $system ($records records)"
    run_benchmark "${MODULE[$system]}" "${PACKAGE[$system]}" "${PATTERN[$system]}" "$RESULTS/$system.txt"
done

# Report what is actually in the database, and fail if it does not match.
note "row counts in PostgreSQL"
counts="$(pg_row_counts)"
echo "$counts" | tee "$RESULTS/row_counts.txt" >&2
for system in "${selected[@]}"; do
    case "$system" in
    hmf-audit) table=hmf.encrypted_logs ;;
    evseb) table=evseb.encrypted_logs ;;
    *) continue ;;
    esac
    found="$(echo "$counts" | awk -v t="$table" '$1 == t {print $2}')"
    [[ "$found" == "$records" ]] || die "$table has '${found:-no rows}' rows, expected $records"
done
if [[ "$CLOUD" == 1 ]]; then
    # HMF regions stay separate partitions; EVSEB and DHT-backed routing rows are present.
    bash "$SYSTEMS_DIR/cloud/check.sh" post "$ingest_systems" || die "cloud post-ingest verification failed; see above"
fi
note "ingestion of $records records finished in $(( $(date +%s) - started )) s; logs in $RESULTS"
