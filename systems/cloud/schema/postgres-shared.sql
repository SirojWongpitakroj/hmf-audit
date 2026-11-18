-- Shared cloud-provider PostgreSQL VM. Applied after
-- fair-persistence/docker/postgresql/001-fair-benchmark.sql, which creates the
-- hmf and evseb schemas. This file adds the dht schema (the DHT-backed
-- fixture's Migrate creates its records table inside it through
-- search_path=dht). The blas schema is deliberately NOT created here: BLAS has
-- its own PostgreSQL VM. Idempotent.
CREATE SCHEMA IF NOT EXISTS dht;
