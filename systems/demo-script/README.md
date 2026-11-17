# Demo: audit a query by attribute, and tamper with a matching log (100,000 logs)

Give an attribute query; get back the **log count**, the **log IDs**, and one
status: **VALID** if verification passes, **REJECTED** if it fails.

```bash
# bash
./verification-query.sh --tenant T745 --service spark-executor --log-type UNSPECIFIED \
          --region R1 --at 2015-09-01T18:14:40Z
```

```powershell
# PowerShell
.\verification-query.ps1 -Tenant T745 -Service spark-executor -LogType UNSPECIFIED -Region R1 -At 2015-09-01T18:14:40Z
```

```
Query:     tenant=T745 service=spark-executor log_type=UNSPECIFIED region=R1 time=2015-09-01T18:14:40Z
Dataset:   100,000 ingested logs
Log count: 1
Log IDs:
  c9e0a6ac-22de-4de4-b713-101f7eb04e52
Status:    VALID  (all 1 log(s) verified against the anchored root)
```

A query that matches several logs, and what a failed verification looks like:

```bash
./verification-query.sh --tenant T246 --service spark-executor --log-type INFO --region R0 \
          --start 2017-03-01T00:00:00Z --end 2017-12-31T00:00:00Z
```
```
Log count: 42
Status:    REJECTED  (1 of 42 log(s) failed verification)
  rejected: ee28c9aa-...  stored log does not match the digest committed under the anchored root
```

## Tamper script: modify a log that the query returns

`tamper.sh` / `tamper.ps1` takes **the same query** and modifies **one existing
log that satisfies it**, in place, in the storage provider's table
(`hmfq.encrypted_logs`): one byte of the stored ciphertext is changed. No log is
inserted or removed, and the digest committed in the hierarchy is untouched, so
the verification script, run with the same query, now says REJECTED.

```bash
Q="--tenant T246 --service spark-executor --log-type INFO --region R0 --start 2017-03-01T00:00:00Z --end 2017-12-31T00:00:00Z"
./verification-query.sh $Q          # Status: VALID  (42 logs)
./tamper.sh $Q --verify                  # modifies the first matching log, then re-runs the check
./tamper.sh --restore                    # puts the original content back
```
```powershell
.\tamper.ps1 -Tenant T246 -Service spark-executor -LogType INFO -Region R0 -Start 2017-03-01T00:00:00Z -End 2017-12-31T00:00:00Z -Verify
.\tamper.ps1 -Restore
```

* Which log: the first match by default; `--index N` (`-Index N`) picks the N-th
  (0-based). The log always comes from the query's own result, so it satisfies
  the query by construction. A query that matches nothing is an error.
* One tampering at a time: the log ID and its original content hash are recorded
  in `.tamper-state` (git-ignored); a second tamper is refused until `--restore`.
  Restore checks the content hash and refuses if something else changed the row.
* Exit code: 0 done, 2 error. It changes only the local `hmfq.encrypted_logs`
  table in Docker; it never touches the benchmark schemas, the other paper's
  data or AWS.

## Find-tampered script: correct content vs tampered content

`find-tampered.sh` / `find-tampered.ps1` takes the same query, runs the
verification, and for every log that fails shows what it should contain next to
what it contains now.

```bash
./find-tampered.sh $Q
```
```
Tampered log 1 of 1
  Log ID:    98041e56-15a7-4220-92c6-f6466d0df7d7
  Location:  shard 0, segment 0, leaf 6579  (tenant=T246 service=spark-executor type=INFO region=R0 time=2017-04-13T20:56:22Z)
  Correct content (the log as ingested, from the source dataset):
      Update row: 501601
  Tampered content (the stored row, decrypted without authentication):
      \xaapdate row: 501601
  Differs at byte offset(s): 0
  Correct digest  (committed under the anchored root): 68bff329a872f46110a13886a336879757242c5e148e8b00bae7d75f11df7c03
  Tampered digest (digest of the row stored now):      10ba8b796d409ff5bac87b01249a0ccced847cba2ded36416be4ba83cb7124ac
```

* **Correct content** is read, by log ID, from the source dataset
  (`systems/experiments/datasets/spark_stratified_100000.jsonl`), the log
  generator's copy. The auditor and the provider's table do not hold it: the
  provider's copy is the one that was altered.
* **Tampered content** is the stored row decrypted without checking its
  authentication tag (possible because AES-GCM is counter mode underneath). It
  needs the key in `systems/attribute-query/keys/`; it is shown only to illustrate
  the change and is not trustworthy. Without the key it says so.
* **The digests are always shown.** *Correct digest* is the one the hierarchy
  committed for the log (I checked it equals the digest of the original row,
  computed independently in SQL). *Tampered digest* is the digest of the row
  stored now. Verification compares exactly these two, so they differ whenever
  the content, nonce, tag or attributes were changed, even if no content can be read.
* Exit code: 0 no tampered log, 1 tampered log(s) found, 2 error. `--json` / `-Json`
  prints the same report as JSON.

## Scripts in this folder

| Script (bash and PowerShell) | What it does |
|---|---|
| `verification-query` | audit a query: log count, log IDs, VALID or REJECTED |
| `tamper` | modify one existing log that satisfies the query (`--restore` undoes it) |
| `find-tampered` | list the failed logs: correct content vs tampered content, plus digests |

Helpers: `common.sh` / `common.ps1` (shared query handling), `format_result.py`,
`pick_target.py`, `find_tampered.py`.

## Input

All five attributes are required and must match exactly (no wildcards): tenant,
service, log type, region, and a time, either one instant (`--at`) or a range
(`--start`, `--end`; start included, end excluded). Times are RFC 3339 in UTC
(`2015-09-01T18:14:40Z`). Take values from any line of
`systems/experiments/datasets/spark_stratified_100000.jsonl` (`tenant_id`,
`service_id`, `log_type`, `region_id`, `event_time`).

Other options: `--max-ids N` (IDs printed; default 100, `-1` for all), `--json`
(one JSON object with `log_count`, `status`, `log_ids`, `rejected`).

## Output and exit code

| Status | Meaning | Exit code |
|---|---|---|
| VALID | the locator proof verified and every matching log hashes to the leaf committed under the anchored root | 0 |
| VALID with count 0 | nothing matches; the locator proof shows nothing was left out | 0 |
| REJECTED | the locator proof failed, or at least one log failed verification (its ID and reason are listed) | 1 |
| (error) | bad input, database unreachable, nothing ingested; no status is printed | 2 |

## Requirements

* The 100k ingest in the local Docker stack: `systems/attribute-query/ingest.sh
  --records 100000` (the demo checks the database holds exactly 100,000 logs and
  stops with that command if not).
* Go, Docker, Python 3.9+. The demo builds `cmd/hmf-auditor` into
  `systems/attribute-query/bin/` and runs it; it talks to the local database only.

How the verification works is in `systems/attribute-query/README.md`.
