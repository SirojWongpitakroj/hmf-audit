# Cloud deployment (Google Compute Engine, asia-southeast1-c)

The same fair benchmark as the local Docker study, with each scheme's storage on
its own VM. All traffic uses the VMs' **private** addresses; you SSH into the
runner only. Protocol, dataset (the pinned 10M subset), request sets, repetitions
and the in-memory chain in the timed paths are unchanged.

| VM | Name | Internal IP | Type | Runs | Holds |
|---|---|---|---|---|---|
| 1 | hmfaudit | 10.148.0.2 | t2a-standard-2 | Cassandra | keyspace `hmf_audit`: the four regions R0..R3 as separate `(region_id, shard_id, …)` partitions |
| 2 | evseb | 10.148.0.3 | t2a-standard-2 | Cassandra | keyspace `evseb`: `dht_entries`, `routing_table` |
| 3 | dhtbacked | 10.148.0.4 | t2a-standard-2 | Cassandra | keyspace `dhtbacked`: `proof_entries`, `routing_table` |
| 4 | blas | 10.148.0.5 | t2a-standard-2 | PostgreSQL 17 | schema `blas` |
| 5 | cloudprovider | 10.148.0.6 | t2a-standard-2 | PostgreSQL 17 | schemas `hmf`, `evseb`, `dht` (the cloud-provider log storage) |
| 6 | runner | 10.148.0.8 | t2a-standard-4 | the four Go benchmarks | nothing persistent |
| 7 | blockchainanchor | 10.148.0.7 | e2-medium (x86) | Hardhat node, chain id 31337 | nothing the benchmarks time |

No scheme-specific application servers exist: each benchmark is a Go test process on
the runner that connects to its own storage VMs (HMF-Audit → VM 1 + VM 5, EVSEB →
VM 2 + VM 5, DHT-backed → VM 3 + VM 5, BLAS → VM 4). Each process is given only the
endpoints of its own scheme, so a wiring mistake fails instead of reading another
scheme's database. Every VM runs one database engine; `check.sh` fails if a VM holds
another scheme's keyspace or schema.

## What is preserved

* **DHT routing (EVSEB and DHT-backed).** Both keep the 16 logical nodes with 4
  real routing reads per lookup (`DHT_PEERS=16`). `check.sh pre` refuses
  `DHT_PEERS < 2` (which turns routing off), and `check.sh post` verifies each
  `routing_table` holds all 16 × 4 = 64 rows after seeding, so a lookup cannot
  silently become a direct read. Nothing in the Go code changed.
* **HMF regions.** The hierarchy tables are partitioned by region, so R0..R3 stay
  separate inside the one Cassandra instance; `check.sh post` confirms exactly four
  regions × 1 shard exist after seeding.
* **Chain.** The timed paths keep the in-memory chain. The anchor VM is a standing
  node for anchoring/setup; nothing measured depends on it.

## One-time setup

1. **Firewall (VPC internal only).** Allow the runner `10.148.0.8` to reach TCP 9042
   on VMs 1–3, 5432 on VMs 4–5 and 8545 on VM 7. Nothing needs a public address.
2. **Data disks.** Attach a persistent SSD to each database VM and mount it at
   `/var/lib/fair-data`. Sizes (linear estimates from the local 500k measurement,
   plus WAL/compaction room): VM 5 ≥ 60 GB (HMF 2.5 + EVSEB 5.4 + DHT 8.4 GB),
   VM 4 ≥ 20 GB, VMs 1–3 ≥ 20 GB. The runner needs ≈ 40 GB (dataset 25 GB + results).
3. **`cloud.env`.** Set `CLOUD_PG_PASSWORD` in `systems/cloud/cloud.env` (letters and
   digits only; currently `admin`, which is acceptable only because port 5432 is
   reachable from the runner alone) and copy the file to every VM at the same path
   (it is git-ignored; the IPs are already filled in). The password must be identical
   on the PostgreSQL VMs and the runner.
4. **Clone the repository on every VM** (`git clone --recurse-submodules …`), then run
   on each VM its own script from the repo root:

   | VM | Command |
   |---|---|
   | hmfaudit | `bash systems/cloud/provision/cassandra-vm.sh hmf` |
   | evseb | `bash systems/cloud/provision/cassandra-vm.sh evseb` |
   | dhtbacked | `bash systems/cloud/provision/cassandra-vm.sh dht` |
   | blas | `bash systems/cloud/provision/postgres-vm.sh blas` |
   | cloudprovider | `bash systems/cloud/provision/postgres-vm.sh shared` |
   | blockchainanchor | `bash systems/cloud/provision/anchor-vm.sh` |
   | runner | `bash systems/cloud/provision/runner-vm.sh`, then log out and in |

   The database containers restart with the VM. The Hardhat node also restarts, but
   keeps its state in memory: after the anchor VM has been stopped and started, run
   `bash systems/cloud/provision/anchor-vm.sh --deploy-only` to redeploy `EpochAnchor`.

## After a `git pull` on a database VM

The provisioning scripts are safe to repeat: they keep the data and only add what is
missing. On the EVSEB and DHT-backed Cassandra VMs they also make sure
`routing_table` exists (the table behind the 4 real routing reads per lookup). So after
pulling, just run that VM's script again:

```bash
cd ~/hmf-audit && git pull
bash systems/cloud/provision/cassandra-vm.sh evseb    # evseb VM
bash systems/cloud/provision/cassandra-vm.sh dht      # dhtbacked VM
```

The benchmark itself fills `routing_table` (16 nodes x 4 levels = 64 rows) when it
starts; `check.sh post` verifies the rows after seeding.

## Every session

Start the VMs (databases and anchor first), SSH into the runner, then:

```bash
source .venv/bin/activate
bash systems/cloud/check.sh pre                       # reachability, one keyspace/schema per VM, routing on, anchor up
bash systems/cloud/run-pipeline.sh --records 100000   # dry run on the 100k subset first
bash systems/cloud/run-pipeline.sh                    # the 10M study, detached from SSH
tail -f systems/experiments/results/pipeline-*.log
```

`run-pipeline.sh` is `experiments/scripts/run_pipeline.sh` with `FAIR_CLOUD=1`, so
the individual scripts (`exp1_verification.sh`, …) work the same way:
`FAIR_CLOUD=1 ./exp1_verification.sh --systems hmf-audit,evseb`.
`environment.txt` in every result folder records the VM layout and endpoints.

## Things to know

* **Arm.** The runner and the five database VMs are Arm (Ampere); the Go benchmarks
  are built on the runner itself (`CGO_ENABLED=0`), and `postgres:17` and
  `cassandra:5.0.9` are multi-arch. The anchor is x86 (e2).
* **Cassandra heap** is 3 GB here (local Docker: 2 GB) so the 8 GiB VMs keep page
  cache; it is the same on all three Cassandra VMs. PostgreSQL has `shared_buffers=2GB`
  on both PostgreSQL VMs.
* **PostgreSQL asymmetry.** VM 5 holds three schemes' data (≈ 16 GB) while VM 4 holds
  BLAS only (≈ 2.4 GB), so cache hit rates differ by design. Runs are sequential, so
  there is no cross-scheme contention, but read the BLAS numbers with that in mind.
* **Hardhat accounts are public.** Keep port 8545 reachable only from the runner.
* **Windows/PowerShell** is not supported in cloud mode (`FAIR_CLOUD=1` errors out);
  the runner is Linux.
* **Re-seeding.** As locally, each experiment re-seeds (truncates) its own fixtures
  at the start. In the cloud that happens in the schemes' own keyspaces/schemas on
  these VMs only; no other database is configured anywhere.
