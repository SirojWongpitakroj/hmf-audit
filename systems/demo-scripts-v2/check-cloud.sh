#!/usr/bin/env bash
# Read-only probes against the existing cloud ingest.
set -euo pipefail
export FAIR_CLOUD=1
. "$(dirname "${BASH_SOURCE[0]}")/../experiments/scripts/lib.sh"

pg_ready() {
    PGOPTIONS='-c default_transaction_read_only=on -c statement_timeout=5000' \
        cloud_psql "$1" -c "SELECT 1 FROM $2 LIMIT 1" 2>/dev/null | grep -qx 1
}

cql_ready() {
    cloud_cql "$1" -e "SELECT $2 FROM $3 LIMIT 1" 2>/dev/null | grep -F '(1 rows)' >/dev/null
}

check_scheme() {
    local scheme="$1" pg_host="$2" pg_table="$3"
    shift 3
    printf 'DEMO_EVENT {"type":"scheme","scheme":"%s","message":"Checking %s cloud storage"}\n' "$scheme" "$scheme"
    local status=READY message='Cloud storage returned a row'
    if ! pg_ready "$pg_host" "$pg_table"; then
        status=ERROR
        message='PostgreSQL table is missing, empty, or unreachable'
    elif (( $# )) && ! cql_ready "$@"; then
        status=ERROR
        message='Cassandra table is missing, empty, or unreachable'
    elif (( $# )); then
        message='PostgreSQL and Cassandra each returned a row'
    fi
    printf 'DEMO_EVENT {"type":"result","scheme":"%s","status":"%s","message":"%s"}\n' "$scheme" "$status" "$message"
    [[ "$status" == READY ]]
}

failed=0
check_scheme HMF-Audit "$CLOUD_SHARED_PG_HOST" hmf.encrypted_logs \
    "$CLOUD_HMF_CASSANDRA_HOST" region_id hmf_audit.hmf_segments_by_shard || failed=1
check_scheme BLAS "$CLOUD_BLAS_PG_HOST" blas.entry_files || failed=1
check_scheme DHT-backed "$CLOUD_SHARED_PG_HOST" dht.records \
    "$CLOUD_DHT_CASSANDRA_HOST" proof_key dhtbacked.proof_entries || failed=1
check_scheme EVSEB "$CLOUD_SHARED_PG_HOST" evseb.encrypted_logs \
    "$CLOUD_EVSEB_CASSANDRA_HOST" lid_hash evseb.dht_entries || failed=1
exit "$failed"
