#!/usr/bin/env bash
# Helpers shared by the provisioning scripts. Source this file; do not run it.
# The scripts run ON the VM they provision, from a checkout of this repository.
set -euo pipefail

PROVISION_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLOUD_DIR="$(cd "$PROVISION_DIR/.." && pwd)"
SYSTEMS_DIR="$(cd "$CLOUD_DIR/.." && pwd)"
CLOUD_ENV_FILE="${CLOUD_ENV_FILE:-$CLOUD_DIR/cloud.env}"

note() { echo "[$(date +%H:%M:%S)] $*" >&2; }
die() { echo "error: $*" >&2; exit 1; }

[[ -f "$CLOUD_ENV_FILE" ]] || die "missing $CLOUD_ENV_FILE (copy it from the runner)"
# shellcheck disable=SC1090
. "$CLOUD_ENV_FILE"
: "${FAIR_DATA_DIR:=/var/lib/fair-data}" "${CLOUD_CASSANDRA_HEAP:=3G}" "${CLOUD_CASSANDRA_NEWSIZE:=600M}"
: "${CLOUD_PG_USER:=fair}" "${CLOUD_PG_DB:=fair_benchmark}"
CASSANDRA_IMAGE=cassandra:5.0.9
POSTGRES_IMAGE=postgres:17

# Docker runs the databases (the same images as the local stack, both multi-arch).
# Containers restart with the VM, so a stopped-and-started VM comes back by itself.
ensure_docker() {
    if ! command -v docker >/dev/null 2>&1; then
        note "installing Docker"
        curl -fsSL https://get.docker.com | sudo sh
    fi
    sudo systemctl enable --now docker >/dev/null 2>&1 || true
    sudo docker info >/dev/null 2>&1 || die "the docker daemon is not running"
}

# This VM's private address (the first one reported).
internal_ip() { hostname -I | awk '{print $1}'; }

# The data directory should be a separate persistent disk, not the boot disk.
ensure_data_dir() {
    sudo mkdir -p "$FAIR_DATA_DIR/$1"
    if ! mountpoint -q "$FAIR_DATA_DIR"; then
        echo "  warn: $FAIR_DATA_DIR is not a mount point; data will be on the boot disk." >&2
        echo "        Attach a persistent SSD disk and mount it there first (see systems/cloud/README.md)." >&2
    fi
}
