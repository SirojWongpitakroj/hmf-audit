#!/usr/bin/env python3
"""Shared helper for the three local experiment scripts (PowerShell and bash).

  env      write environment.txt for one experiment
  exp1     validate + summarize + plot Experiment 1 (verification latency)
  exp2     validate + summarize + plot Experiment 2 (tamper localization latency)
  exp3     summarize + plot Experiment 3 (HMF-Audit ablation)
  compare  compare two latency_summary.csv files (or two ablation summaries)

Experiments 1 and 2 reuse the v8 parsing, validation, statistics and plotting
code of systems/fair_benchmark.py unchanged; this file only selects which
benchmarks each experiment expects and adds the Experiment 3 summary.
"""

from __future__ import annotations

import argparse
import csv
import math
import os
import platform
import statistics
import subprocess
import sys
from collections import defaultdict
from pathlib import Path
from typing import Any

SYSTEMS_DIR = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(SYSTEMS_DIR))

ABLATION_VARIANTS = ["FULL", "NO_SHARED_PLANNING", "NO_DEDUP", "NO_PARTITION_AWARE"]
ABLATION_LABELS = {
    "FULL": "Full HMF-Audit",
    "NO_SHARED_PLANNING": "No shared planning",
    "NO_DEDUP": "No deduplication",
    "NO_PARTITION_AWARE": "No partition-aware retrieval",
}
ABLATION_COLORS = {
    "FULL": "#2a78d6",
    "NO_SHARED_PLANNING": "#eb6834",
    "NO_DEDUP": "#1baf7a",
    "NO_PARTITION_AWARE": "#eda100",
}
DISTRIBUTIONS = ["clustered-2", "scattered"]
DISTRIBUTION_TITLES = {"clustered-2": "Clustered Requested IDs", "scattered": "Scattered Requested IDs"}


def git(*arguments: str, directory: Path = SYSTEMS_DIR) -> str:
    try:
        return subprocess.run(["git", "-C", str(directory), *arguments], capture_output=True, text=True,
                              check=True).stdout.strip()
    except Exception:
        return "unknown"


def command_output(*command: str) -> str:
    try:
        return subprocess.run(list(command), capture_output=True, text=True, check=True).stdout.strip()
    except Exception:
        return "unknown"


# --------------------------------------------------------------------- env
def native_environment_lines(args: argparse.Namespace) -> list[str]:
    """Request sizes and query selection: the native query sets, or the fixed placements."""
    query_dir = getattr(args, "query_dir", None)
    if not query_dir:
        counts = [q for q in (1, 16, 64, 256, 1024, 4096, 16384) if q <= int(os.environ.get("FAIR_MAX_REQUEST_Q", "16384"))]
        return [f"requested_counts={','.join(map(str, counts))}", "query_distributions=clustered-2,scattered"]
    return [
        "requested_counts=1,16,64,256,1024,4096",
        "query_distributions=clustered,scattered",
        f"native_query_dir={query_dir}",
        "native_queries=up to 5 clustered and 5 scattered audit batches per scheme and q (native-queries/README.md)",
        f"dht_backed_max_q={getattr(args, 'dht_max_q', 2048)}",
    ]


