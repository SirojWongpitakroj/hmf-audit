#!/usr/bin/env bash
# Find the tampered logs among the logs that satisfy an attribute query, and show
# each one's correct content next to its tampered content.
#
# The query is exactly the one verification-query.sh takes:
#   find-tampered.sh --tenant T246 --service spark-executor --log-type INFO --region R0 \
#                    --start 2017-03-01T00:00:00Z --end 2017-12-31T00:00:00Z
#
# For every log that fails verification it prints:
#   Correct content   the log as ingested (from the source dataset)
#   Tampered content  the stored row, decrypted without authentication (needs the key)
#   Correct / tampered digest  the digest committed under the anchored root versus
#                     the digest of the row stored now (always shown; this is what
#                     verification compares, and it differs when the content changed)
# If a content cannot be seen, only the digests are shown for it.
#
# Exit code: 0 no tampered log, 1 tampered log(s) found, 2 error. Option: --json.
# Local Docker only (the 100k ingest).
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

for arg in "$@"; do
    if [[ "$arg" == -h || "$arg" == --help ]]; then
        sed -n '2,19p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
        exit 0
    fi
done
parse_query_args "$@"
require_100k
AUDIT_WITH_KEY=1
audit_query
trap 'rm -f "$AUDIT_OUT" "$AUDIT_ERR"' EXIT
if [[ $AUDIT_CODE -eq 2 ]]; then
    cat "$AUDIT_ERR" >&2
    exit 2
fi
"$PYTHON" "$(winpath "$DEMO_DIR/find_tampered.py")" --stdout "$(winpath "$AUDIT_OUT")" \
    --query "$QUERY_DESCRIPTION" --dataset "$(winpath "$SOURCE_100K")" "${json[@]}"
