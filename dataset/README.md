# Datasets

The raw logs and the large derived files are **not stored in git** (the combined
Spark file alone is about 12 GB; GitHub rejects files over 100 MB). This page
lists what is tracked, what you have to supply, and the commands that rebuild
everything else. All generation is deterministic (fixed seed, no randomness), so
the same inputs give byte-identical outputs.

## One command: download and derive the 10M subset

```bash
python dataset/scripts/prepare_spark_10m.py
```

This downloads the Spark logs from the Loghub Zenodo record
(<https://zenodo.org/records/8196385>, CC BY 4.0; project
<https://github.com/logpai/loghub>), checks the archive MD5, extracts it safely
into `dataset/Spark/`, and verifies all 3,852 container logs against the SHA-256
values recorded in `combined/spark_uniform_combined.config.json`. It then builds
`combined/spark_uniform_combined.jsonl` (33,236,604 records) and selects the
10,000,000-record subset with `select_fair_subset.go`, writing it to the file
the experiment pipeline reads
(`systems/fair-results-10m-localization/spark_stratified_10000000.jsonl`). The
subset's SHA-256 is compared with the pinned value; on a mismatch the file is
kept as `*.mismatch`, not used, and the script exits with status 3. Steps whose
output is already present and verified are skipped, and `systems/experiments/scripts/run_pipeline`
runs this script first. Needs Python 3.9+, Go 1.27 and about 25 GB of free disk.
Options: `--dest-dir`, `--output`, `--stop-after raw|combined|subset`, `--force`.

The selection is deterministic and uses no random numbers: each of the four
regions (R0 to R3) contributes the records at positions
`floor(k * (M - 1) / 2,499,999)` for k = 0 to 2,499,999, where M is that
region's record count, and the four blocks are concatenated in region order.

## Layout

| Path | In git? | What it is |
|---|---|---|
| `scripts/` | yes | `prepare_spark_10m.py` (the one-command entry point), the combined-file generator and the subset selector |
| `combined/*.config.json`, `*.validation.json` | yes | Exact configuration and validation report of the combined Spark file, including the SHA-256 of every raw input file |
| `Spark/` | **no** | Raw Spark/YARN container logs, downloaded by `prepare_spark_10m.py`: `application_*/container_*.log` |
| `downloads/` | **no** | Cached `Spark.tar.gz` archive (183.5 MB) |
| `combined/*.jsonl` | **no** | Rebuilt from the raw logs (below) |

Only the Spark dataset is used. Everything not tracked here is rebuilt by
`prepare_spark_10m.py`, so `Spark/` and `combined/*.jsonl` can be deleted to save
space; as long as the 10M subset is in place and verified, the script does not
download or rebuild them.

## Rebuilding by hand

Normally `prepare_spark_10m.py` does this. Run from the repository root; Python
3.9+ is required.

```bash
# Spark: one record per raw log line, with region/tenant/service/type/time
python dataset/scripts/gen_spark_combined.py --raw-dir dataset/Spark \
    --out dataset/combined/spark_uniform_combined.jsonl \
    --num-regions 4 --num-tenants 1000 --seed 42
```

Check the result against the committed `combined/*.config.json` and
`combined/*.validation.json`; the record counts and input hashes must match.

## The benchmark subsets

The benchmarks never read the 12 GB combined file directly. They use a
deterministic, region-stratified subset made by `scripts/select_fair_subset.go`
(evenly spaced records from each region, no randomness):

```bash
go run dataset/scripts/select_fair_subset.go \
    -input dataset/combined/spark_uniform_combined.jsonl \
    -output systems/experiments/datasets/spark_stratified_100000.jsonl \
    -regions 4 -per-region 25000
```

You normally do not run this yourself: `systems/experiments/scripts/run_pipeline.sh`
creates the 100,000-record subset automatically. The 10,000,000-record subset
(`-regions 4 -per-region 2500000`) is checked by SHA-256
(`a8fa53db3327ce5b21cc61b249276817d81b2ee29a6facf64f48619d215dba82`) before
every run, and a different file is refused, so results are only produced from
exactly the dataset the reported numbers used.
