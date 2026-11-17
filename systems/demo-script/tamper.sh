#!/usr/bin/env bash
# Tamper with ONE existing log that satisfies an attribute query: the log's
# stored ciphertext is modified in place (no log is inserted or removed) in the
# storage provider's table, hmfq.encrypted_logs. The committed digest in the
# hierarchy is untouched, so verification of the same query now REJECTS it.
#
# The query is exactly the one verification-query.sh takes:
#   tamper.sh --tenant T246 --service spark-executor --log-type INFO --region R0 \
#             --start 2017-03-01T00:00:00Z --end 2017-12-31T00:00:00Z
#   tamper.sh <query> --index 3     modify the 4th matching log (default: the first)
#   tamper.sh <query> --verify      then run verification-query.sh with the same query
#   tamper.sh --restore             undo the recorded tampering
#
# Exit code: 0 done, 2 error. Local Docker only (the 100k ingest).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"
STATE="$DEMO_DIR/.tamper-state"

index=0 verify=0 restore=0 rest=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --index) index="${2:-}"; shift 2 ;;
    --verify) verify=1; shift ;;
    --restore) restore=1; shift ;;
    -h | --help) sed -n '2,15p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 0 ;;
    *) rest+=("$1"); shift ;;
    esac
done
[[ "$index" =~ ^[0-9]+$ ]] || die "--index must be a non-negative integer"

pg() { docker exec fair-postgres psql -U fair -d fair_benchmark -q -t -A -v ON_ERROR_STOP=1 "$@"; }
row_hash() { pg -c "SELECT encode(sha256(ciphertext),'hex') FROM hmfq.encrypted_logs WHERE log_id = '$1'"; }
flip() {
    pg -c "UPDATE hmfq.encrypted_logs SET ciphertext = set_byte(ciphertext, 0, get_byte(ciphertext, 0) # 255) WHERE log_id = '$1' RETURNING encode(sha256(ciphertext),'hex')"
}
state_value() { grep "^$1=" "$STATE" | head -1 | cut -d= -f2-; }

if [[ $restore -eq 1 ]]; then
    [[ -f "$STATE" ]] || die "nothing to restore: no tampering is recorded"
    docker ps --format '{{.Names}}' | grep -qx fair-postgres || die "fair-postgres is not running"
    id="$(state_value log_id)" original="$(state_value original_sha256)" tampered="$(state_value tampered_sha256)"
    current="$(row_hash "$id")"
    if [[ "$current" == "$original" ]]; then
        note "log $id already holds its original content"
    elif [[ "$current" == "$tampered" || -z "$tampered" ]]; then
        # An empty tampered hash means the tampering was interrupted before it was
        # recorded. The byte flip is its own inverse, so apply it and check the result.
        restored="$(flip "$id")"
        if [[ "$restored" != "$original" ]]; then
            flip "$id" >/dev/null
            die "restore did not reproduce the original content for $id (row left as it was found); stop and inspect"
        fi
        note "restored log $id to its original content"
    else
        die "log $id was changed by something else since the tampering; not touching it"
    fi
    rm -f "$STATE"
    exit 0
fi

[[ ${#rest[@]} -gt 0 ]] || die "give the attribute query (see --help)"
[[ ! -f "$STATE" ]] || die "a tampered log is already recorded ($(state_value log_id)); run: tamper.sh --restore"
parse_query_args "${rest[@]}"
require_100k
audit_query
trap 'rm -f "$AUDIT_OUT" "$AUDIT_ERR"' EXIT
if [[ $AUDIT_CODE -eq 2 ]]; then
    cat "$AUDIT_ERR" >&2
    exit 2
fi
set +e
picked="$("$PYTHON" "$(winpath "$DEMO_DIR/pick_target.py")" --stdout "$(winpath "$AUDIT_OUT")" --index "$index")"
pick_code=$?
set -e
[[ $pick_code -eq 0 ]] || exit 2
count="${picked%% *}" id="${picked#* }"
[[ "$id" =~ ^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$ ]] || die "unexpected log ID '$id'"

original="$(row_hash "$id")"
[[ -n "$original" ]] || die "log $id is not in the provider's table"
printf 'log_id=%s\noriginal_sha256=%s\ntampered_sha256=\n' "$id" "$original" >"$STATE"
if ! tampered="$(flip "$id")" || [[ -z "$tampered" || "$tampered" == "$original" || "$tampered" == *$'\n'* ]]; then
    rm -f "$STATE"
    die "the modification did not change exactly one row; nothing recorded"
fi
printf 'log_id=%s\noriginal_sha256=%s\ntampered_sha256=%s\n' "$id" "$original" "$tampered" >"$STATE"

echo "Query:    $QUERY_DESCRIPTION"
echo "Matches:  $count log(s); modified #$index"
echo "Tampered: $id"
echo "          one byte of its stored ciphertext was changed in place (no log inserted or removed)"
echo "          content hash  $original"
echo "                    ->  $tampered"
echo "Verify:   ./verification-query.sh ${rest[*]}"
echo "Undo:     ./tamper.sh --restore"
if [[ $verify -eq 1 ]]; then
    echo
    set +e
    bash "$DEMO_DIR/verification-query.sh" "${rest[@]}"
    set -e
fi
