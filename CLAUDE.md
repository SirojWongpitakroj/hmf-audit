# HMF-Audit fair benchmark: cloud runner

Read systems/cloud/README.md first, then systems/experiments/README.md.

## Context

This branch (cloud-implementation) runs the fair benchmark of HMF-Audit, EVSEB,
DHT-backed and BLAS against separate GCP VMs (asia-southeast1-c, private IPs only).
This machine is the benchmark runner (10.148.0.8). Topology: 10.148.0.2 HMF-Audit
Cassandra, .3 EVSEB Cassandra, .4 DHT-backed Cassandra, .5 BLAS PostgreSQL,
.6 shared cloud-provider PostgreSQL, .7 blockchain anchor (Hardhat). Cloud mode is
FAIR_CLOUD=1 (bash only).

## Rules

- One database engine per VM; no scheme-specific app servers.
- Keep the four HMF regions as separate partitions in the one HMF Cassandra.
- EVSEB and DHT-backed DHT routing (16 nodes, 4 real Cassandra reads per lookup)
  must stay on; never set DHT_PEERS below 2.
- The blockchain anchor (chain state of the DHT-backed and EVSEB timed paths) stays in memory and the anchor VM is outside the timers. BLAS is the exception: its full node reads the block-level and transaction-level indexes from PostgreSQL on every request (blas.chain_segments, blas.chain_blocks), because those indexes do not fit in memory at the scale the paper targets; `FAIR_BLAS_CHAIN_IN_MEMORY=1` restores the in-memory chain for debugging only, never for results.
- systems/cloud/cloud.env holds the DB password: never print it, commit it or send
  it anywhere. Never open the .pem files in other-paper-input/.
- Never delete anything under systems/experiments/results/ without first reading the
  run's pipeline-*.log tail ("pipeline finished") and listing the folder; a "killed"
  notice does not mean the run died.
- Do not git push or change other VMs' data without asking the user.
