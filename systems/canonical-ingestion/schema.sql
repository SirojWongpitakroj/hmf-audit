CREATE SCHEMA IF NOT EXISTS canonical_ingestion;
-- Append-only logical stream, independent of a scheme's native log IDs.
CREATE TABLE IF NOT EXISTS canonical_ingestion.streams (
 stream_id text PRIMARY KEY,
 fingerprint text NOT NULL,
 committed bigint NOT NULL DEFAULT 0 CHECK(committed >= 0),
 prefix_hash bytea NOT NULL CHECK(octet_length(prefix_hash)=32)
);
CREATE TABLE IF NOT EXISTS canonical_ingestion.entries (
 stream_id text NOT NULL REFERENCES canonical_ingestion.streams,
 ordinal bigint NOT NULL CHECK(ordinal > 0),
 source_id text NOT NULL,
 payload bytea NOT NULL,
 payload_hash bytea NOT NULL CHECK(octet_length(payload_hash)=32),
 prefix_hash bytea NOT NULL CHECK(octet_length(prefix_hash)=32),
 -- No UNIQUE(stream_id,source_id): random source IDs made that index the
 -- dominant publication cost. Readers seek by ordinal only. Source uniqueness is
 -- enforced by the MVA journal primary key and each consumer's mappings.
 PRIMARY KEY(stream_id,ordinal)
);
-- Consumers live in EACH baseline's database. Native writes, mappings, and
-- cursor publication must share one transaction; no DB-to-file dual write.
CREATE TABLE IF NOT EXISTS canonical_ingestion.consumers (
 consumer_id text PRIMARY KEY,
 stream_id text NOT NULL,
 fingerprint text NOT NULL,
 config_hash text NOT NULL,
 deferred_completion boolean NOT NULL DEFAULT false,
 committed bigint NOT NULL DEFAULT 0 CHECK(committed >= 0),
 target bigint NOT NULL DEFAULT 0,
 prefix_hash bytea NOT NULL CHECK(octet_length(prefix_hash)=32),
 native_checkpoint bytea NOT NULL DEFAULT ''::bytea
);
ALTER TABLE canonical_ingestion.consumers ADD COLUMN IF NOT EXISTS deferred_completion boolean NOT NULL DEFAULT false;
CREATE TABLE IF NOT EXISTS canonical_ingestion.mappings (
 consumer_id text NOT NULL REFERENCES canonical_ingestion.consumers,
 ordinal bigint NOT NULL CHECK(ordinal > 0),
 source_id text NOT NULL,
 native_id text NOT NULL,
 payload_hash bytea NOT NULL CHECK(octet_length(payload_hash)=32),
 PRIMARY KEY(consumer_id,ordinal),
 UNIQUE(consumer_id,source_id), UNIQUE(consumer_id,native_id)
);
CREATE TABLE IF NOT EXISTS canonical_ingestion.stages (
 consumer_id text NOT NULL REFERENCES canonical_ingestion.consumers,
 target bigint NOT NULL CHECK(target IN (1000000,10000000,50000000,100000000,250000000)),
 started_total bigint NOT NULL,
 completed_at timestamptz,
 PRIMARY KEY(consumer_id,target)
);
