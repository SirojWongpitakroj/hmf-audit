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

| Tool | Version | Used for |
|---|---|---|
| Git | any recent | cloning the repository |
| Go | **1.27.0 or newer** | building and running every system and benchmark (`go.mod` says `go 1.27.0`) |
| Docker with Compose v2 | any recent (tested: Docker 29.9, Compose v5.7) | the local PostgreSQL 17 and Cassandra 5.0.9 databases |
| Python | **3.9 or newer** (tested: 3.12) | dataset download, result processing, figures |
| `matplotlib` | 3.9 or newer | drawing the three figures |
| PowerShell 5.1+ (Windows) **or** bash | | running the scripts (Git for Windows includes Git Bash) |

Hardware for the full 10,000,000-log run: **16 GB RAM** (Cassandra needs about
3 GB and the benchmark programs several GB more, so at least 8 GB should be
free), about **60 GB of free disk** (about 25 GB for the dataset and about 35 GB
for the databases), and an internet connection for the one-time 183 MB dataset
download. The run takes well over a day.

## Install the tools (from a clean machine)

Skip any tool you already have, then run the checks at the end of this section.

### Windows 10/11 (PowerShell)

```powershell
winget install -e --id Git.Git
winget install -e --id GoLang.Go
winget install -e --id Python.Python.3.12
winget install -e --id Docker.DockerDesktop
```

1. **Close and reopen PowerShell** so the new programs are on your `PATH`.
2. Restart Windows if the Docker Desktop installer asks. It uses WSL 2; if it
   reports WSL is missing, run `wsl --install` in an administrator PowerShell and
   restart.
3. **Start Docker Desktop** and wait until it says the engine is running.
4. Give Docker at least 6 GB of memory. Create or edit
   `%UserProfile%\.wslconfig` so it contains

   ```
   [wsl2]
   memory=8GB
   ```

   then run `wsl --shutdown` and start Docker Desktop again.

### macOS (Homebrew)

Install Homebrew from <https://brew.sh> first, then:

```bash
brew install git go python
brew install --cask docker
```

Open the Docker app once and wait for it to start. In Docker Desktop, go to
Settings, then Resources, and set Memory to at least 6 GB.

### Ubuntu / Debian

```bash
sudo apt update && sudo apt install -y git curl python3 python3-pip python3-venv

# Go: distribution packages usually lag behind, so use the official tarball
# (arm64 machines: replace amd64 with arm64; see https://go.dev/dl/)
curl -LO https://go.dev/dl/go1.27.0.linux-amd64.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf go1.27.0.linux-amd64.tar.gz
echo 'export PATH=$PATH:/usr/local/go/bin' >> ~/.profile
export PATH=$PATH:/usr/local/go/bin

# Docker Engine and the Compose plugin
curl -fsSL https://get.docker.com | sudo sh
sudo usermod -aG docker "$USER"      # then log out and back in
```

### Check that everything is installed

```bash
git --version
go version                 # must print go1.27.0 or newer
docker --version
docker compose version     # must work (Compose v2)
docker run --rm hello-world
python --version           # 3.9 or newer (on macOS/Linux the command is python3)
```

If `docker run --rm hello-world` fails, Docker is not running yet. If
`go version` shows something older than 1.27.0, install a newer Go before
continuing.

## Get the code and the Python packages

```bash
git clone --recurse-submodules https://github.com/SirojWongpitakroj/hmf-audit.git
cd hmf-audit
python -m pip install -r requirements.txt
```

On macOS and Linux, recent Python installs refuse to install packages system-wide.
Use a virtual environment instead, and keep it active for the rest of the steps:

```bash
python3 -m venv .venv
source .venv/bin/activate
python -m pip install -r requirements.txt
```

The datasets are not in the repository (the raw logs and the 12 GB combined file
are too large). The pipeline below downloads and prepares them for you. To do
only that step: `python dataset/scripts/prepare_spark_10m.py`, described in
[`dataset/README.md`](dataset/README.md).

## Run the experiments (one command)

Before you start: Docker must be running, and (macOS/Linux) the virtual
environment from the install step must be active.

