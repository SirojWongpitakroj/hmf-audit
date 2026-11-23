# Local experiment scripts

Three experiment scripts and one runner, each in PowerShell (`.ps1`) and bash
(`.sh`). They drive the **unchanged** fair-benchmark fixtures of HMF-Audit,
EVSEB, DHT-backed and BLAS against the local Docker stack in
`systems/fair-persistence` (PostgreSQL `127.0.0.1:55432`, Cassandra
`127.0.0.1:19042`). The endpoints are fixed in `lib.ps1` / `lib.sh`; nothing here
can be pointed at a remote database.

| Script | Experiment | Output (in `<output>/<name>/`) |
|---|---|---|
| `exp1_verification` | Verification latency: HMF-Audit, DHT-backed, BLAS, EVSEB; native clustered and scattered queries, 5 per cell; maximum q is 1024 for BLAS and EVSEB, 4096 for HMF-Audit and DHT-backed | raw `*.txt`, `latency_trials.csv`, `latency_summary.csv`, `figure1_verification_latency.png` |
| `exp2_localization` | Tamper localization latency: HMF-Audit vs EVSEB, one tampered record; the native HMF-Audit batches (clustered and scattered) for both systems; q = 1 … 4096 | raw `*.txt`, `latency_trials.csv`, `latency_summary.csv`, `figure2_localization_latency.png` |
| `exp2b_localization` | Experiment 2B, corruption density and locality: ACHP vs fixed-depth HMF at q = 1024, m = 1, 4, 16, 64 corrupted records, clustered or scattered, on the same corruption; 5 fixed query sets, 3 warm-up + 20 measured per set | `exp2b/manifests/{queries,corruption}.json`, `exp2b/results/{raw.csv,summary.csv,run_config.json}` (no figure) |
| `exp3_ablation` | HMF-Audit ablation: Full, no shared planning, no deduplication, no partition-aware retrieval; q = 256 … 4096 | `ablation_latency_raw.csv`, `ablation_summary.csv`, `figure3_ablation_latency.png` |
| `run_all` | Runs the three above in order, then compares with the earlier 10M results | all of the above, plus the comparison on the console |

## Usage

```powershell
# PowerShell (from systems\experiments\scripts)
.\run_all.ps1                                   # existing 10M subset, 30 repetitions, 5 warm-ups
.\exp1_verification.ps1 -Systems hmf-audit,evseb
.\run_all.ps1 -PerRegion 125000 -Count 3 -Warmup 1 -BenchTime 50ms   # small smoke run
```

```bash
# bash (Git Bash, WSL, Linux, macOS), from systems/experiments/scripts
./run_all.sh
./exp1_verification.sh --systems hmf-audit,evseb
./run_all.sh --per-region 125000 --count 3 --warmup 1 --bench-time 50ms
```

### One command for the whole 10M study

```powershell
.\run_pipeline.ps1                  # preflight -> ingest 10M -> exp1, exp2, exp3 -> Figures 1-3
.\run_pipeline.ps1 -SkipIngest      # same, without the (redundant) separate ingest step
```

```bash
./run_pipeline.sh
./run_pipeline.sh --skip-ingest
```

Fixed to the 10,000,000-record dataset. It checks Python + matplotlib, the
dataset SHA-256 and Docker first, so a missing dependency fails before the long
run rather than at the figures. Experiment 1 runs all four systems on the native
clustered/scattered query sets of `native-queries/<records>/` (bash scripts only; the
PowerShell scripts still use the older fixed placements); DHT-backed is capped at
q = 1024 (see "Known limitations"). Override with `--systems` / `-Systems`. The three
figures are copied to `results/spark-10000000/figures/` and the whole run is
logged to `results/pipeline-<records>-<timestamp>.log`. Other options: count, warmup,
bench-time, no-compare.

Options (same meaning in both): count, warmup, bench-time, per-region, dataset,
output, and (experiment 1 / runner only) systems. `run_all` also takes
`-NoCompare` / `--no-compare`.

Requirements: Go 1.27, Docker with Compose, Python 3.9+ with `matplotlib`
(figures only), and for a regenerated subset the 12 GB source file
`dataset/combined/spark_uniform_combined.jsonl`.

## Experiments 1 and 3: the native query set

