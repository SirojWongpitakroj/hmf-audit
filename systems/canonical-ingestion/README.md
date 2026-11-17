# Shared canonical ingestion contract

The single-command live producer/consumer supervisor is documented in
[SINGLE_COMMAND_INGESTION.md](../SINGLE_COMMAND_INGESTION.md). It runs all four
existing bounded pipelines concurrently and automatically publishes finalized
MVA prefixes; standalone `Consumer.Run` retains its original manual behavior.

This module contains no experiment configuration or authentication algorithm.
The stream is an append-only PostgreSQL prefix published from MVA-PHV's finalized
ingestion ledger, not from a second independent selection pass. Each entry has
its cumulative ordinal, exact canonical JSON bytes, SourceObservationID,
SHA-256 payload digest and chained prefix digest. The stream fingerprint binds
the prepared source index and selection policy version. Payload order matters.

Each baseline stores its cursor, immutable configuration binding, source/native
ID mapping and native checkpoint in its own database. `Consumer.Step` locks the
cursor and commits the native adapter's SQL writes together with the mappings
and cursor. Adapters must use the supplied transaction. They must not perform
untracked external writes or retain mutations in RAM after a rollback. DHT
publication requires a durable outbox and reconciliation before completion.

`Run` accepts exactly one of `1M`, `10M`, `50M`, `100M`, `250M`. The previous
milestone must have completed first. It stops at the selected total, including
when the configured batch crosses that total. It never selects the next mode.
`Step` is also available for orchestration and recovery testing. A source prefix
that is not yet published causes a clear error; it does not invent records.

Persistent storage grows with N: stream entries, provenance mappings and native
scheme state. Application RAM holds only the configured row/byte-limited batch,
bounded checkpoint and the adapter's bounded working set. Use distinct source
and destination SQL pools (or at least two source-pool connections): a consumer
transaction must not deadlock its own source reads. No complete source-ID set
is loaded in RAM. Native-ID/source uniqueness is enforced in PostgreSQL.

The schema is append-only through this API. Operational roles should prohibit
UPDATE/DELETE on `entries` and `mappings`; a database administrator is part of
the storage trust boundary. Hash checks detect drift at consumption time, but
are not substitutes for each baseline's native authentication verification.

Tests use `CANONICAL_TEST_POSTGRES_DSN` for an isolated, disposable database.
They never ingest raw datasets. Do not point the integration tests at live data.