From `systems/experiments/scripts`. The pipeline checks its prerequisites, downloads the
Loghub Spark logs and deterministically derives the 10,000,000-record subset,
loads it into PostgreSQL and Cassandra, runs the three experiments, and plots the
three figures. Data that is already present and verified is not downloaded or
rebuilt, so an interrupted run can be started again with the same command.

```powershell
# PowerShell
cd systems\experiments\scripts
.\run_pipeline.ps1                        # 10,000,000 logs
```

If PowerShell says that running scripts is disabled on this system, run it like
this instead (it changes nothing permanently):

```powershell
powershell -ExecutionPolicy Bypass -File .\run_pipeline.ps1
```

```bash
# bash (macOS, Linux, or Git Bash on Windows)
cd systems/experiments/scripts
./run_pipeline.sh                         # 10,000,000 logs
```

| Experiment | Measures | Figure |
|---|---|---|
| 1 Verification | Verification latency, clustered vs scattered requests, q = 1 … 16384 | `figure1_verification_latency.png` |
| 2 Localization | Time to detect and localize one tampered log (HMF-Audit vs EVSEB) | `figure2_localization_latency.png` |
| 3 Ablation | HMF-Audit with each optimization removed in turn | `figure3_ablation_latency.png` |

Output goes to `systems/experiments/results/spark-10000000/` (the three figures
are also copied to its `figures/` folder) and a log of the run to
`systems/experiments/results/pipeline-10000000-<timestamp>.log`. Defaults:
30 repetitions, 5 warm-ups, 250 ms timing window. Expect well over a day.
Details, options and known limitations are in [`systems/experiments/README.md`](systems/experiments/README.md).

By default experiment 1 compares HMF-Audit, BLAS and EVSEB. DHT-backed is
rejected by its own memory bounds at q = 4096 and 16384, so it is left out
unless you pass `--systems hmf-audit,dht-backed,blas,evseb`.

## Test

Each system is a separate Go module. Run the unit tests of all five from the
repository root (once: `go env -w CGO_ENABLED=0`, which builds pure Go and works
on every platform).

```bash
# bash
for m in canonical-ingestion EVBES blas dht-backed hmf-audit; do (cd systems/$m && go test -count=1 ./...); done
```

```powershell
# PowerShell
foreach ($m in "canonical-ingestion","EVBES","blas","dht-backed","hmf-audit") { Push-Location "systems\$m"; go test -count=1 ./...; Pop-Location }
```

The unit tests do not need Docker or a database. Expected result:

| Module | Result |
|---|---|
| `canonical-ingestion`, `EVBES`, `hmf-audit` | all packages pass |
| `blas` | all packages pass except `blas/audit`: `TestNoExperimentConstantsInProtocolPackages` is a source-scan policy check that flags the literal `4096` in `internal/durable/generator_keywords.go` |
| `dht-backed` | all packages pass except `internal/repo`: four source-scan policy checks (`TestNoGoStatementAnywhere`, `TestNoConcurrencyPrimitives`, `TestNoSleepOrDelaySimulation`, `TestNoTestingBBenchmarks`) reject constructs that the benchmark code deliberately uses |

These five failures are policy checks over the source text, not functional tests,
and they fail the same way in the original development tree.

The persistence-backed fair benchmarks need PostgreSQL and Cassandra and
`FAIR_PERSISTENCE=1`; the experiment scripts start the databases and set that for
you, and the benchmarks are skipped or refuse to run otherwise. To remove the
containers and their data afterwards:

```bash
docker compose -f systems/fair-persistence/docker-compose.yml down -v
```

## Results

Experiment results (CSV summaries, figures, raw benchmark output and run logs)
are **not committed to git**. Generate them with the pipeline above; they are
written to `systems/experiments/results/spark-10000000/` and ignored by
`.gitignore`. The placement definitions behind "clustered" and "scattered" are in
`systems/FAIR_BENCHMARK_PROTOCOL.md`: clustered-2 draws from the first two
segments of region R0, and scattered draws from regions R1 through the last.

