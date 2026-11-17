-- Fair-benchmark PostgreSQL schema, one isolated schema per system.
-- Idempotent: safe to re-run against an existing "fair_benchmark" database
-- (the runner execs this file after "docker compose up" on every invocation,
-- since docker-entrypoint-initdb.d only fires on a brand-new data volume).

-- ============================================================
-- HMF-Audit: hmf.encrypted_logs
-- ============================================================
-- Matches systems/hmf-audit/benchmarks/latency/persistence_fair_test.go,
-- which COPYs (log_id, ciphertext, nonce, auth_tag, associated_data) and
-- reads/updates rows by log_id.

CREATE SCHEMA IF NOT EXISTS hmf;

CREATE TABLE IF NOT EXISTS hmf.encrypted_logs (
    log_id          UUID PRIMARY KEY,
    ciphertext      BYTEA NOT NULL,
    nonce           BYTEA NOT NULL,
    auth_tag        BYTEA NOT NULL,
    associated_data BYTEA
);

-- ============================================================
-- EVSEB: evseb.encrypted_logs, stored_tree_*
-- ============================================================
-- Matches systems/EVBES/internal/integrity/fair_benchmark_test.go.

CREATE SCHEMA IF NOT EXISTS evseb;

CREATE TABLE IF NOT EXISTS evseb.encrypted_logs (
    lid         TEXT PRIMARY KEY,
    ct_log      BYTEA NOT NULL,
    ct_metadata BYTEA
);

CREATE TABLE IF NOT EXISTS evseb.stored_tree_positions (
    lid     TEXT PRIMARY KEY,
    ordinal INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS evseb.stored_tree_nodes (
    level      INTEGER NOT NULL,
    node_index INTEGER NOT NULL,
    node_hash  BYTEA NOT NULL,
    PRIMARY KEY (level, node_index)
);

CREATE TABLE IF NOT EXISTS evseb.stored_tree_state (
    tree_id    INTEGER PRIMARY KEY,
    leaf_count INTEGER NOT NULL,
    root       BYTEA NOT NULL
);