def write_environment(args: argparse.Namespace) -> None:
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    records = args.per_region * args.regions
    first_shard = records // args.regions // args.shards_per_region
    lines = [
        f"experiment={args.experiment}",
        "benchmark_protocol=HMF-Audit ALL-to-HPP verification-latency benchmark protocol v8 (local Docker experiments)",
        f"machine={platform.node()}",
        f"os={platform.platform()}",
        f"process_architecture={platform.machine()}",
        "cpu_allocation=1 logical CPU",
        "gomaxprocs=1",
        "setup_gomaxprocs=all CPUs (FAIR_SETUP_GOMAXPROCS overrides) while a fixture is seeded; seeding is outside every timer, and each timed request runs at gomaxprocs=1",
        "setup_writers=EVSEB 32 Cassandra writers and 4096-row PostgreSQL COPY, DHT-backed 32 writers with batches of up to 32 rows, HMF-Audit 8 segment writers (FAIR_HMF_SEED_CONCURRENCY); the timed concurrency of 8 is unchanged",
        "cgo_enabled=0",
        f"go_version={command_output('go', 'version')}",
        f"python_version={platform.python_version()}",
        f"workspace_commit={git('rev-parse', 'HEAD')}",
        f"hmf_audit_commit={git('rev-parse', 'HEAD', directory=SYSTEMS_DIR / 'hmf-audit')}",
        f"measured_repetitions={args.count}",
        f"warmup_operations_per_case={args.warmup}",
        f"timing_window={args.bench_time}",
        "percentile_method=nearest-rank",
        "p99_minimum_trials=100",
        f"source_dataset_path={args.source_dataset}",
        "benchmark_source=spark",
        "benchmark_dataset=subset",
        "benchmark_subset_term=deterministic region-stratified benchmark subset",
        f"benchmark_subset_path={args.dataset}",
        f"benchmark_subset_records={records}",
        f"benchmark_subset_sha256={args.dataset_sha256}",
        f"dataset_identifier=spark_stratified_{records}@{args.dataset_sha256}",
        "metadata_seed=42",
        "subset_selection_seed=none (deterministic formula)",
        "workload_selection_randomness=none",
        f"subset_selection={args.per_region} evenly spaced records from each region R0..R{args.regions - 1}, grouped by region",
        *native_environment_lines(args),
        f"region0_records={args.per_region}",
        f"region1_records={args.per_region}",
        f"hmf_shards_per_region={args.shards_per_region}",
        f"hmf_segment_leaves={args.segment_leaves}",
        f"hmf_first_shard_records={first_shard}",
        "clustered_segment_spans=2 (legacy placements only)",
        "hmf_hpp_concurrency=8",
        f"dht_epoch_capacity={records}",
        "dht_stride=8",
        "dht_window=1024",
        "dht_backed_concurrency=8",
        f"dht_backed_peers={args.dht_peers}",
        f"dht_routing_reads_per_lookup={max(0, (args.dht_peers - 1).bit_length())}",
        "evseb_dht_concurrency=8",
        f"evseb_dht_peers={args.dht_peers}",
        "evseb_merkle_arity=4",
        "blas_num_generators=1",
        "blas_entries_per_file=128",
        "blas_files_per_block=8",
        "blas_fanout=4",
        "blas_segment_size=8",
        "persistence_in_primary_timers=true",
        "proof_generation_in_primary_timers=true",
        "timer_boundary=request arrival through final verification result",
        "timing_machinery=testing.B for every series",
        "postgres_image=postgres:17",
        f"postgres_endpoint={args.postgres_endpoint}",
        "cassandra_image=cassandra:5.0.9",
        f"cassandra_endpoint={args.cassandra_endpoint}",
        f"deployment={args.deployment}",
        f"anchor_endpoint={args.anchor_endpoint}",
        "blockchain_network_in_primary_timers=false",
        f"systems_run={args.systems}",
    ]
    (out / "environment.txt").write_text("\n".join(lines) + "\n", encoding="utf-8")


# --------------------------------------------------------------- experiment 1
def run_exp1(args: argparse.Namespace) -> None:
    command = [
        sys.executable, str(SYSTEMS_DIR / "fair_benchmark.py"),
        "--results-dir", args.results_dir, "--dataset", args.dataset,
        "--expected-repetitions", str(args.count), "--warmup", str(args.warmup),
        "--bench-time", args.bench_time, "--metadata-seed", "42",
        "--systems", args.systems, "--verification-only",
    ]
    if getattr(args, "query_dir", None):
        command += ["--query-dir", args.query_dir]
        if "dht-backed" in args.systems.split(","):
            command += ["--max-q", f"DHT-backed={args.dht_max_q}"]
    subprocess.run(command, check=True)


