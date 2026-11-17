-- Legacy fixture bootstrap. Active ingestion creates missing schema using
-- internal/cassandradht/deployment.json (NetworkTopologyStrategy, configured RF).
-- Do not apply the fixture RF=1 setting to the production cluster.
CREATE KEYSPACE IF NOT EXISTS dhtbacked WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};
CREATE TABLE IF NOT EXISTS dhtbacked.proof_entries (
    proof_key blob PRIMARY KEY,
    payload blob
);
CREATE TABLE IF NOT EXISTS dhtbacked.routing_table (
    peer int,
    level int,
    next_peer int,
    PRIMARY KEY (peer, level)
);
CREATE TABLE IF NOT EXISTS dhtbacked.store_identity (
    singleton text PRIMARY KEY,
    identity blob
);
