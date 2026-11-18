#!/usr/bin/env bash
# Checks of the cloud deployment, run from the benchmark runner.
#
#   check.sh pre     before any run: tools, reachability, one keyspace/schema per VM,
#                    DHT routing switched on, anchor node answering
#   check.sh post    after ingest/seeding: the four HMF regions are separate
#                    partitions in the HMF-Audit Cassandra, the EVSEB and
#                    DHT-backed routing tables hold every routing row, and the
#                    DHT stores are populated
#
# Exit status 0 only if every check passes. Nothing here modifies a database.
set -euo pipefail
export FAIR_CLOUD=1
. "$(dirname "${BASH_SOURCE[0]}")/../experiments/scripts/lib.sh"

mode="${1:-}"
[[ "$mode" == pre || "$mode" == post ]] || die "usage: check.sh pre|post"

failed=0
ok() { echo "  ok:   $*" >&2; }
bad() { echo "  FAIL: $*" >&2; failed=1; }

KEYSPACES=(hmf_audit evseb dhtbacked)
PEERS="$DHT_PEERS"
LEVELS="$(routing_levels "$PEERS")"

# cql_value <host> <query>: first bare integer in the cqlsh output (COUNT results).
cql_count() { cloud_cql "$1" -e "$2" 2>/dev/null | awk '/^ *[0-9]+ *$/ {print $1; exit}'; }

check_cassandra() { # name host own-keyspace
    local name="$1" host="$2" own="$3" listing keyspace
    if ! listing="$(cloud_cql "$host" -e 'DESCRIBE KEYSPACES' 2>&1)"; then
        bad "$name Cassandra $host:$CLOUD_CASSANDRA_PORT is unreachable: $(echo "$listing" | tail -1)"
        return
    fi
    ok "$name Cassandra $host reachable"
    if grep -qw "$own" <<<"$listing"; then ok "$name Cassandra has keyspace $own"; else bad "$name Cassandra lacks keyspace $own (run provision/cassandra-vm.sh)"; fi
    for keyspace in "${KEYSPACES[@]}"; do
        [[ "$keyspace" == "$own" ]] && continue
        if grep -qw "$keyspace" <<<"$listing"; then bad "$name Cassandra also holds keyspace $keyspace; every scheme must have its own VM"; fi
    done
}

check_postgres() { # name host "wanted schemas" "forbidden schemas"
    local name="$1" host="$2" wanted="$3" forbidden="$4" schemas schema
    if ! schemas="$(cloud_psql "$host" -c "SELECT schema_name FROM information_schema.schemata" 2>&1)"; then
        bad "$name PostgreSQL $host:$CLOUD_PG_PORT is unreachable: $(echo "$schemas" | tail -1)"
        return
    fi
    ok "$name PostgreSQL $host reachable"
    for schema in $wanted; do
        if grep -qx "$schema" <<<"$schemas"; then ok "$name PostgreSQL has schema $schema"; else bad "$name PostgreSQL lacks schema $schema (run provision/postgres-vm.sh)"; fi
    done
    for schema in $forbidden; do
        if grep -qx "$schema" <<<"$schemas"; then bad "$name PostgreSQL also holds schema $schema, which belongs on another VM"; fi
    done
}