# --------------------------------------------------------------- experiment 2
def run_exp2(args: argparse.Namespace) -> None:
    import fair_benchmark as fb

    results = Path(args.results_dir).resolve()
    environment = fb.parse_environment(results / "environment.txt")
    segment_leaves = int(environment.get("hmf_segment_leaves", "8192"))
    localization = {
        "hmf-audit.txt": {"BenchmarkFairHMFLocalizationAfterMismatch"},
        "evseb.txt": {"BenchmarkFairEVBESLocalizationAfterMismatch"},
    }
    trials: list[dict[str, Any]] = []
    for filename in localization:
        path = results / filename
        if not path.exists():
            raise SystemExit(f"missing raw result file {path}")
        trials.extend(fb.parse_result_file(path))
    fb.validate_trials(trials, args.count, args.warmup, localization, verification_only=False)

    manifest = fb.build_workload_manifest(
        Path(args.dataset).resolve(), args.count, args.warmup, args.bench_time, 42, segment_leaves,
        verification_only=False,
    )
    if environment.get("benchmark_subset_sha256") != manifest["dataset"]["sha256"]:
        raise SystemExit("benchmark subset hash differs between environment and dataset")
    manifest["system_parameters"]["HMF-Audit"]["shards_per_region"] = int(environment.get("hmf_shards_per_region", "1"))
    manifest["system_parameters"]["HMF-Audit"]["leaves_per_segment"] = segment_leaves
    (results / "workload_manifest.json").write_text(
        __import__("json").dumps(manifest, indent=2, sort_keys=True) + "\n", encoding="utf-8")

    fb.write_trials(results / "latency_trials.csv", trials, manifest)
    summary = fb.summarize(trials)
    fb.write_summary(results / "latency_summary.csv", summary)
    fb.plot_one_figure(
        results / "figure2_localization_latency.png", summary, 2, "Tamper Localization Latency",
        ["HMF-Audit", "EVSEB"], {"HMF-Audit": "HMF-Audit", "EVSEB": "EVSEB"},
        "The systems produce different localization evidence; the lines characterize native procedures "
        "and are not strict speedups.",
        args.count,
    )
    print(f"validated trials: {len(trials)}")
    print(f"summary CSV:      {results / 'latency_summary.csv'}")
    print(f"Figure 2:         {results / 'figure2_localization_latency.png'}")


# --------------------------------------------------------------- experiment 3
def nearest_rank(values: list[float], quantile: float) -> float:
    ordered = sorted(values)
    return ordered[max(0, math.ceil(quantile * len(ordered)) - 1)]


