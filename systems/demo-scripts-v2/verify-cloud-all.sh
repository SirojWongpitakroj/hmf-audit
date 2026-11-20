#!/usr/bin/env bash
set -euo pipefail
mode="$1"
shift
scripts="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
work="$(mktemp -d)"
trap 'rm -f "$work/ordinals.json"; rmdir "$work"' EXIT
batch="$work/ordinals.json"

result() {
    printf 'DEMO_EVENT {"type":"result","scheme":"%s","status":"%s","message":"%s"}\n' "$1" "$2" "$3"
}

failed=0
case "$mode" in
    hmf)
        repeats="${7}"
        if ! DEMO_BATCH_OUT="$batch" bash "$scripts/verify-cloud.sh" "$@"; then
            result HMF-Audit ERROR 'HMF-Audit verification failed. See the activity log.'
            failed=1
        fi
        if [[ ! -s "$batch" ]]; then
            for scheme in BLAS DHT-backed EVSEB; do
                result "$scheme" UNAVAILABLE 'HMF-Audit could not resolve the source batch'
            done
            exit 1
        fi
        if ! DEMO_BATCH_IN="$batch" bash "$scripts/verify-cloud-blas.sh" "$@"; then
            result BLAS ERROR 'BLAS verification failed. See the activity log.'
            failed=1
        fi ;;
    blas)
        repeats="${!#}"
        if ! DEMO_BATCH_OUT="$batch" bash "$scripts/verify-cloud-blas.sh" "$@"; then
            result BLAS ERROR 'BLAS verification failed. See the activity log.'
            failed=1
        fi
        if [[ ! -s "$batch" ]]; then
            for scheme in DHT-backed EVSEB; do
                result "$scheme" UNAVAILABLE 'BLAS could not resolve the source batch'
            done
            exit 1
        fi ;;
    *) exit 2 ;;
esac
for scheme in DHT-backed EVSEB; do
    if ! DEMO_BATCH_IN="$batch" bash "$scripts/verify-cloud-recipient.sh" "$scheme" "$repeats"; then
        failed=1
    fi
done
exit "$failed"
