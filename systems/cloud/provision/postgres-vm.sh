#!/usr/bin/env bash
# Provision one PostgreSQL VM (t2a-standard-2, Arm). Run it ON that VM.
#
#   postgres-vm.sh blas      VM 4  schema blas only
#   postgres-vm.sh shared    VM 5  schemas hmf, evseb, dht (cloud-provider storage)
#
# Both VMs get the identical server configuration. Needs CLOUD_PG_PASSWORD in
# cloud.env. Idempotent: re-running keeps the data.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

role="${1:-}"
[[ "$role" == blas || "$role" == shared ]] || die "usage: postgres-vm.sh blas|shared"
[[ "${CLOUD_PG_PASSWORD:-}" =~ ^[A-Za-z0-9]+$ ]] || die "set CLOUD_PG_PASSWORD (letters and digits only) in $CLOUD_ENV_FILE"

ensure_docker
ensure_data_dir postgres
note "PostgreSQL '$role', data $FAIR_DATA_DIR/postgres"

if sudo docker ps -a --format '{{.Names}}' | grep -qx fair-postgres; then
    sudo docker start fair-postgres >/dev/null
else
    # 8 GiB VM: shared_buffers 2 GB, the rest left to the OS page cache.
    sudo docker run -d --name fair-postgres --restart unless-stopped -p 5432:5432 \
        -e POSTGRES_USER="$CLOUD_PG_USER" -e POSTGRES_PASSWORD="$CLOUD_PG_PASSWORD" -e POSTGRES_DB="$CLOUD_PG_DB" \
        -v "$FAIR_DATA_DIR/postgres:/var/lib/postgresql/data" "$POSTGRES_IMAGE" \
        -c listen_addresses='*' -c max_connections=100 -c shared_buffers=2GB \
        -c effective_cache_size=5GB -c max_wal_size=8GB -c checkpoint_timeout=15min >/dev/null
fi

note "waiting for PostgreSQL"
for ((i = 0; i < 60; i++)); do
    sudo docker exec fair-postgres pg_isready -U "$CLOUD_PG_USER" -d "$CLOUD_PG_DB" >/dev/null 2>&1 && break
    sleep 3
done
sudo docker exec fair-postgres pg_isready -U "$CLOUD_PG_USER" -d "$CLOUD_PG_DB" >/dev/null 2>&1 || die "PostgreSQL did not become ready"

apply() { sudo docker exec -i fair-postgres psql -v ON_ERROR_STOP=1 -q -U "$CLOUD_PG_USER" -d "$CLOUD_PG_DB" <"$1"; }
if [[ "$role" == shared ]]; then
    note "applying 001-fair-benchmark.sql (hmf, evseb) and postgres-shared.sql (dht)"
    apply "$SYSTEMS_DIR/fair-persistence/docker/postgresql/001-fair-benchmark.sql"
    apply "$CLOUD_DIR/schema/postgres-shared.sql"
else
    note "applying postgres-blas.sql (blas)"
    apply "$CLOUD_DIR/schema/postgres-blas.sql"
fi
note "done: PostgreSQL '$role' ready on $(internal_ip):5432. Allow port 5432 only from the runner (10.148.0.8)."
