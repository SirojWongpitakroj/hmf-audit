#!/usr/bin/env bash
# Provision one Cassandra VM (t2a-standard-2, Arm). Run it ON that VM.
#
#   cassandra-vm.sh hmf      VM 1  keyspace hmf_audit  (HMF-Audit; the four regions stay separate partitions)
#   cassandra-vm.sh evseb    VM 2  keyspace evseb      (dht_entries + routing_table)
#   cassandra-vm.sh dht      VM 3  keyspace dhtbacked  (proof_entries + routing_table)
#
# One keyspace per VM and nothing else: the schema applied here is only that
# scheme's own file from this repository, so the keyspace definitions are the
# ones the unchanged fixtures expect. Idempotent: re-running keeps the data.
. "$(dirname "${BASH_SOURCE[0]}")/common.sh"

role="${1:-}"
case "$role" in
hmf) schema="$SYSTEMS_DIR/fair-persistence/docker/cassandra/init-hmf.cql"; keyspace=hmf_audit ;;
evseb) schema="$SYSTEMS_DIR/fair-persistence/docker/cassandra/init-evseb.cql"; keyspace=evseb ;;
dht) schema="$SYSTEMS_DIR/experiments/init_dht.cql"; keyspace=dhtbacked ;;
*) die "usage: cassandra-vm.sh hmf|evseb|dht" ;;
esac
[[ -f "$schema" ]] || die "missing $schema"

ensure_docker
ensure_data_dir cassandra
ip="$(internal_ip)"
note "Cassandra '$role' on $ip, heap $CLOUD_CASSANDRA_HEAP, data $FAIR_DATA_DIR/cassandra"

if sudo docker ps -a --format '{{.Names}}' | grep -qx fair-cassandra; then
    sudo docker start fair-cassandra >/dev/null
else
    # Host networking so the address Cassandra advertises to the driver is this
    # VM's private IP (a bridge network would advertise an unreachable container IP).
    sudo docker run -d --name fair-cassandra --restart unless-stopped --network host \
        -e CASSANDRA_CLUSTER_NAME=fair-cluster -e CASSANDRA_DC=datacenter1 \
        -e MAX_HEAP_SIZE="$CLOUD_CASSANDRA_HEAP" -e HEAP_NEWSIZE="$CLOUD_CASSANDRA_NEWSIZE" \
        -e CASSANDRA_LISTEN_ADDRESS="$ip" -e CASSANDRA_BROADCAST_ADDRESS="$ip" \
        -e CASSANDRA_RPC_ADDRESS=0.0.0.0 -e CASSANDRA_BROADCAST_RPC_ADDRESS="$ip" \
        -v "$FAIR_DATA_DIR/cassandra:/var/lib/cassandra" "$CASSANDRA_IMAGE" >/dev/null
fi

note "waiting for Cassandra to accept CQL (first start takes a minute or two)"
for ((i = 0; i < 60; i++)); do
    sudo docker exec fair-cassandra cqlsh "$ip" -e 'DESCRIBE KEYSPACES' >/dev/null 2>&1 && break
    sleep 5
done
sudo docker exec fair-cassandra cqlsh "$ip" -e 'DESCRIBE KEYSPACES' >/dev/null 2>&1 || die "Cassandra did not become ready"

note "applying $(basename "$schema")"
sudo docker exec -i fair-cassandra cqlsh "$ip" <"$schema"
sudo docker exec fair-cassandra cqlsh "$ip" -e "DESCRIBE KEYSPACE $keyspace" >/dev/null \
    || die "keyspace $keyspace was not created"

# EVSEB and DHT-backed model a 16-node DHT: every lookup first does ceil(log2 n)
# real reads of routing_table, which the benchmark seeds when it starts. Make
# sure the table exists even if the schema file on this VM predates it. This is
# the same definition as in the schema files, and it is idempotent.
case "$role" in
evseb | dht)
    note "ensuring $keyspace.routing_table"
    sudo docker exec fair-cassandra cqlsh "$ip" -e \
        "CREATE TABLE IF NOT EXISTS $keyspace.routing_table (peer int, level int, next_peer int, PRIMARY KEY (peer, level));"
    sudo docker exec fair-cassandra cqlsh "$ip" -e "DESCRIBE TABLE $keyspace.routing_table" >/dev/null \
        || die "$keyspace.routing_table is missing: DHT routing would be skipped"
    ;;
esac
note "done: keyspace $keyspace ready on $ip:9042. Allow port 9042 only from the runner (10.148.0.8)."
