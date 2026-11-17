# Active logical DHT and Cassandra deployment

The incremental runner and Experiment 1B use **a four-node logical private-DHT
model backed by persistent Cassandra storage, with relational baseline state
stored in the shared PostgreSQL `baselines_db`**. The four private hosts in
`deployment.json` are Cassandra contact points, not physical DHT peers.
`internal/peerdht` remains an alternative/reference implementation.

Experiment 1B overrides only the audit model to 1,024 logical nodes and adds a
default **387 microsecond simulated RTT per logical hop**, including cache hits.
The audit flag `-peer-rtt 387us` is additional to measured Cassandra latency;
`-peer-rtt 0` disables simulation. Ingestion has no added delay. Requested and
observed waits are reported separately because timers may overshoot. Native
configuration hashes, routing identities and persisted proofs are unchanged.
See [the audit routing policy](../../../DHT_LOGICAL_ROUTING.md).

## Configuration and storage ownership

Run commands from `dht-backed`. The default `-dht-config` is
`internal/cassandradht/deployment.json`, containing four logical identities,
contact points `10.10.1.66`, `10.10.1.194`, `10.10.1.205`, `10.10.1.58`, port 9042,
and keyspace `dhtbacked`. The public CA path is relative to this JSON file.
Provision keys once with:

```sh
go run ./benchmark/cmd/incremental_runner -prepare-keys -key-dir ./dht-live-keys
```

Preserve this directory for every restart and milestone. Configure independent
baseline credentials through runner-side `BASELINES_POSTGRES_*`; database must
be `baselines_db`. Do not use MVA-PHV CP/checkpoint credentials as a fallback.
The runner connects to PostgreSQL and Cassandra; Cassandra needs no PostgreSQL
credentials. Private runner-to-Cassandra connectivity on 9042 is required.

PostgreSQL owns `dhtbacked.records`, canonical native-ID mappings/cursors,
native checkpoint, unpublished epoch outbox and committed epoch anchors.
DELIA and BLAS retain their isolated `delia` and `blas` schemas; shared provenance
uses `canonical_ingestion`. No separate baseline databases are created.

Cassandra owns serialized native P/C/R/M objects in `proof_entries`, eight
fixed logical routing rows in `routing_table`, and one run/stream/configuration
binding in `store_identity`. No historical proof objects are stored in PostgreSQL.
Ingestion creates missing Cassandra schema only, using NetworkTopologyStrategy,
RF=1 (matching MVA-PHV's single-node regional keyspaces) and durable writes.
Existing keyspace replication is not changed by code; the audit runner's
`-expect-replication-factor` refuses a mismatched live keyspace. To reduce an
existing RF=3 keyspace, follow `EXP1B_FAIRNESS_CORRECTIONS.md`. Set
`LocalDC` explicitly when appropriate; otherwise schema creation reads the
connected server's data center. Reads/writes use LOCAL_QUORUM; immutable LWT
publication uses LOCAL_SERIAL. The audit command performs no schema creation.

## Native algorithm and recovery

Each lookup retains SHA-256 key ownership modulo four, the existing persisted
bit-routing model, token/envelope authorization, native parent/certificate keys,
and ancestor-assisted verification. There are exactly two routing-row reads per
successful logical lookup, including cache hits. These are modeled routing
steps, not physical HTTP hops. Cassandra partition placement is independent of
logical key ownership. All P/C/R/M serialization and Merkle semantics remain
unchanged. PostgreSQL root anchors remain the existing ledger emulator.

The canonical path is unchanged:

1. Commit encrypted records, native IDs, source mappings and cursor atomically.
2. Recover sealed unpublished epochs from persisted ciphertext digests, using
   a disk spool and bounded reads rather than replaying raw input.
3. Publish immutable Cassandra objects. Identical replay is accepted; conflicting
   proof bytes fail. Wait for all writes before continuing.
4. Commit epoch anchor and outbox completion in one PostgreSQL transaction.
5. Complete the requested cumulative stage only after publication is ready.

```sh
go run ./benchmark/cmd/incremental_runner -env-file ../MVA-PHV/.env.local -run comparison-v1 -mode 1M
```

This stops at the selected target. Later `-mode 10M` resumes it; it does not run
automatically. The legacy JSONL ingestion command is not used. A versioned
backend binding rejects old peer-backed cursors and mismatched/absent Cassandra
identity. There is no automatic peer-to-Cassandra migration or reset. Restore
matching proof storage or perform a separately reviewed explicit migration.

## Bounded memory and telemetry

Defaults: 8 workers; 32,768 proof requests; 64 KiB publication/lookup queue;
64 KiB maximum value; 8 MiB proof workspace; 32 MiB ancestor-map workspace;
256-record fetch batches; 8 MiB fetched-record budget. One reusable LRU cache
is limited to 64 MiB of accounted bytes and 65,536 entries, whichever is reached
first. Accounting includes payload plus fixed metadata allowance; actual Go RSS
also includes allocator/runtime overhead and is measured separately. Setting
either cache limit to zero disables caching. Ingestion never warms the query
cache. Persistent history grows on disk, not in application maps.

Raw reports retain per-owner lookups, routing-row reads, proof reads, payload
bytes, cache hits/misses/occupancy, ancestor groups, reuse and path reconstruction,
SQL record retrievals, latency, evidence size and application RSS. Byte counters
exclude protocol framing and relational control-row bytes. Database server RSS
is not included in application RSS.

## Validation

Unit tests cover deterministic ownership, cache/queue/request/value limits,
authorization on cache hits, immutable replay and failure propagation. With
`DHT_TEST_POSTGRES_DSN`, `TestLogicalOutboxRecovery` uses a disk-backed proof test
double and a fresh child process to exercise publication-before-anchor failure,
cursor recovery, canonical identity, unchanged ciphertexts, native root equality,
ancestor reuse and corrupt/missing evidence rejection. This is not a Cassandra
integration result.

Set `DHT_TEST_CASSANDRA_HOSTS` to comma-separated reachable test contact points
to enable actual Cassandra close/reopen and immutable-publication tests.
With both variables set, `TestRealCassandraOutboxRecovery` exercises the complete
outbox path against Cassandra. Tests create isolated test keyspaces/databases.
No live Cassandra or real 1M memory pass is implied by skipped integration tests.
