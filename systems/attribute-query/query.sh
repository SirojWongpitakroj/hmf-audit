#!/usr/bin/env bash
# Query the ingested logs by attribute and audit them; every matching log is
# reported VALID or REJECTED.
#
#   query.sh --tenant T745 --service spark-executor --log-type UNSPECIFIED \
#            --region R1 --at 2015-09-01T18:14:40Z
#   query.sh ... --start 2015-09-01T18:00:00Z --end 2015-09-01T19:00:00Z --limit 50
#
# Options are passed to cmd/hmf-auditor (see its flags: -json, -limit, ...).
# Exit code: 0 all VALID, 1 at least one REJECTED, 2 error.
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
args=()
for arg in "$@"; do
    case "$arg" in
    --tenant | --service | --log-type | --region | --at | --start | --end | --limit) args+=("-${arg#--}") ;;
    --json) args+=(-json) ;;
    *) args+=("$arg") ;;
    esac
done
build_auditor
exec "$AUDITOR_BIN" -key-file "$(winpath "$KEY_FILE")" "${args[@]}"
