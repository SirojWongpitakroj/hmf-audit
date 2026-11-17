-- Schema for the attribute-queryable HMF-Audit ingest (separate from the
-- benchmark schemas hmf / evseb / dht / blas, which the benchmarks truncate).
CREATE SCHEMA IF NOT EXISTS hmfq;

CREATE TABLE IF NOT EXISTS hmfq.encrypted_logs (
    provider_id     BIGSERIAL PRIMARY KEY,
    log_id          UUID UNIQUE NOT NULL,
    ciphertext      BYTEA NOT NULL,
    nonce           BYTEA NOT NULL,
    auth_tag        BYTEA NOT NULL,
    associated_data BYTEA,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
