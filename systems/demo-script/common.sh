#!/usr/bin/env bash
# Shared by verification-query.sh and tamper.sh so both read the attribute query identically.
# Source this file; do not run it. Local Docker only.
. "$(dirname "${BASH_SOURCE[0]}")/../attribute-query/lib.sh"
# Every failure in these scripts is an error: exit code 2 (1 is reserved for REJECTED).
die() { echo "error: $*" >&2; exit 2; }
DEMO_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXPECTED_RECORDS=100000
# The log generator's copy of the 100k logs: where the original content of a tampered log is read from.
SOURCE_100K="$SYSTEMS_DIR/experiments/datasets/spark_stratified_100000.jsonl"

tenant="" service="" log_type="" region="" at="" start="" end="" max_ids=100 json=()

# parse_query_args "$@": the attribute query (and --max-ids / --json).
parse_query_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
        --tenant) tenant="${2:-}"; shift 2 ;;
        --service) service="${2:-}"; shift 2 ;;
        --log-type) log_type="${2:-}"; shift 2 ;;
        --region) region="${2:-}"; shift 2 ;;
        --at) at="${2:-}"; shift 2 ;;
        --start) start="${2:-}"; shift 2 ;;
        --end) end="${2:-}"; shift 2 ;;
        --max-ids) max_ids="${2:-}"; shift 2 ;;
        --json) json=(--json); shift ;;
        *) die "unknown option $1 (see --help)" ;;
        esac
    done
    [[ -n "$tenant" && -n "$service" && -n "$log_type" && -n "$region" ]] ||
        die "--tenant, --service, --log-type and --region are required"
    [[ -n "$at" || ( -n "$start" && -n "$end" ) ]] || die "give a time: --at T, or both --start and --end"
    [[ "$max_ids" =~ ^-?[0-9]+$ ]] || die "--max-ids must be an integer"
}

# require_100k: the scripts work on the 100k ingest only.
require_100k() {
    command -v go >/dev/null || die "go is not installed"
    command -v docker >/dev/null || die "docker is not installed"
    find_python
    local rows
    rows="$(docker exec fair-postgres psql -U fair -d fair_benchmark -t -A \
        -c 'SELECT count(*) FROM hmfq.encrypted_logs' 2>/dev/null || true)"
    [[ "$rows" == "$EXPECTED_RECORDS" ]] || die "needs the 100k ingest (found '${rows:-no database}' rows in hmfq.encrypted_logs); run: systems/attribute-query/ingest.sh --records 100000"
}

# audit_query: run the auditor for the parsed query. Sets AUDIT_OUT / AUDIT_ERR
# (temp files; the caller removes them), AUDIT_CODE and QUERY_DESCRIPTION.
audit_query() {
    local args=(-json -tenant "$tenant" -service "$service" -log-type "$log_type" -region "$region")
    QUERY_DESCRIPTION="tenant=$tenant service=$service log_type=$log_type region=$region"
    if [[ -n "$at" ]]; then args+=(-at "$at"); QUERY_DESCRIPTION+=" time=$at"
    else args+=(-start "$start" -end "$end"); QUERY_DESCRIPTION+=" time=[$start, $end)"; fi
    # With AUDIT_WITH_KEY=1 the auditor also reports the stored content of rejected logs.
    if [[ "${AUDIT_WITH_KEY:-0}" == 1 && -f "$KEY_FILE" ]]; then args+=(-key-file "$(winpath "$KEY_FILE")"); fi
    build_auditor
    AUDIT_OUT="$(mktemp)"
    AUDIT_ERR="$(mktemp)"
    set +e
    "$AUDITOR_BIN" "${args[@]}" >"$AUDIT_OUT" 2>"$AUDIT_ERR"
    AUDIT_CODE=$?
    set -e
}
