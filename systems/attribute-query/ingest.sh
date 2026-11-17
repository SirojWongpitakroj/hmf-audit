#!/usr/bin/env bash
# Ingest a dataset through the real HMF-Audit write path so logs can be queried
# by attribute (tenant, service, log type, region, time) and audited.
# Exactly two sizes are accepted; anything else is rejected.
#
#   ingest.sh --records 100000
#   ingest.sh --records 10000000
#   ingest.sh --records 100000 --reset      empty this tool's tables first
set -euo pipefail
. "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

records="" reset=()
while [[ $# -gt 0 ]]; do
    case "$1" in
    --records) [[ $# -ge 2 ]] || die "--records needs a value"; records="$2"; shift 2 ;;
    --records=*) records="${1#--records=}"; shift ;;
    --reset) reset=(-reset); shift ;;
    -h | --help) echo "usage: ingest.sh --records 100000|10000000 [--reset]" >&2; exit 0 ;;
    *) die "unknown option $1 (usage: ingest.sh --records 100000|10000000 [--reset])" ;;
    esac
done
if [[ "$records" != 100000 && "$records" != 10000000 ]]; then
    echo "error: rejected --records '${records}': only 100000 (100k) and 10000000 (10 million) are allowed" >&2
    exit 2
fi

command -v go >/dev/null || die "go is not installed"
command -v docker >/dev/null || die "docker is not installed"
parse_args --per-region $((records / REGIONS))
prepare_dataset
attribute_services
note "ingesting $records records"
(cd "$SYSTEMS_DIR/hmf-audit" && go run ./cmd/hmf-ingest -dataset "$(winpath "$DATASET")" \
    -key-file "$(winpath "$KEY_FILE")" -keyspace "$KEYSPACE" "${reset[@]}")
