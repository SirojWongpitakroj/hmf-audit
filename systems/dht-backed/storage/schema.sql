-- Cloud data lake schema, exactly as Q43 specifies.
--
-- All cryptographic byte strings are raw BYTEA; they are never stored as hex
-- or base64 text.
--
-- Exactly one additional B-tree constraint is authorized:
-- UNIQUE (epoch, log_type, leaf_index). This is NAMED EXCEPTION 1 in
-- REQUIREMENTS.md §0.6. Its purpose is correctness: it enforces the Q44
-- leaf-position invariant. No other secondary index exists; in particular the
-- (epoch, log_type) index considered under Q43 was NOT approved and must not
-- be created.

CREATE TABLE IF NOT EXISTS records (
    record_id            TEXT   PRIMARY KEY,
    epoch                BIGINT NOT NULL,
    log_type             TEXT   NOT NULL,
    leaf_index           BIGINT NOT NULL,
    producer_id          TEXT   NOT NULL,
    timestamp_ns         BIGINT NOT NULL,
    c                    BYTEA  NOT NULL,
    t                    BYTEA  NOT NULL,
    ad                   BYTEA  NOT NULL,
    d                    BYTEA  NOT NULL,
    sigma                BYTEA  NOT NULL,
    producer_public_key  BYTEA  NOT NULL,
    UNIQUE (epoch, log_type, leaf_index)
);
