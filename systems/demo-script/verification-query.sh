#!/usr/bin/env bash
# Demo: audit a query by attribute against the ingested 100,000 logs.
#
#   verification-query.sh --tenant T745 --service spark-executor --log-type UNSPECIFIED \
#           --region R1 --at 2015-09-01T18:14:40Z
#   verification-query.sh --tenant T246 --service spark-executor --log-type INFO --region R0 \
#           --start 2017-03-01T00:00:00Z --end 2017-12-31T00:00:00Z
#
# Output: the log count, the log IDs, and Status VALID (verification passed) or
# REJECTED (verification failed). Exit code: 0 VALID, 1 REJECTED, 2 error.
# Options: --max-ids N (IDs printed, default 100, -1 for all), --json.
# Local Docker only. Ingest first with: systems/attribute-query/ingest.sh --records 100000
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

for arg in "$@"; do
    if [[ "$arg" == -h || "$arg" == --help ]]; then
        sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
        exit 0
    fi
done
parse_query_args "$@"
require_100k
audit_query
trap 'rm -f "$AUDIT_OUT" "$AUDIT_ERR"' EXIT
if [[ $AUDIT_CODE -eq 2 ]]; then
    cat "$AUDIT_ERR" >&2
    exit 2
fi
"$PYTHON" "$(winpath "$DEMO_DIR/format_result.py")" --stdout "$(winpath "$AUDIT_OUT")" --query "$QUERY_DESCRIPTION" \
    --max-ids "$max_ids" --records "$EXPECTED_RECORDS" "${json[@]}"
