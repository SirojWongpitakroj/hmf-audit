-- BLAS PostgreSQL VM: only the blas schema. Idempotent.
-- entry_files is what the BLAS fixture TRUNCATEs and COPYs into; the fixture
-- creates its other blas.* tables itself.
CREATE SCHEMA IF NOT EXISTS blas;

CREATE TABLE IF NOT EXISTS blas.entry_files (
    lid     BIGINT NOT NULL,
    fid     BIGINT NOT NULL,
    payload BYTEA  NOT NULL,
    PRIMARY KEY (lid, fid)
);