def run_exp3(args: argparse.Namespace) -> None:
    results = Path(args.results_dir).resolve()
    raw = results / "ablation_latency_raw.csv"
    if not raw.exists():
        raise SystemExit(f"missing {raw}")
    groups: dict[tuple[str, str, int], list[dict[str, str]]] = defaultdict(list)
    query_sets: dict[tuple[str, int], set[str]] = defaultdict(set)
    warmups = 0
    with raw.open(newline="", encoding="utf-8") as source:
        for row in csv.DictReader(source):
            if row["is_warmup"].lower() == "true":
                warmups += 1
                continue
            if row["verification_success"].lower() != "true":
                raise SystemExit("an ablation measurement did not verify; refusing to summarize")
            groups[(row["variant"], row["distribution"], int(row["q"]))].append(row)
            query_sets[(row["distribution"], int(row["q"]))].add(row.get("query_set", "1"))
    if not groups:
        raise SystemExit("ablation CSV has no measured rows")
    sizes = sorted({q for (_, _, q) in groups})
    # The fixed placements are clustered-2 and scattered; the native query sets are clustered and
    # scattered, with several query sets per cell whose measured rounds are pooled.
    present = {distribution for (_, distribution, _) in groups}
    distributions = [d for d in ("clustered-2", "clustered", "scattered") if d in present]
    titles = {**DISTRIBUTION_TITLES, "clustered": "Clustered Requested IDs"}
    for variant in ABLATION_VARIANTS:
        for distribution in distributions:
            for q in sizes:
                count = len(groups.get((variant, distribution, q), []))
                expected = args.count * max(1, len(query_sets.get((distribution, q), ())))
                if count != expected:
                    raise SystemExit(f"{variant}/{distribution}/q={q}: {count} measured rows, expected {expected}")

    rows = []
    for (variant, distribution, q), samples in sorted(groups.items()):
        latency = [float(s["latency_ms"]) for s in samples]
        row: dict[str, Any] = {
            "variant": variant, "distribution": distribution, "q": q, "trials": len(latency),
            "mean_ms": statistics.fmean(latency),
            "stddev_ms": statistics.stdev(latency) if len(latency) > 1 else "",
            "p50_ms": nearest_rank(latency, 0.5), "p95_ms": nearest_rank(latency, 0.95),
            "min_ms": min(latency), "max_ms": max(latency),
        }
        # Structural counters are identical in every round of a cell; the mean
        # is reported (and equals each round's value).
        for column in ("logical_proof_nodes", "unique_proof_nodes", "duplicate_proof_nodes", "proof_bytes",
                       "cassandra_reads", "cassandra_partitions", "cassandra_requested_key_occurrences",
                       "cassandra_unique_rows_returned", "postgres_reads"):
            row[column] = statistics.fmean(float(s[column]) for s in samples)
        for column in ("planning_ms", "cassandra_ms", "postgres_ms", "verification_ms"):
            row[column + "_p50"] = nearest_rank([float(s[column]) for s in samples], 0.5)
        rows.append(row)
    fields = list(rows[0].keys())
    with (results / "ablation_summary.csv").open("w", newline="", encoding="utf-8") as destination:
        writer = csv.DictWriter(destination, fieldnames=fields)
        writer.writeheader()
        for row in rows:
            writer.writerow({k: (f"{v:.9g}" if isinstance(v, float) else v) for k, v in row.items()})

    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        print("matplotlib is not installed; ablation_summary.csv written, figure skipped")
        return
    fig, axes = plt.subplots(1, len(distributions), figsize=(12, 5.5), sharey=True, squeeze=False)
    axes = axes[0]
    for axis, distribution in zip(axes, distributions):
        for variant in ABLATION_VARIANTS:
            points = sorted((r for r in rows if r["variant"] == variant and r["distribution"] == distribution),
                            key=lambda r: r["q"])
            medians = [r["p50_ms"] for r in points]
            upper = [max(r["p95_ms"] - r["p50_ms"], 0.0) for r in points]
            axis.errorbar([r["q"] for r in points], medians, yerr=[[0.0] * len(upper), upper], marker="o",
                          markersize=5, linewidth=2, capsize=3, label=ABLATION_LABELS[variant],
                          color=ABLATION_COLORS[variant])
        axis.set_xscale("log", base=2)
        axis.set_yscale("log")
        axis.set_xticks(sizes)
        axis.set_xticklabels([f"$2^{{{int(math.log2(q))}}}$" for q in sizes])
        axis.set_xlabel("Requested records, q")
        axis.set_title(titles[distribution])
        axis.grid(True, which="major", alpha=0.25)
        axis.legend(loc="upper left", fontsize=9)
    axes[0].set_ylabel("Latency (ms, log scale): P50, whisker to P95")
    fig.suptitle("HMF-Audit component ablation")
    fig.tight_layout()
    fig.savefig(results / "figure3_ablation_latency.png", dpi=200, bbox_inches="tight")
    plt.close(fig)
    print(f"ablation summary: {results / 'ablation_summary.csv'}")
    print(f"Figure:           {results / 'figure3_ablation_latency.png'}")


# --------------------------------------------------------------------- compare
# Metrics that vary with timing or memory behaviour are not structural counters.
TIMING_WORDS = ("_ns", "_ms", "warmup", "bytes", "allocs", "b_per_op")


def read_summary(path: Path) -> dict[tuple[str, ...], dict[str, str]]:
    with path.open(newline="", encoding="utf-8-sig") as source:
        rows = list(csv.DictReader(source))
    if rows and "variant" in rows[0]:
        key_fields = ("variant", "distribution", "q")
    else:
        key_fields = ("system", "benchmark", "query_distribution", "requested_count_q")
    return {tuple(row[f] for f in key_fields): row for row in rows}


