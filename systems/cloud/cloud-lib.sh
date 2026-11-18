#!/usr/bin/env bash
# Cloud endpoints for the experiment scripts. Sourced by
# experiments/scripts/lib.sh when FAIR_CLOUD=1; do not run it.
#
# Topology (see README.md in this folder):
#   HMF-Audit Cassandra, EVSEB Cassandra, DHT-backed Cassandra   one VM each
#   BLAS PostgreSQL                                              one VM
#   shared cloud-provider PostgreSQL (hmf, evseb, dht schemas)   one VM
#   benchmark runner (this machine)                              runs all four Go benchmarks
#   blockchain anchor                                            Hardhat node, not in any timer
#
# Every benchmark is its own `go test` process, so each one is pointed at its
# own scheme's servers (apply_endpoints) and the variables it does not use are
# left unset: a mis-routed scheme fails loudly instead of silently reading
# another scheme's database.

CLOUD_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLOUD_ENV_FILE="${CLOUD_ENV_FILE:-$CLOUD_DIR/cloud.env}"
[[ -f "$CLOUD_ENV_FILE" ]] || die "cloud mode needs $CLOUD_ENV_FILE (copy cloud.env.example and fill it in)"
# shellcheck disable=SC1090
. "$CLOUD_ENV_FILE"

: "${CLOUD_CASSANDRA_PORT:=9042}" "${CLOUD_PG_PORT:=5432}" "${CLOUD_PG_USER:=fair}"
: "${CLOUD_PG_DB:=fair_benchmark}" "${CLOUD_ANCHOR_PORT:=8545}"
CASSANDRA_IMAGE=cassandra:5.0.9

cloud_validate() {
    local name
    for name in CLOUD_HMF_CASSANDRA_HOST CLOUD_EVSEB_CASSANDRA_HOST CLOUD_DHT_CASSANDRA_HOST \
        CLOUD_BLAS_PG_HOST CLOUD_SHARED_PG_HOST CLOUD_ANCHOR_HOST CLOUD_PG_PASSWORD; do
        [[ -n "${!name:-}" ]] || die "$name is not set in $CLOUD_ENV_FILE"
    done
    # The password is placed in a postgres:// URL, so it must not need escaping.
    [[ "$CLOUD_PG_PASSWORD" =~ ^[A-Za-z0-9]+$ ]] \
        || die "CLOUD_PG_PASSWORD must be letters and digits only (it is used inside a URL)"
    [[ "$CLOUD_HMF_CASSANDRA_HOST" != "$CLOUD_EVSEB_CASSANDRA_HOST" \
        && "$CLOUD_HMF_CASSANDRA_HOST" != "$CLOUD_DHT_CASSANDRA_HOST" \
        && "$CLOUD_EVSEB_CASSANDRA_HOST" != "$CLOUD_DHT_CASSANDRA_HOST" ]] \
        || die "the three Cassandra hosts must be three different VMs"
    [[ "$CLOUD_BLAS_PG_HOST" != "$CLOUD_SHARED_PG_HOST" ]] \
        || die "the BLAS PostgreSQL and the shared PostgreSQL must be different VMs"
}

# cloud_dsn <host> [search_path]. The traffic stays inside the VPC; the VMs'
# firewall admits only the runner, so TLS is not enabled.
cloud_dsn() {
    local dsn="postgres://${CLOUD_PG_USER}:${CLOUD_PG_PASSWORD}@$1:${CLOUD_PG_PORT}/${CLOUD_PG_DB}?sslmode=disable"
    [[ -z "${2:-}" ]] || dsn+="&search_path=$2"
    printf '%s' "$dsn"
}

# apply_endpoints <module dir>: called inside the subshell of one benchmark process.
apply_endpoints() {
    [[ "${CLOUD:-0}" == 1 ]] || return 0
    unset FAIR_POSTGRES_DSN FAIR_DHT_POSTGRES_DSN FAIR_CASSANDRA_HOST FAIR_CASSANDRA_PORT
    case "$1" in
    hmf-audit)
        export FAIR_POSTGRES_DSN="$(cloud_dsn "$CLOUD_SHARED_PG_HOST")"
        export FAIR_CASSANDRA_HOST="$CLOUD_HMF_CASSANDRA_HOST" FAIR_CASSANDRA_PORT="$CLOUD_CASSANDRA_PORT" ;;
    EVBES)
        export FAIR_POSTGRES_DSN="$(cloud_dsn "$CLOUD_SHARED_PG_HOST")"
        export FAIR_CASSANDRA_HOST="$CLOUD_EVSEB_CASSANDRA_HOST" FAIR_CASSANDRA_PORT="$CLOUD_CASSANDRA_PORT" ;;
    dht-backed)
        export FAIR_DHT_POSTGRES_DSN="$(cloud_dsn "$CLOUD_SHARED_PG_HOST" dht)"
        export FAIR_CASSANDRA_HOST="$CLOUD_DHT_CASSANDRA_HOST" FAIR_CASSANDRA_PORT="$CLOUD_CASSANDRA_PORT" ;;
    blas)
        export FAIR_POSTGRES_DSN="$(cloud_dsn "$CLOUD_BLAS_PG_HOST")" ;;
    *) die "apply_endpoints: unknown module '$1'" ;;
    esac
}

cloud_psql() { # cloud_psql <host> [psql args]
    local host="$1"; shift
    PGCONNECT_TIMEOUT=10 PGPASSWORD="$CLOUD_PG_PASSWORD" psql -h "$host" -p "$CLOUD_PG_PORT" \
        -U "$CLOUD_PG_USER" -d "$CLOUD_PG_DB" -v ON_ERROR_STOP=1 -q -At -F ' ' "$@"
}

cloud_cql() { # cloud_cql <host> [cqlsh args]; cqlsh runs from the Cassandra image
    local host="$1"; shift
    docker run --rm -i --network host "$CASSANDRA_IMAGE" cqlsh "$host" "$CLOUD_CASSANDRA_PORT" \
        --connect-timeout=15 --request-timeout=60 "$@"
}

# cloud_row_counts <sql-template-fn>: row counts of every scheme's tables.
cloud_row_counts() {
    cloud_psql "$CLOUD_SHARED_PG_HOST" -c "$(row_count_sql "'hmf','evseb','dht'")"
    cloud_psql "$CLOUD_BLAS_PG_HOST" -c "$(row_count_sql "'blas'")"
}

# Number of routing levels for n logical DHT nodes: ceil(log2 n) = bit_length(n-1).
routing_levels() {
    local n=$(($1 - 1)) bits=0
    while ((n > 0)); do bits=$((bits + 1)); n=$((n >> 1)); done
    echo "$bits"
}

# Text recorded in environment.txt (postprocess.py env --deployment ...).
cloud_description() {
    echo "GCP asia-southeast1; runner t2a-standard-4; HMF-Audit/EVSEB/DHT-backed Cassandra, BLAS PostgreSQL and shared PostgreSQL on t2a-standard-2; anchor e2-medium"
}

cloud_validate
