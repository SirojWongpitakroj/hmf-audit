-- ============================================================
-- EVSEB Cassandra DHT Schema (fair-benchmark keyspace)
-- ============================================================
-- Keyspace "evseb", table "dht_entries" match
-- systems/EVBES/internal/integrity/fair_dht_test.go:
--   fairEVBESDHTKeyspace = "evseb"
--   SELECT h_lid FROM dht_entries WHERE lid_hash = ?
--   INSERT INTO dht_entries (lid_hash, h_lid) VALUES (?, ?)
-- lid_hash is Key(lid) = hex(SHA-256(lid)) (internal/integrity/integrity.go),
-- a 64-character hex string; h_lid is the 32-byte leaf digest.

CREATE KEYSPACE IF NOT EXISTS evseb
WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'datacenter1': 1
};

USE evseb;

CREATE TABLE IF NOT EXISTS dht_entries (
    lid_hash text PRIMARY KEY,
    h_lid    blob
);
