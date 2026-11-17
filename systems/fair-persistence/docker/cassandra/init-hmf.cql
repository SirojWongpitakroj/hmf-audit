-- ============================================================
-- HMF-Audit Cassandra Schema (fair-benchmark keyspace)
-- ============================================================
-- Copied from systems/hmf-audit/docker/cassandra/init.cql. Keyspace name
-- "hmf_audit" matches cassandrastore.NewSession(..., Keyspace: "hmf_audit")
-- in systems/hmf-audit/benchmarks/latency/persistence_fair_test.go.

CREATE KEYSPACE IF NOT EXISTS hmf_audit
WITH replication = {
    'class': 'NetworkTopologyStrategy',
    'datacenter1': 1
};

USE hmf_audit;

CREATE TABLE IF NOT EXISTS all_pages (
    tree_id         text,
    bucket_id       bigint,
    page_id         bigint,

    is_leaf         boolean,

    parent_page_id  bigint,
    next_page_id    bigint,

    page_data       blob,
    format_version  int,
    page_hash       blob,

    PRIMARY KEY ((tree_id, bucket_id), page_id)
);

CREATE TABLE IF NOT EXISTS all_tree_state (
    tree_id          text PRIMARY KEY,

    root_page_id     bigint,
    root_hash        blob,
    next_page_id     bigint,
    tree_height      int,
    tree_order       int,
    record_count     bigint,
    page_count       bigint,
    pages_per_bucket int,

    updated_at       timestamp
);

CREATE TABLE IF NOT EXISTS merkle_segment_nodes (
    region_id       text,
    shard_id        bigint,
    segment_id      bigint,

    level           int,
    node_index      bigint,

    node_hash       blob,

    PRIMARY KEY (
        (region_id, shard_id, segment_id),
        level,
        node_index
    )
)
WITH CLUSTERING ORDER BY (
    level ASC,
    node_index ASC
);

CREATE TABLE IF NOT EXISTS hmf_segments_by_shard (
    region_id        text,
    shard_id         bigint,
    segment_id       bigint,

    segment_root     blob,
    leaf_count       bigint,
    shard_leaf_index bigint,
    max_leaves       int,
    max_age_ms       bigint,

    start_ts         timestamp,
    end_ts           timestamp,
    sealed           boolean,
    created_at       timestamp,
    sealed_at        timestamp,

    PRIMARY KEY ((region_id, shard_id), segment_id)
)
WITH CLUSTERING ORDER BY (segment_id ASC);

CREATE TABLE IF NOT EXISTS shard_tree_frontiers (
    region_id       text,
    shard_id        bigint,
    level           int,

    node_index      bigint,
    node_hash       blob,

    PRIMARY KEY ((region_id, shard_id), level)
)
WITH CLUSTERING ORDER BY (level ASC);

CREATE TABLE IF NOT EXISTS upper_merkle_nodes (
    scope_type      text,
    scope_id        text,
    bucket_id       bigint,

    level           int,
    node_index      bigint,

    node_hash       blob,

    PRIMARY KEY (
        (scope_type, scope_id, bucket_id),
        level,
        node_index
    )
)
WITH CLUSTERING ORDER BY (
    level ASC,
    node_index ASC
);

CREATE TABLE IF NOT EXISTS tree_state_by_id (
    tree_id             text PRIMARY KEY,

    tree_type           text,

    region_id           text,
    shard_id            bigint,
    segment_id          bigint,

    current_root        blob,

    current_segment_id  bigint,
    next_segment_id     bigint,
    leaf_count          bigint,
    tree_height         int,
    parent_leaf_index   bigint,

    updated_at          timestamp
);

CREATE TABLE IF NOT EXISTS checkpoint_roots (
    system_id               text,
    checkpoint_id           timeuuid,
    checkpoint_sequence     bigint,

    locator_root            blob,
    global_hmf_root          blob,
    state_commitment         blob,

    blockchain_tx_hash      text,
    blockchain_block_height bigint,
    anchored_at             timestamp,
    finalized_at            timestamp,

    created_at              timestamp,

    PRIMARY KEY ((system_id), checkpoint_sequence)
)
WITH CLUSTERING ORDER BY (checkpoint_sequence DESC);

CREATE TABLE IF NOT EXISTS finality_state (
    system_id                   text PRIMARY KEY,

    finalized_checkpoint_id     timeuuid,
    checkpoint_sequence         bigint,

    locator_tree_id             text,
    locator_root                blob,
    global_hmf_root             blob,
    state_commitment            blob,

    blockchain_tx_hash          text,
    blockchain_block_height     bigint,
    anchored_at                 timestamp,
    finalized_at                timestamp,
    updated_at                  timestamp
);