if [[ "$mode" == pre ]]; then
    echo "cloud preflight" >&2
    for tool in docker psql curl go; do
        command -v "$tool" >/dev/null 2>&1 && ok "$tool installed" || bad "$tool is not installed (run provision/runner-vm.sh)"
    done
    case "$(uname -m)" in
    aarch64 | arm64) ok "runner architecture $(uname -m)" ;;
    *) echo "  warn: runner architecture is $(uname -m); the cloud plan uses Arm (t2a)" >&2 ;;
    esac
    # FAIR_DHT_PEERS=1 turns the DHT routing reads off for EVSEB and DHT-backed.
    if ((PEERS >= 2)); then
        ok "DHT routing on: $PEERS logical nodes, $LEVELS routing reads per lookup"
    else
        bad "DHT_PEERS=$PEERS switches DHT routing off; it must be at least 2"
    fi
    check_cassandra "HMF-Audit" "$CLOUD_HMF_CASSANDRA_HOST" hmf_audit
    check_cassandra "EVSEB" "$CLOUD_EVSEB_CASSANDRA_HOST" evseb
    check_cassandra "DHT-backed" "$CLOUD_DHT_CASSANDRA_HOST" dhtbacked
    if cloud_cql "$CLOUD_EVSEB_CASSANDRA_HOST" -e 'DESCRIBE TABLE evseb.routing_table' >/dev/null 2>&1; then ok "evseb.routing_table exists"; else bad "evseb.routing_table is missing"; fi
    if cloud_cql "$CLOUD_DHT_CASSANDRA_HOST" -e 'DESCRIBE TABLE dhtbacked.routing_table' >/dev/null 2>&1; then ok "dhtbacked.routing_table exists"; else bad "dhtbacked.routing_table is missing"; fi
    check_postgres "shared cloud-provider" "$CLOUD_SHARED_PG_HOST" "hmf evseb dht" "blas"
    check_postgres "BLAS" "$CLOUD_BLAS_PG_HOST" "blas" "hmf evseb dht"
    anchor="http://$CLOUD_ANCHOR_HOST:$CLOUD_ANCHOR_PORT"
    chain="$(curl -fsS -m 10 -X POST -H 'Content-Type: application/json' \
        --data '{"jsonrpc":"2.0","id":1,"method":"eth_chainId","params":[]}' "$anchor" 2>&1 || true)"
    if grep -q '"0x7a69"' <<<"$chain"; then ok "anchor node $anchor answers (chain id 31337)"; else bad "anchor node $anchor does not answer as chain 31337: $chain"; fi
else
    echo "cloud post-ingest verification" >&2
    # --- HMF-Audit: four regions, logically separate partitions in ONE keyspace.
    rows="$(cloud_cql "$CLOUD_HMF_CASSANDRA_HOST" -e 'SELECT DISTINCT region_id, shard_id FROM hmf_audit.hmf_segments_by_shard' 2>/dev/null \
        | awk -F'|' 'NF == 2 && $1 !~ /region_id/ { gsub(/ /, "", $1); gsub(/ /, "", $2); print $1 " " $2 }' | sort)"
    expected_rows=$((REGIONS * SHARDS_PER_REGION))
    found_rows="$(grep -c . <<<"$rows" || true)"
    found_regions="$(awk 'NF {print $1}' <<<"$rows" | sort -u | tr '\n' ' ')"
    wanted_regions="$(for ((r = 0; r < REGIONS; r++)); do printf 'R%d ' "$r"; done)"
    if [[ "$found_rows" == "$expected_rows" && "$found_regions" == "$wanted_regions" ]]; then
        ok "HMF-Audit Cassandra: regions ${found_regions}each with $SHARDS_PER_REGION shard(s) as separate (region_id, shard_id) partitions"
    else
        bad "HMF-Audit Cassandra: expected regions ${wanted_regions}x $SHARDS_PER_REGION shard(s) = $expected_rows partitions, found $found_rows (${found_regions})"
    fi
    # --- EVSEB and DHT-backed: every routing row present, so lookups pay the routing reads.
    expected_routing=$((PEERS * LEVELS))
    for pair in "evseb|$CLOUD_EVSEB_CASSANDRA_HOST|evseb.routing_table|SELECT lid_hash FROM evseb.dht_entries LIMIT 1" \
        "dhtbacked|$CLOUD_DHT_CASSANDRA_HOST|dhtbacked.routing_table|SELECT proof_key FROM dhtbacked.proof_entries LIMIT 1"; do
        IFS='|' read -r label host table probe <<<"$pair"
        have="$(cql_count "$host" "SELECT COUNT(*) FROM $table")"
        if [[ "$have" == "$expected_routing" ]]; then
            ok "$table holds $have routing rows ($PEERS nodes x $LEVELS levels)"
        else
            bad "$table holds '${have:-none}' routing rows, expected $expected_routing ($PEERS nodes x $LEVELS levels); routing would be skipped or wrong"
        fi
        if cloud_cql "$host" -e "$probe" 2>/dev/null | grep -q '(1 rows)'; then ok "$label store is populated"; else bad "$label store is empty"; fi
    done
fi

if ((failed)); then
    echo "cloud check ($mode): FAILED" >&2
    exit 1
fi
echo "cloud check ($mode): all checks passed" >&2
