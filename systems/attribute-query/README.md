# Query by attribute, audited by HMF-Audit

Ingest logs through the real HMF-Audit write path, then ask for logs by
**tenant, service, log type, region and time**. Every matching log comes back
**VALID** or **REJECTED**.

Local Docker only (`systems/fair-persistence`: PostgreSQL `127.0.0.1:55432`,
Cassandra `127.0.0.1:19042`). The commands refuse any non-local host.

## Run

```bash
# bash
./ingest.sh --records 100000            # or 10000000; any other number is rejected
./query.sh --tenant T745 --service spark-executor --log-type UNSPECIFIED \
           --region R1 --at 2015-09-01T18:14:40Z
```

```powershell
# PowerShell
.\ingest.ps1 -Records 100000
.\query.ps1 -Tenant T745 -Service spark-executor -LogType UNSPECIFIED -Region R1 -At 2015-09-01T18:14:40Z
```

All five attributes are required (a time is a point `-At`, or a range `-Start`/`-End`;
the range is half-open: `start` included, `end` excluded). Other options:
`--limit N` rows printed (default 20; the summary counts all), `--json` for one
JSON line per log. Exit code: 0 all VALID, 1 at least one REJECTED, 2 error.

## What ingestion does (`systems/hmf-audit/cmd/hmf-ingest`)

For each record: seal the log with AES-256-GCM (the log's attributes and ID are
the authenticated data); store the row in PostgreSQL (`hmfq.encrypted_logs`);
compute its leaf digest over ciphertext, tag, nonce and associated data; submit
it to the hierarchy manager (segment -> shard -> region -> global tree); record
attributes -> physical address in the Authenticated Log Locator (ALL). At the
end it persists the hierarchy and locator to Cassandra (keyspace `hmf_audit_q`)
and finalizes one checkpoint binding the locator root `R_L` and global root `R_G`
(the anchor is the repository's mock anchor, not a blockchain).

Separate from the benchmarks: its own PostgreSQL schema `hmfq` and Cassandra
keyspace `hmf_audit_q`, so the experiment scripts (which truncate their own
schemas) do not touch it, and it does not touch theirs.

The encryption key is created on first ingest in `keys/hmf-ingest.key`
(git-ignored). Keep it private; it is what lets `query` show VALID logs decrypted.

## What the audit does (`systems/hmf-audit/cmd/hmf-auditor`)

1. Reads the current finalized checkpoint (`R_L`, `R_G`).
2. Resolves the attribute query through the locator and verifies its range proof
   against `R_L`. The proof also shows no matching log was omitted. If it does
   not verify, nothing is reported VALID.
3. Reads each matching log's **current** row from PostgreSQL, recomputes its
   digest, builds the HMF proof from Cassandra, and checks that the
   reconstructed root equals `R_G`. A failing group is split until the rejected
   logs are isolated.

A log is REJECTED when its stored row no longer hashes to the committed leaf
(altered ciphertext, tag, nonce or attributes), its stored attributes differ from
the locator entry, or it is missing from the database.

## Limits

* Queries are exact matches on tenant, service, log type and region plus a time
  range; there are no wildcards or partial queries. This is how the locator is
  keyed (`internal/all/locator_key.go`).
* The ingest is one pass and expects an empty database (`--reset` empties this
  tool's tables). It is not an incremental or concurrent writer.
* 10,000,000 records builds the whole locator tree in memory first; it needs
  several GB of RAM and has not been run.