Experiment 1 (all four schemes) and experiment 3 (HMF-Audit ablation) run the
query set in [`native-queries/`](native-queries/README.md), at q = 1, 16, 64,
256, 1024, 4096 (experiment 1 omits 4096 for BLAS and EVSEB, and experiment 3 uses 256, 1024, 4096): five queries or batches per
scheme, shape and q, timed separately and pooled per cell in the summary
(`query_sets` = 5; the raw trials keep a `query_set` column).

* **HMF-Audit**: native ALL queries (region R0, service, log type, `[from, to)`,
  optional tenant). The fixture's ALL tree indexes every record under its real
  attributes; each query is checked against the dataset before it is timed.
* **BLAS**: closed time range `[ts, te]` plus a keyword expression over its
  16-token vocabulary; the placeholder batch keywords are not indexed.
* **DHT-backed**: clustered = q leaves of one MK-Tree subtree sharing ancestor
  certificates; scattered = as many (log type, ancestor) pairs as exist.
* **EVSEB**: clustered = q consecutive leaves (one subtree); scattered = every
  leaf under a different branch.

Benchmarks are named `<shape>/q=<q>/set=<1..5>` and report
`workload-version/op` 9.

**Experiment 2** (HMF-Audit vs EVSEB) runs the same HMF-Audit batches for both schemes: the
`record_ordinals` of every HMF-Audit entry of `R0_queries.json` (the HMF-Audit query is
resolved through ALL once, outside the timer, and checked against the file) are the
records both schemes audit, in ascending ordinal order. The middle record of each batch
(index `q/2` of that list) is the single tampered log, the same one in both schemes. A timed
operation detects the mismatch and localizes the tampered record (detect + localize in both),
and the benchmark checks that exactly that record was found. Benchmarks are named
`<shape>/q=<q>/set=<n>`, pooled per cell in the summary. EVSEB's own `dht_evseb_queries.json`
sets are not used here: a batch that is clustered in HMF-Audit is not clustered in EVSEB's
quaternary tree. `--q-list 1,16,...` runs and summarizes only those q values.

