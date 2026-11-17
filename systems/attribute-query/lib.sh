#!/usr/bin/env bash
# Shared helpers for ingest.sh and query.sh. Source this file; do not run it.
# Everything targets the local Docker stack in systems/fair-persistence
# (PostgreSQL 127.0.0.1:55432, Cassandra 127.0.0.1:19042).
AQ_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
export MSYS_NO_PATHCONV=1 MSYS2_ARG_CONV_EXCL='*'
. "$AQ_DIR/../experiments/scripts/lib.sh"
KEY_FILE="$AQ_DIR/keys/hmf-ingest.key"
KEYSPACE=hmf_audit_q

# attribute_services: start PostgreSQL and Cassandra and create this tool's own
# schema (hmfq) and keyspace (hmf_audit_q). Idempotent.
attribute_services() {
    note "starting the local PostgreSQL and Cassandra containers"
    compose up -d --wait postgres cassandra
    compose exec -T postgres psql -v ON_ERROR_STOP=1 -q -U fair -d fair_benchmark <"$AQ_DIR/init_pg.sql"
    # Same table definitions as the HMF-Audit benchmark keyspace, under its own name.
    sed "s/hmf_audit/$KEYSPACE/g" "$SYSTEMS_DIR/fair-persistence/docker/cassandra/init-hmf.cql" >"$AQ_DIR/.init_cassandra.cql"
    docker cp "$(winpath "$AQ_DIR/.init_cassandra.cql")" fair-cassandra:/tmp/init_hmf_q.cql
    compose exec -T cassandra cqlsh -f /tmp/init_hmf_q.cql
    rm -f "$AQ_DIR/.init_cassandra.cql"
}

# build_auditor: compile cmd/hmf-auditor into attribute-query/bin and set
# AUDITOR_BIN. `go run` would turn the auditor's exit codes (0 VALID, 1
# REJECTED, 2 error) into 1, so callers run the binary directly.
build_auditor() {
    local ext=""
    case "$(uname -s)" in MINGW* | MSYS* | CYGWIN*) ext=".exe" ;; esac
    mkdir -p "$AQ_DIR/bin"
    AUDITOR_BIN="$AQ_DIR/bin/hmf-auditor$ext"
    (cd "$SYSTEMS_DIR/hmf-audit" && go build -o "$(winpath "$AUDITOR_BIN")" ./cmd/hmf-auditor) ||
        die "could not build cmd/hmf-auditor"
}
