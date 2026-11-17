-- Local fair-benchmark schemas that fair-persistence/docker/postgresql/001-fair-benchmark.sql
-- does not create: DHT-backed (schema dht; the fixture's Migrate creates the
-- records table inside it through search_path=dht) and BLAS (schema blas and
-- its entry_files table, which the fixture TRUNCATEs and COPYs into).
--
-- Local Docker only. Idempotent: CREATE ... IF NOT EXISTS.

CREATE SCHEMA IF NOT EXISTS dht;

CREATE SCHEMA IF NOT EXISTS blas;

CREATE TABLE IF NOT EXISTS blas.entry_files (
    lid     BIGINT NOT NULL,
    fid     BIGINT NOT NULL,
    payload BYTEA  NOT NULL,
    PRIMARY KEY (lid, fid)
);
