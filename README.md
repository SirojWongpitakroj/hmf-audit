# hmf-log-integrity

Repository: <https://github.com/SirojWongpitakroj/hmf-audit>

Go implementations of log-integrity verification schemes and a benchmark that
compares them under identical conditions (same dataset, same requests, same
persistence layer):

| System | Folder | What it is |
|---|---|---|
| **HMF-Audit** | `systems/hmf-audit` (git submodule) | Tamper-evident audit service built on a Hierarchical Merkle Forest, backed by Cassandra and PostgreSQL |
| **BLAS** | `systems/blas` | Blockchain-enabled verifiable log audit with hybrid storage (baseline) |
| **DHT-backed** | `systems/dht-backed` | DHT-backed ancestor-assisted Merkle verification (baseline) |
| **EVSEB** | `systems/EVBES` | Searchable-encryption log integrity with Boolean search (baseline) |

## Repository layout

```
.
├── dataset/                  data generators and subset selector; raw/large data is NOT in git (dataset/README.md)
├── systems/
│   ├── hmf-audit/            proposed system (submodule)
│   ├── blas/  EVBES/  dht-backed/    baselines
│   ├── canonical-ingestion/  shared ingestion library used by blas and dht-backed
│   ├── experiments/          experiment runner; the one-command pipeline is scripts/run_pipeline.sh / .ps1 (see its README)
│   ├── fair-persistence/     docker-compose for the shared local PostgreSQL + Cassandra
│   ├── attribute-query/      demo: ingest logs and query by attribute, each result VALID or REJECTED
│   ├── demo-script/          demo: tamper with a log and find it (100,000 logs)
│   └── FAIR_BENCHMARK_PROTOCOL.md    exact benchmark protocol (placements, request sizes, settings)
```

## Requirements

* Go 1.27 (build with `CGO_ENABLED=0`)
* Docker with Compose (runs PostgreSQL 17 and Cassandra 5.0.9 locally; the scripts only ever use `127.0.0.1`)
* Python 3.9+ with `matplotlib` (post-processing and figures)
* PowerShell 5.1+ (Windows) **or** bash (Git Bash, WSL, Linux, macOS)
* Memory and disk: Cassandra takes about 3 GB and the benchmark fixtures several GB more, so keep at least 8 GB of RAM free. The 10,000,000-record run needs about 35 GB of free disk.

## Install

```bash
git clone --recurse-submodules https://github.com/SirojWongpitakroj/hmf-audit.git
cd hmf-audit
python -m pip install matplotlib
```

The datasets are not in the repository (the raw logs and the 12 GB combined file
are too large). The pipeline below downloads and prepares them for you. To do
only that step: `python dataset/scripts/prepare_spark_10m.py`, described in
[`dataset/README.md`](dataset/README.md).

## Run the experiments (one command)

From `systems/experiments/scripts`. The pipeline checks its prerequisites, downloads the
Loghub Spark logs and deterministically derives the 10,000,000-record subset,
loads it into PostgreSQL and Cassandra, runs the three experiments, and plots the
three figures. Data that is already present and verified is not downloaded or
rebuilt.

```powershell
# PowerShell
cd systems\experiments\scripts
.\run_pipeline.ps1 -Records 100000        # dry run on 100,000 logs
.\run_pipeline.ps1                        # full run on 10,000,000 logs
```

```bash
# bash
cd systems/experiments/scripts
./run_pipeline.sh --records 100000
./run_pipeline.sh
```

| Experiment | Measures | Figure |
|---|---|---|
| 1 Verification | Verification latency, clustered vs scattered requests, q = 1 … 16384 | `figure1_verification_latency.png` |
| 2 Localization | Time to detect and localize one tampered log (HMF-Audit vs EVSEB) | `figure2_localization_latency.png` |
| 3 Ablation | HMF-Audit with each optimization removed in turn | `figure3_ablation_latency.png` |

Output goes to `systems/experiments/results/spark-<records>/` (the three figures
are also copied to its `figures/` folder) and a log of the run to
`systems/experiments/results/pipeline-<records>-<timestamp>.log`. Defaults:
30 repetitions, 5 warm-ups, 250 ms timing window. Expect about 3 hours for
100,000 logs and well over a day for 10,000,000. Details, options and known
limitations are in [`systems/experiments/README.md`](systems/experiments/README.md).

By default experiment 1 compares HMF-Audit, BLAS and EVSEB. DHT-backed is
rejected by its own memory bounds at q = 4096 and 16384, so it is left out
unless you pass `--systems hmf-audit,dht-backed,blas,evseb`.

## Test

Each system is a separate Go module. Run its unit tests from its folder:

```bash
cd systems/hmf-audit && CGO_ENABLED=0 go test ./...
cd systems/blas      && CGO_ENABLED=0 go test ./...
```

and the same for `systems/EVBES`, `systems/dht-backed` and `systems/canonical-ingestion`.
The unit tests do not need a database. The persistence-backed fair benchmarks
need PostgreSQL and Cassandra and `FAIR_PERSISTENCE=1`; they are started for you
by the experiment scripts and are skipped or refuse to run otherwise. To remove
the containers and their data afterwards:

```bash
docker compose -f systems/fair-persistence/docker-compose.yml down -v
```

## Results

Experiment results (CSV summaries, figures, raw benchmark output and run logs)
are **not committed to git**. Generate them with the pipeline above; they are
written to `systems/experiments/results/spark-<records>/` and ignored by
`.gitignore`. The placement definitions behind "clustered" and "scattered" are in
`systems/FAIR_BENCHMARK_PROTOCOL.md`: clustered-2 draws from the first two
segments of region R0, and scattered draws from regions R1 through the last.

Also excluded from the repository: third-party papers (PDF), the raw log
datasets, generated benchmark subsets, build caches and private keys (see
`.gitignore`).