**Same logs in every scheme (Experiment 1).** `exp1_verification.sh --records-from-hmf` makes
DHT-backed and EVSEB verify the `record_ordinals` of the HMF-Audit queries (ascending ordinal
order) instead of their own `dht_evseb_queries.json` batches, so HMF-Audit, DHT-backed and EVSEB
audit the same logs in every cell. It sets `FAIR_BATCHES_FROM_HMF=1`, which the DHT-backed and
EVSEB benchmarks and the post-processing read. For example, for just those two schemes:
`FAIR_QUERY_SET=10000000-skewed ./exp1_verification.sh --systems dht-backed,evseb --records-from-hmf --count 20 --warmup 3 --dataset <skewed jsonl>`.
A batch that is clustered in HMF-Audit is not clustered in their trees, and the records of a
scattered HMF-Audit batch can need more DHT proof lookups than DHT-backed's own scattered batch
(the benchmark's limits are `FAIR_DHT_MAX_REQUESTS` etc., see "Known limitations").

## Experiment 2B: ACHP corruption density and locality

`exp2b_localization.sh` asks whether ACHP's adaptive frontier beats fixed-depth localization as
the number of corrupted records m and their physical distribution change. It never touches Experiment 2
(2A), which keeps writing to `<output>/exp2_localization/`; 2B writes to `<output>/exp2_localization/exp2b/`.

* **Workload.** Five fixed query sets, each exactly q = 1024 distinct records of the 10M dataset: 4 dense
  segments with 128 consecutive requested records (so a clustered m up to 64 fits in one segment) and 64
  sparse segments with 8 records each (so a scattered m up to 64 has 68 distinct segments, one record each).
  `manifests/queries.json` holds them. The ALL range query is not part of 2B: the manifest's ordinals are the
  batch.
* **Corruption locality** is a property of where the corrupted records sit and is kept apart from the
  *query locality* (how the audited records are laid out: here always dense+sparse). m in {1, 4, 16, 64}.
  m = 1 is one shared case (locality `common`), aggregated once. For m > 1: *clustered* draws m distinct
  requested records from one randomly chosen segment that holds at least m of them (a query with no such segment
  is flagged unsuitable and the run stops, the definition is never weakened); *scattered* takes one record per
  segment, from m equal strata of the segments the query touches in physical order, so the m segments are
  distinct and spread over the whole query (if the query touched fewer than m segments, every segment is used
  and the records are dealt out as evenly as capacity allows). Each (query, m, locality) has 3 warm-up and 20
  measured draws, all distinct, seeded from the master seed (default 20261123, `--seed`), the query, m, locality
  and repetition. They are drawn once and persisted in `manifests/corruption.json` with, per case, the actual
  locality, the distinct segments, shards and regions, the records per segment and the corrupted ordinals (the
  canonical record IDs) and HMF addresses; validation recomputes this from the records and checks that exactly
  m distinct records of the audit query are corrupted, clustered cases sit in one segment and scattered cases
  span the widest spread the query allows. Every algorithm receives exactly the same corrupted records, and a
  rerun reuses the manifests (a changed parameter or layout is an error, never a silent redraw; manifest files
  from before locality metadata, version 1, are refused). 805 cases in all (5 queries x 7 localities x 23).
* **Algorithms.** `ACHP` (`StrategyACHP`, default coefficients, recorded in `run_config.json`) and `FIXED`
  (`StrategyFixedDepth`). EVSEB is not run: its single detect-and-localize operation has no post-mismatch phase
  to time separately and no Cassandra partition reads or retrieval rounds to compare (`run_config.json` says so).
  **kappa = 16 is not available:** `Service.Localize` and `VerifyProof` accept only K = 63 (one round per tree,
  root to leaves), so the fixed-depth strategy runs unchanged and `run_config.json` records both the requested
  and the effective kappa. Fixed-depth retrieves one reference proof whatever kappa is, so only its offline
  comparison rounds would differ.
* **Timed operation.** `Service.Localize` only, after the initial integrity failure: planning, Cassandra reference
  retrieval (including the fixed-depth reference proof), authentication and hash comparison, adaptive replanning
  or the fixed-depth traversal, leaf confirmation. Outside the timer: query construction, corruption injection (a
  persistent change to the auditor's PostgreSQL ciphertext, restored after every run), the failed proof (the
  initial batch verification), proof replay and the checkpoint. The algorithm order rotates from case to case.
* **Per operation (`raw.csv`, every measured operation including errors and fallbacks):** latency, partition reads
  (from the request plans, failed calls included), the Cassandra reader's own successful-read count, distinct
  partitions, retrieval rounds, nodes and evidence bytes (32 per node), retrieval time, ACHP planning time,
  mismatching nodes met, confirmed corrupted leaves, requested and executed strategy, fallback and its reason,
  exact-set correctness (false positives, missed, non-record suspects, unresolved evidence), proof replay against
  the trusted root, status, query and its locality, m, corruption locality (requested and actual), repetition, seed, corrupted ordinals and addresses, and the segments, shards and regions affected. A measurement that does
  not apply (planning time of fixed-depth) is an empty cell, never 0, and an errored run has no latency.
* **`summary.csv`:** per corruption count x corruption locality x algorithm, with the locality metadata (mean distinct segments, shards and regions): sample counts, error / incorrect / unverified / fallback /
  unresolved counts, p50 and p95 latency (nearest-rank) and the mean I/O, transfer, round and planning figures.
  Latency statistics use valid rows only; a fixed-depth fallback is never an ACHP result.

```bash
bash systems/experiments/scripts/exp2b_localization.sh --dry-run          # manifests at full scale + in-memory smoke run; no database
FAIR_CLOUD=1 FAIR_QUERY_SET=10000000-skewed bash systems/experiments/scripts/exp2b_localization.sh     --dataset <skewed 10M file> --output <results dir>                      # the real run (about 805 cases x 2 algorithms)
```

The real run needs the HMF-Audit commit that contains `benchmarks/exp2b` (and ACHP) checked out in
`systems/hmf-audit`.

## Shard-count study (HMF-Audit)

How many shards per region HMF-Audit should use is measured in two steps; the experiments themselves
use 4 (`SHARDS_PER_REGION` in `scripts/lib.sh`).

1. **Structural, no database** (seconds): the real HPP planner plans the native queries of a query set for
   each shard count and counts shards, segments, Cassandra requests and fetched nodes:

   ```bash
   cd systems/hmf-audit
   FAIR_QUERY_DIR=../experiments/native-queries/<set> FAIR_SHARD_SWEEP=1,2,4,8,16,32,64 \
   FAIR_SHARD_PLAN_OUT=plan.csv go test ./benchmarks/latency -run TestFairHMFShardPlan -v
   ```

   It shows what a shard count changes in the proof (it does not change the nodes fetched; it changes
   how many shards, partitions and upper-layer requests a query touches), not what that costs in time.
2. **Real implementation** (`scripts/exp_shards.sh`): for every shard count the HMF fixture is seeded with that
   many shards per region on the real databases and the native verification benchmark is timed on the
   chosen cells. `shard_sweep_summary.py` gives p50 and p95 per cell and shard count, the best count per
   cell, and the geometric mean of p50 over the best p50 as the overall ranking (ties within 5%).

   ```bash
   cd systems/experiments/scripts
   FAIR_CLOUD=1 FAIR_QUERY_SET=<set> bash ./exp_shards.sh --shards 1,2,4,8 --q-list 64,256,1024,4096 \
       --count 10 --warmup 3 --dataset <jsonl> --output <results folder>
   ```

   Each shard count re-seeds the fixture, so the sweep costs (setup + run) per count (about 35 minutes of
   setup at 10M records). The order can bias the result through the database VMs' cache state: if two
   counts are close, repeat the sweep with `--shards` reversed into another `--output` and pass both
   folders to `shard_sweep_summary.py`.

## What is fixed (the deterministic part)

* **Dataset.** The default is the existing 10,000,000-record subset
  `systems/fair-results-10m-localization/spark_stratified_10000000.jsonl`
  (3,695,211,373 bytes, 3.44 GiB). Its SHA-256 is checked against
  `a8fa53db3327ce5b21cc61b249276817d81b2ee29a6facf64f48619d215dba82` before
  every run; a different file is refused. Other sizes are generated by
  `dataset/scripts/select_fair_subset.go` (evenly spaced records per region,
  no randomness) into `systems/experiments/datasets/`.
* **Requests.** Clustered-2 and scattered request sets are bit-reversal
  prefixes over fixed ordinals: no random numbers anywhere.
* **Settings.** 4 regions × 1 shard, 8,192-leaf segments, concurrency 8,
  `GOMAXPROCS=1`, `-cpu 1`, 30 repetitions, 5 warm-ups, 250 ms window, nearest-rank
  percentiles, BLAS 1 generator / 128 entries per file / 8 files per block,
  DHT-backed one epoch, stride 8, window 1024, 16 logical DHT nodes for both
  DHT-backed and EVSEB (4 real Cassandra routing reads per lookup).
* **Record of the run.** Each experiment writes `environment.txt` with these
  settings, the dataset hash, and the git state.

What is **not** deterministic: wall-clock latencies (they depend on the machine
and what else is running), and the keys/IVs EVSEB and DHT-backed generate fresh
for every fixture (these change stored bytes, never the workload or any counter).

## Running on the cloud VMs

The same scripts run against separate database VMs when `FAIR_CLOUD=1` (bash only);
see [`../cloud/README.md`](../cloud/README.md) for the topology, provisioning and the
`cloud/run-pipeline.sh` wrapper. Without `FAIR_CLOUD` nothing changes: the local
Docker stack above is used and cannot be pointed at a remote database.

## Cost

* Each experiment re-seeds the fixtures it uses (the original fixtures truncate
  and reload at process start), so HMF-Audit is seeded three times, EVSEB twice,
  DHT-backed and BLAS once.
* **Storage.** The existing 10M file is 3,695,211,373 bytes (3.44 GiB). Database
  storage was measured at 500,000 records (after `nodetool flush`) and scaled
  linearly by 20 to 10,000,000:

  | Store | 500k measured | 10M estimate |
  |---|---|---|
  | PostgreSQL: HMF-Audit / EVSEB / DHT-backed / BLAS | 127 / 272 / 419 / 118 MB | 2.5 / 5.4 / 8.4 / 2.4 GB |
  | Cassandra: HMF-Audit / EVSEB / DHT-backed | 50 / 90 / 127 MB | 1.0 / 1.8 / 2.5 GB |
  | Total | 1.20 GB | about 24 GB (22.4 GiB) |

  Together with the file that is about 26 GiB. WAL, commit logs and compaction
  need extra room, so keep roughly 35 GB free for Docker. The estimate is a
  linear extrapolation, not a measurement at 10M.
* **Time.** The earlier 10M run took about 9.5 h for HMF-Audit and about 6 h
  more for EVSEB (30 repetitions, verification + localization + tamper-count in
  one process per system). Because each experiment script re-seeds, expect the
  three scripts to take well over a day at 10M.

## Setup speed (outside every timer)

Seeding a fixture is not measured, so it is allowed to be faster than the timed
requests. The timed protocol (`GOMAXPROCS=1`, 8-wide request concurrency, real DHT
routing reads, no proof cache) is unchanged, and the stored rows are the same.

* **GOMAXPROCS during seeding.** Every fixture raises it to the CPU count while it
  seeds and restores it before the first timed request. `FAIR_SETUP_GOMAXPROCS=N`
  overrides the count.
* **EVSEB.** 4,096 records per PostgreSQL `COPY` (was 256); the Cassandra DHT is
  written by 32 writers in unlogged batches of at most 256 entries. The 32 is
  separate from `FAIR_EVSEB_DHT_CONCURRENCY` (8), which bounds timed lookups.
* **DHT-backed.** 32 writers, each sending up to 32 queued records (at most 32 KiB)
  as one unlogged batch instead of one `INSERT` per round trip; transient write
  timeouts are retried (the inserts are idempotent).
* **HMF-Audit.** 8 segment trees are written to Cassandra at once (was 4);
  `FAIR_HMF_SEED_CONCURRENCY=N` overrides it.
* **BLAS.** Only the GOMAXPROCS change; its load is one streaming `COPY`.

**Parallel ingest (opt-in).** `scripts/ingest.sh --records N --parallel` ingests the
schemes at the same time; `--max-parallel K` limits it to K at once. Guards in
`scripts/parallel.sh`: never more jobs than CPUs; each job gets an equal share of the CPUs
for its setup threads and a lower priority; a job starts only if the memory budget
(RAM minus a reserve of 10%, at least 1 GiB) covers the estimated peak of every running job plus
the new one; and, on Linux, if available memory falls below the floor the newest job
is stopped and run again alone (the last running job is never stopped). Per-job logs are
`results/.../ingest/<system>.log`. The 10M memory estimates (HMF-Audit 8000 MB, EVSEB 3000,
DHT-backed 3000, BLAS 1500) are extrapolated from 100k peaks, not measured at 10M; override
with `INGEST_MEM_MB_<SYSTEM>` (for example `INGEST_MEM_MB_HMF_AUDIT=10000`) and
`INGEST_MEM_RESERVE_MB`. `bash scripts/test_parallel.sh` tests the guards with fake jobs.
Nothing protects against another process writing to the same databases while it runs.

`environment.txt` records this. If a Cassandra VM reports write timeouts while
seeding, lower `FAIR_HMF_SEED_CONCURRENCY`.

## Known limitations

* **DHT-backed limits are raised for the benchmark.** `systems/dht-backed`
  bounds each proof call (`internal/cassandradht/limits.go`: 32,768 requests,
  8 MiB of proof bytes by default). The largest native batch (scattered
  q = 4096) needs about 53,800 proof lookups, so the benchmark gives the
  Cassandra DHT client 65,536 requests, 64 MiB of proof bytes and 128 MiB of
  ancestor map (`FAIR_DHT_MAX_REQUESTS`, `FAIR_DHT_PROOF_BYTES`,
  `FAIR_DHT_ANCESTOR_MAP_BYTES` override them). The scheme's defaults are
  unchanged; the values are in `environment.txt` and in every result line.
* **HMF-Audit tenant queries authenticate the whole range.** HMF-Audit's ALL
  range does not include the tenant: it is resolved and proven over every
  tenant, and a named tenant is filtered afterwards. For the native queries
  that name a tenant the authenticated range is much larger than q (about
  1.1M entries at scattered q = 1024); `all-entries/op` records it.
* **Native queries need the 10M dataset.** They were found on the pinned 10M
  subset, so experiments 1 and 3 (whose HMF-Audit fixture indexes the real
  attributes) do not run on the 100k subset.
* Memory: Cassandra uses about 3 GB and the fixtures several GB more. On a
  machine with little free memory, close other containers first.

## Cleaning up

```powershell
docker compose -f ..\fair-persistence\docker-compose.yml down -v   # removes the containers and the database volumes
```
