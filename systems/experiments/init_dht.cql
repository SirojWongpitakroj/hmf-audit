-- DHT-backed proof store for the local fair benchmark: keyspace "dhtbacked",
-- tables proof_entries and routing_table, exactly as
-- systems/dht-backed/internal/cassandradht expects. The local Docker
-- Cassandra only; idempotent.

CREATE KEYSPACE IF NOT EXISTS dhtbacked
WITH replication = {'class': 'NetworkTopologyStrategy', 'datacenter1': 1};

CREATE TABLE IF NOT EXISTS dhtbacked.proof_entries (
    proof_key blob PRIMARY KEY,
    payload   blob
);

CREATE TABLE IF NOT EXISTS dhtbacked.routing_table (
    peer      int,
    level     int,
    next_peer int,
    PRIMARY KEY (peer, level)
);