def run_compare(args: argparse.Namespace) -> None:
    old, new = read_summary(Path(args.old)), read_summary(Path(args.new))
    shared = sorted(set(old) & set(new))
    if not shared:
        raise SystemExit("the two summaries have no points in common")
    ratios: list[float] = []
    structural_mismatch = 0
    print(f"{'point':<70} {'p50 old':>10} {'p50 new':>10} {'new/old':>8}  structural")
    for key in shared:
        o, n = old[key], new[key]
        p_old, p_new = float(o["p50_ms"]), float(n["p50_ms"])
        ratio = p_new / p_old if p_old else float("nan")
        ratios.append(ratio)
        columns = [c for c in o if c in n and (c.startswith("metric_") and c.endswith("_mean")
                                              or c.endswith("_nodes") or c.endswith("_reads")
                                              or c.endswith("_partitions") or c.endswith("_occurrences")
                                              or c.endswith("_returned"))
                   and not any(word in c for word in TIMING_WORDS)]
        different = [c for c in columns if o[c] != "" and n[c] != "" and not math.isclose(
            float(o[c]), float(n[c]), rel_tol=1e-9, abs_tol=1e-9)]
        structural_mismatch += bool(different)
        print(f"{'/'.join(key):<70} {p_old:>10.3f} {p_new:>10.3f} {ratio:>8.2f}  "
              f"{'same' if not different else 'DIFFERS: ' + ','.join(different[:3])}")
    finite = [r for r in ratios if math.isfinite(r)]
    print(f"\npoints compared: {len(shared)}; structural counters identical at "
          f"{len(shared) - structural_mismatch}/{len(shared)}")
    print(f"P50 new/old: median {statistics.median(finite):.2f}, min {min(finite):.2f}, max {max(finite):.2f}, "
          f"within +/-{args.tolerance:.0%}: {sum(abs(r - 1) <= args.tolerance for r in finite)}/{len(finite)}")


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = parser.add_subparsers(dest="command", required=True)

    env = sub.add_parser("env")
    for name, kind in (("--out", str), ("--experiment", str), ("--dataset", str), ("--dataset-sha256", str),
                       ("--source-dataset", str), ("--bench-time", str), ("--systems", str)):
        env.add_argument(name, type=kind, required=True)
    for name in ("--per-region", "--regions", "--count", "--warmup"):
        env.add_argument(name, type=int, required=True)
    env.add_argument("--shards-per-region", type=int, default=1)
    env.add_argument("--segment-leaves", type=int, default=8192)
    env.add_argument("--dht-peers", type=int, default=16)
    env.add_argument("--deployment", default="local Docker (systems/fair-persistence)")
    env.add_argument("--postgres-endpoint", default="127.0.0.1:55432/fair_benchmark")
    env.add_argument("--cassandra-endpoint", default="127.0.0.1:19042")
    env.add_argument("--anchor-endpoint", default="none (in-memory chains)")
    env.add_argument("--query-dir", default=os.environ.get("FAIR_QUERY_DIR", ""))
    env.add_argument("--dht-max-q", type=int, default=int(os.environ.get("FAIR_DHT_MAX_Q", "2048")))
    env.set_defaults(handler=write_environment)

    for name, handler in (("exp1", run_exp1), ("exp2", run_exp2), ("exp3", run_exp3)):
        p = sub.add_parser(name)
        p.add_argument("--results-dir", required=True)
        p.add_argument("--count", type=int, required=True)
        if name != "exp3":
            p.add_argument("--dataset", required=True)
            p.add_argument("--warmup", type=int, required=True)
            p.add_argument("--bench-time", required=True)
            p.add_argument("--systems", default="hmf-audit,dht-backed,blas,evseb")
            p.add_argument("--query-dir", default=os.environ.get("FAIR_QUERY_DIR", ""))
            p.add_argument("--dht-max-q", type=int, default=int(os.environ.get("FAIR_DHT_MAX_Q", "2048")))
        p.set_defaults(handler=handler)

    compare = sub.add_parser("compare")
    compare.add_argument("--old", required=True)
    compare.add_argument("--new", required=True)
    compare.add_argument("--tolerance", type=float, default=0.25)
    compare.set_defaults(handler=run_compare)

    args = parser.parse_args()
    args.handler(args)


if __name__ == "__main__":
    main()
