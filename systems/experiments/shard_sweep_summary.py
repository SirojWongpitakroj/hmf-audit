#!/usr/bin/env python3
"""Summarise the HMF-Audit shard-count study (scripts/exp_shards.sh).

    python shard_sweep_summary.py <results-folder> [<more folders from repeated sweeps>]

Each folder holds shards-<S>/hmf-audit.txt (raw `go test -bench` output of the verification
benchmark, cells named <shape>/q=<q>/set=<k>). For every (shape, q) cell and shard count S the
timings of all sets and repetitions are pooled; the table gives p50 and p95 in milliseconds and the
ratio to the best S of that cell. A shard count is called a tie with the best when its p50 is
within --tie (default 5%) of the best p50, because the run-to-run noise of a cloud VM is of that
order. The recommendation is the S with the lowest geometric mean of (p50 / best p50) over all
cells, and it says how many cells it is within the tie of the best.

Writes shard_sweep_summary.csv and shard_sweep_summary.md next to the first folder's raw results.
"""
import argparse
import csv
import math
import re
import statistics
import sys
from collections import defaultdict
from pathlib import Path

LINE = re.compile(r"^Benchmark\S+?/(clustered|scattered)/q=(\d+)/set=(\d+)\S*\s+\d+\s+(\d+(?:\.\d+)?) ns/op(.*)$")
METRIC = re.compile(r"([\d.]+) ([A-Za-z\-]+/op)")


def percentile(values, fraction):
    ordered = sorted(values)
    rank = max(1, math.ceil(fraction * len(ordered)))      # nearest rank, as the other experiments
    return ordered[rank - 1]


def read(folders):
    samples = defaultdict(list)            # (S, shape, q) -> [ms]
    metrics = defaultdict(dict)            # (S, shape, q) -> last metric values
    for folder in folders:
        for raw in sorted(Path(folder).glob("shards-*/hmf-audit.txt")):
            shards = int(raw.parent.name.split("-", 1)[1])
            for line in raw.read_text(encoding="utf-8", errors="replace").splitlines():
                match = LINE.match(line)
                if not match:
                    continue
                shape, q, _, ns, rest = match.groups()
                key = (shards, shape, int(q))
                samples[key].append(float(ns) / 1e6)
                for value, name in METRIC.findall(rest):
                    metrics[key][name] = float(value)
    return samples, metrics


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("folders", nargs="+")
    parser.add_argument("--tie", type=float, default=0.05)
    args = parser.parse_args()
    samples, metrics = read(args.folders)
    if not samples:
        sys.exit("no benchmark lines found under " + ", ".join(args.folders))
    shard_counts = sorted({key[0] for key in samples})
    cells = sorted({key[1:] for key in samples}, key=lambda c: (c[0], c[1]))
    complete = [c for c in cells if all((s, *c) in samples for s in shard_counts)]
    rows, log_ratios = [], defaultdict(list)
    for cell in complete:
        p50 = {s: statistics.median(samples[(s, *cell)]) for s in shard_counts}
        best = min(p50.values())
        for s in shard_counts:
            values = samples[(s, *cell)]
            rows.append({"shape": cell[0], "q": cell[1], "shards_per_region": s, "samples": len(values),
                         "p50_ms": round(p50[s], 3), "p95_ms": round(percentile(values, 0.95), 3),
                         "mean_ms": round(statistics.fmean(values), 3), "ratio_to_best": round(p50[s] / best, 3),
                         "tie_with_best": p50[s] <= best * (1 + args.tie),
                         "proof_nodes": metrics[(s, *cell)].get("proof-nodes/op", ""),
                         "cassandra_proof_builds": metrics[(s, *cell)].get("cassandra-proof-builds/op", "")})
            log_ratios[s].append(math.log(p50[s] / best))
    out = Path(args.folders[0])
    with open(out / "shard_sweep_summary.csv", "w", newline="", encoding="utf-8") as target:
        writer = csv.DictWriter(target, fieldnames=list(rows[0]))
        writer.writeheader()
        writer.writerows(rows)

    lines = ["# HMF-Audit shard-count study", "",
             f"Shard counts {shard_counts}; {len(complete)} cells measured at every count "
             f"({len(cells) - len(complete)} skipped as incomplete). p50 / p95 in ms; `x` = p50 over the best "
             f"p50 of the cell; `=` marks a tie within {args.tie:.0%} of the best.", ""]
    header = "| shape | q | " + " | ".join(f"S={s} p50 (p95) x" for s in shard_counts) + " |"
    lines += [header, "|" + "---|" * (2 + len(shard_counts))]
    for cell in complete:
        cells_text = []
        for s in shard_counts:
            row = next(r for r in rows if (r["shape"], r["q"], r["shards_per_region"]) == (*cell, s))
            mark = "=" if row["tie_with_best"] else ""
            cells_text.append(f"{row['p50_ms']:.1f} ({row['p95_ms']:.1f}) {row['ratio_to_best']:.2f}{mark}")
        lines.append(f"| {cell[0]} | {cell[1]} | " + " | ".join(cells_text) + " |")
    score = {s: math.exp(statistics.fmean(log_ratios[s])) for s in shard_counts}
    ties = {s: sum(1 for r in rows if r["shards_per_region"] == s and r["tie_with_best"]) for s in shard_counts}
    best_s = min(score, key=score.get)
    lines += ["", "| shards per region | geometric mean of p50 / best p50 | cells within the tie |", "|---|---|---|"]
    lines += [f"| {s} | {score[s]:.3f} | {ties[s]} of {len(complete)} |" for s in shard_counts]
    others = [s for s in shard_counts if s != best_s and score[s] <= score[best_s] * (1 + args.tie)]
    lines += ["", f"**Lowest geometric mean: S = {best_s}** ({ties[best_s]} of {len(complete)} cells within the tie of the best)."]
    if others:
        lines.append(f"S = {others} are within {args.tie:.0%} of it overall, so the study does not separate them; "
                     "repeat the sweep in the opposite order before choosing between them.")
    (out / "shard_sweep_summary.md").write_text("\n".join(lines) + "\n", encoding="utf-8")
    print("\n".join(lines))


if __name__ == "__main__":
    main()
