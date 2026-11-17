#!/usr/bin/env bash
# Ingest the 100,000-log dataset that the demo scripts work on. This is a thin
# wrapper around ../attribute-query/ingest.sh so the whole demo can be run from
# this folder.
#
#   ./ingest.sh            ingest 100,000 logs
#   ./ingest.sh --reset    empty the demo's tables first, then ingest
#
# Local Docker only.
set -euo pipefail
exec "$(dirname "${BASH_SOURCE[0]}")/../attribute-query/ingest.sh" --records 100000 "$@"
