#!/usr/bin/env python3
"""Validate, aggregate, and plot the three HMF-Audit fair-benchmark figures."""

from __future__ import annotations

import argparse
import csv
import hashlib
import json
import math
import os
import re
import statistics
from array import array
from collections import defaultdict
from itertools import combinations
from pathlib import Path
from typing import Any


REQUEST_COUNTS = (1, 16, 64, 256, 1024, 4096, 16384)
PLACEMENTS = ("clustered-2", "scattered")
PLACEMENT_TITLES = {
    "clustered-2": "Clustered Requested IDs",
    "scattered": "Scattered Requested IDs",
}
TAMPER_COUNT_BATCH_SIZE = 1 << 14
TAMPER_COUNTS = (1, 2, 4, 8, 16, 32, 64, 128, 256)
P99_MINIMUM_TRIALS = 100

# Native query sets (--query-dir): the clustered and scattered audit batches of
# systems/experiments/native-queries/<records>/ (several per shape and q) replace the fixed
# clustered-2/scattered placements in the verification experiment. None = legacy protocol.
NATIVE: dict[str, Any] | None = None
NATIVE_PLACEMENTS = ("clustered", "scattered")
NATIVE_PLACEMENT_TITLES = {"clustered": "Clustered batches", "scattered": "Scattered batches"}
NATIVE_WORKLOAD_VERSION = 9
NATIVE_SCHEME_FILES = (
    ("R0_queries.json", ("HMF-Audit", "BLAS")),
    ("dht_evseb_queries.json", ("DHT-backed", "EVSEB")),
)


def active_placements() -> tuple[str, ...]:
    return NATIVE_PLACEMENTS if NATIVE is not None else PLACEMENTS


def request_counts_for(system: str) -> tuple[int, ...]:
    """The q values a system is measured at: the protocol's, or those in the query files."""
    if NATIVE is None:
        return REQUEST_COUNTS
    return tuple(sorted({q for (name, _shape, q) in NATIVE["cells"] if name == system}))


def native_sets(system: str, placement: str, q: int) -> list[int]:
    return NATIVE["cells"].get((system, placement, q), []) if NATIVE is not None else []


def load_native(query_dir: Path, max_q: dict[str, int]) -> dict[str, Any]:
    """Read the query files: cells[(system, shape, q)] = sorted set numbers."""
    cells: dict[tuple[str, str, int], list[int]] = defaultdict(list)
    files: dict[str, str] = {}
    records = None
    for filename, schemes in NATIVE_SCHEME_FILES:
        file_path = query_dir / filename
        if not file_path.exists():
            raise SystemExit(f"missing query file {file_path}; generate it with native-queries/make_queries.py")
        data = json.loads(file_path.read_text(encoding="utf-8"))
        files[filename] = sha256_file(file_path)
        records = data.get("records", records)
        for entry in data["queries"]:
            if "note" in entry or entry["scheme"] not in schemes:
                continue
            cap = max_q.get(entry["scheme"])
            if cap is not None and entry["q"] > cap:
                continue
            cells[(entry["scheme"], entry["shape"], entry["q"])].append(entry["set"])
    for key in cells:
        cells[key].sort()
    return {"dir": str(query_dir), "cells": dict(cells), "files": files, "records": records, "max_q": dict(max_q)}

BENCHMARKS: dict[str, dict[str, Any]] = {
    "BenchmarkFairHMFVerificationCore": {
        "system": "HMF-Audit",
        "figure": 1,
        "operation": "verification",
        "comparison_role": "direct comparison",
        "input_semantics": "one authenticated ALL range returns q physical addresses; logs and HMF proof read from PostgreSQL/Cassandra",
        "output_semantics": "authenticated HMF subset-verification result",
    },
    "BenchmarkFairDHTVerificationRequest": {
        "system": "DHT-backed",
        "figure": 1,
        "operation": "verification",
        "comparison_role": "direct comparison",
        "input_semantics": "same requested dataset records retrieved from PostgreSQL and DHT proof records retrieved from Cassandra",
        "output_semantics": "authenticated DHT-backed subset-verification result",
    },
    "BenchmarkFairBLASVerificationRequest": {
        "system": "BLAS",
        "figure": 1,
        "operation": "verification",
        "comparison_role": "native-system reference",
        "input_semantics": "declared records; off-chain SP log files retrieved from PostgreSQL, on-chain indexes on the full node",
        "output_semantics": "verified native three-stage BLAS query response",
    },
    "BenchmarkFairEVBESVerificationRequest": {
        "system": "EVSEB",
        "figure": 1,
        "operation": "verification",
        "comparison_role": "direct comparison",
        "input_semantics": "same requested dataset records; ciphertexts and stored_tree proof paths retrieved from PostgreSQL",
        "output_semantics": "EVSEB integrity result and verified candidate set against the anchored root",
    },
    "BenchmarkFairHMFLocalizationAfterMismatch": {
        "system": "HMF-Audit",
        "figure": 2,
        "operation": "persistent tamper detection and localization",
        "comparison_role": "native localization",
        "input_semantics": "tampered PostgreSQL logs plus Cassandra HMF reference state",
        "output_semantics": "suspect locations and authenticated localization evidence",
    },
    "BenchmarkFairEVBESLocalizationAfterMismatch": {
        "system": "EVSEB",
        "figure": 2,
        "operation": "persistent tamper detection and localization",
        "comparison_role": "native localization",
        "input_semantics": "tampered EVSEB ciphertexts and stored_tree proof paths retrieved from PostgreSQL",
        "output_semantics": "verified candidate set excluding corrupted LIDs",
    },
    "BenchmarkFairHMFTamperCountLocalization": {
        "system": "HMF-Audit",
        "figure": 3,
        "operation": "fixed-batch persistent tamper detection and localization",
        "comparison_role": "native localization",
        "input_semantics": "fixed requested set with evenly spread tampered PostgreSQL logs plus Cassandra HMF reference state",
        "output_semantics": "suspect locations and authenticated localization evidence",
    },
    "BenchmarkFairEVBESTamperCountLocalization": {
        "system": "EVSEB",
        "figure": 3,
        "operation": "fixed-batch persistent tamper detection and localization",
        "comparison_role": "native localization",
        "input_semantics": "fixed requested set with evenly spread tampered EVSEB ciphertexts and stored_tree proof paths",
        "output_semantics": "verified candidate set excluding corrupted LIDs",
    },
}

EXPECTED_BY_FILE = {
    "hmf-audit.txt": {
        "BenchmarkFairHMFVerificationCore",
        "BenchmarkFairHMFLocalizationAfterMismatch",
        "BenchmarkFairHMFTamperCountLocalization",
    },
    "dht-backed.txt": {"BenchmarkFairDHTVerificationRequest"},
    "blas.txt": {"BenchmarkFairBLASVerificationRequest"},
    "evseb.txt": {
        "BenchmarkFairEVBESVerificationRequest",
        "BenchmarkFairEVBESLocalizationAfterMismatch",
        "BenchmarkFairEVBESTamperCountLocalization",
    },
}

DIRECT_VERIFICATION_SYSTEMS = {"HMF-Audit", "DHT-backed", "EVSEB"}

NUMBER = r"[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?"
BENCHMARK_LINE = re.compile(
    rf"(?P<benchmark>BenchmarkFair[A-Za-z0-9]+)"
    rf"/(?P<placement>clustered-2|clustered|scattered)"
    rf"/q=(?P<q>\d+)"
    rf"(?:/set=(?P<query_set>\d+))?"
    rf"(?:/tampered=(?P<tamper_count>\d+))?"
    rf"(?:-\d+)?\s+"
    rf"(?P<iterations>\d+)\s+"
    rf"(?P<latency>{NUMBER})\s+ns/op"
    rf"(?P<metrics>.*)$"
)
METRIC = re.compile(rf"(?P<value>{NUMBER})\s+(?P<unit>\S+)")


def read_text(path: Path) -> str:
    data = path.read_bytes()
    for encoding in ("utf-8-sig", "utf-16", "utf-8"):
        try:
            return data.decode(encoding)
        except UnicodeDecodeError:
            continue
    raise ValueError(f"cannot decode benchmark output {path}")


def parse_environment(path: Path) -> dict[str, str]:
    result: dict[str, str] = {}
    for line in read_text(path).splitlines():
        if "=" in line:
            key, value = line.split("=", 1)
            result[key.strip()] = value.strip()
    return result


def parse_metrics(text: str) -> dict[str, float]:
    return {
        match.group("unit"): float(match.group("value"))
        for match in METRIC.finditer(text)
    }


def parse_result_file(path: Path) -> list[dict[str, Any]]:
    allowed = EXPECTED_BY_FILE[path.name]
    rows: list[dict[str, Any]] = []
    malformed: list[int] = []
    for line_number, line in enumerate(read_text(path).splitlines(), start=1):
        match = BENCHMARK_LINE.search(line)
        if match is None:
            if "BenchmarkFair" in line and "ns/op" in line:
                malformed.append(line_number)
            continue
        benchmark = match.group("benchmark")
        if benchmark not in allowed:
            raise ValueError(
                f"{path.name}:{line_number}: unexpected benchmark {benchmark}"
            )
        q = int(match.group("q"))
        metadata = BENCHMARKS[benchmark]
        parsed_tamper_count = match.group("tamper_count")
        if metadata["figure"] == 3:
            if q != TAMPER_COUNT_BATCH_SIZE:
                raise ValueError(f"{path.name}:{line_number}: unexpected fixed q={q}")
            if parsed_tamper_count is None:
                raise ValueError(f"{path.name}:{line_number}: missing tamper count")
            tamper_count = int(parsed_tamper_count)
            if tamper_count not in TAMPER_COUNTS:
                raise ValueError(
                    f"{path.name}:{line_number}: unexpected tamper count {tamper_count}"
                )
        else:
            if q not in REQUEST_COUNTS:
                raise ValueError(f"{path.name}:{line_number}: unexpected q={q}")
            if parsed_tamper_count is not None:
                raise ValueError(f"{path.name}:{line_number}: unexpected tamper count")
            tamper_count = 1 if metadata["figure"] == 2 else 0
        rows.append(
            {
                "source_file": path.name,
                "source_line": line_number,
                "benchmark": benchmark,
                "system": metadata["system"],
                "figure": metadata["figure"],
                "operation": metadata["operation"],
                "comparison_role": metadata["comparison_role"],
                "input_semantics": metadata["input_semantics"],
                "output_semantics": metadata["output_semantics"],
                "placement": match.group("placement"),
                "query_set": (int(match.group("query_set")) if match.group("query_set") else None),
                "q": q,
                "tamper_count": tamper_count,
                "iterations_in_timing_window": int(match.group("iterations")),
                "latency_ns": float(match.group("latency")),
                "metrics": parse_metrics(match.group("metrics")),
            }
        )
    if malformed:
        raise ValueError(
            f"{path.name}: malformed fair benchmark result lines {malformed}"
        )
    return rows


def validate_trials(
    rows: list[dict[str, Any]],
    expected_repetitions: int,
    warmup: int,
    expected_by_file: dict[str, set[str]],
    verification_only: bool = False,
) -> None:
    grouped: dict[tuple[Any, ...], list[dict[str, Any]]] = defaultdict(list)
    for row in rows:
        grouped[(row["benchmark"], row["placement"], row["q"], row["tamper_count"], row.get("query_set"))].append(row)

    errors: list[str] = []
    expected_keys: set[tuple[Any, ...]] = set()
    if NATIVE is not None:
        # Every (system, shape, q, set) of the query files must have exactly the expected
        # number of trials, and nothing else may appear.
        for filename, benchmarks in expected_by_file.items():
            for benchmark in benchmarks:
                system = BENCHMARKS[benchmark]["system"]
                for placement in NATIVE_PLACEMENTS:
                    for q in request_counts_for(system):
                        for query_set in native_sets(system, placement, q):
                            key = (benchmark, placement, q, 0, query_set)
                            expected_keys.add(key)
                            count = len(grouped.get(key, ()))
                            if count != expected_repetitions:
                                errors.append(
                                    f"{filename}: {benchmark}/{placement}/q={q}/set={query_set} has "
                                    f"{count} trials, expected {expected_repetitions}"
                                )
    else:
        for filename, benchmarks in expected_by_file.items():
            for benchmark in benchmarks:
                for placement in PLACEMENTS:
                    metadata = BENCHMARKS[benchmark]
                    cases = (
                        ((TAMPER_COUNT_BATCH_SIZE, tamper_count) for tamper_count in TAMPER_COUNTS)
                        if metadata["figure"] == 3
                        else ((q, 1 if metadata["figure"] == 2 else 0) for q in REQUEST_COUNTS)
                    )
                    for q, tamper_count in cases:
                        key = (benchmark, placement, q, tamper_count, None)
                        count = len(grouped.get(key, ()))
                        if count != expected_repetitions:
                            errors.append(
                                f"{filename}: {benchmark}/{placement}/q={q}/tampered={tamper_count} has "
                                f"{count} trials, expected {expected_repetitions}"
                            )
        for benchmarks in expected_by_file.values():
            for benchmark in benchmarks:
                figure = BENCHMARKS[benchmark]["figure"]
                for placement in PLACEMENTS:
                    if figure == 3:
                        expected_keys.update(
                            (benchmark, placement, TAMPER_COUNT_BATCH_SIZE, tamper_count, None)
                            for tamper_count in TAMPER_COUNTS
                        )
                    else:
                        expected_keys.update(
                            (benchmark, placement, q, 1 if figure == 2 else 0, None)
                            for q in REQUEST_COUNTS
                        )
    for key in grouped:
        if key not in expected_keys:
            errors.append(f"unexpected benchmark case {key}")

    for row in rows:
        actual_warmup = row["metrics"].get("warmup-ops")
        if actual_warmup is None or not math.isclose(actual_warmup, warmup):
            errors.append(
                f"{row['source_file']}:{row['source_line']}: warmup-ops="
                f"{actual_warmup!r}, expected {warmup}"
            )
        if row["latency_ns"] <= 0:
            errors.append(
                f"{row['source_file']}:{row['source_line']}: nonpositive latency"
            )
        if row["iterations_in_timing_window"] <= 0:
            errors.append(
                f"{row['source_file']}:{row['source_line']}: nonpositive iterations"
            )
        wanted_version = NATIVE_WORKLOAD_VERSION if NATIVE is not None else 8
        if verification_only and row["metrics"].get("workload-version/op") != wanted_version:
            errors.append(
                f"{row['source_file']}:{row['source_line']}: missing v{wanted_version} workload marker; "
                "raw trials from another query protocol select different records"
            )
        if NATIVE is not None and row["metrics"].get("records/op") != row["q"]:
            errors.append(
                f"{row['source_file']}:{row['source_line']}: records/op="
                f"{row['metrics'].get('records/op')!r}, expected q={row['q']}"
            )
        actual_tampers = row["metrics"].get("tampered-records/op")
        if verification_only and row["benchmark"] == "BenchmarkFairHMFVerificationCore":
            entries = row["metrics"].get("all-entries/op")
            # Legacy: the range holds exactly q entries. Native: a tenant filter narrows a range
            # that covers every tenant, so the range holds at least q.
            enough = entries is not None and (entries >= row["q"] if NATIVE is not None else entries == row["q"])
            if row["metrics"].get("all-queries/op") != 1 or not enough:
                errors.append(
                    f"{row['source_file']}:{row['source_line']}: ALL did not answer q={row['q']} "
                    f"records in one query (range entries: {entries})"
                )
        if row["figure"] in (2, 3) and actual_tampers is None:
            errors.append(
                f"{row['source_file']}:{row['source_line']}: missing "
                "tampered-records/op"
            )
        elif actual_tampers is not None and not math.isclose(
            actual_tampers, row["tamper_count"]
        ):
            errors.append(
                f"{row['source_file']}:{row['source_line']}: tampered-records/op="
                f"{actual_tampers}, expected {row['tamper_count']}"
            )

    if errors:
        preview = "\n".join(f"- {error}" for error in errors[:30])
        remainder = len(errors) - 30
        if remainder > 0:
            preview += f"\n- ... and {remainder} more"
        raise ValueError("benchmark validation failed:\n" + preview)

    for samples in grouped.values():
        samples.sort(key=lambda item: item["source_line"])
        for repetition, row in enumerate(samples, start=1):
            row["repetition"] = repetition


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def source_id_digest(source_id: str) -> int:
    return int.from_bytes(
        hashlib.blake2b(source_id.encode("utf-8"), digest_size=8).digest(), "big"
    )


def scan_dataset_records(dataset: Path) -> tuple[int, list[int], array]:
    """Validate the dataset in one streaming pass.

    Returns the record count, each region's record count, and a 64-bit digest
    of every source_id. Holding digests instead of the records keeps a
    10M-record dataset in tens of megabytes.
    """
    total = 0
    region_sizes: list[int] = []
    digests = array("Q")
    with dataset.open(encoding="utf-8") as source:
        for line_number, line in enumerate(source, start=1):
            if not line.strip():
                continue
            try:
                record = json.loads(line)
            except json.JSONDecodeError as error:
                raise ValueError(
                    f"{dataset}:{line_number}: invalid JSON: {error}"
                ) from error
            required = ("source_id", "raw_log", "region_id", "tenant_id")
            if any(not record.get(field) for field in required):
                raise ValueError(f"{dataset}: record {total} has an empty field")
            # The benchmark dataset is grouped by region: R0, R1, ... in order.
            if record["region_id"] != f"R{len(region_sizes) - 1}":
                if record["region_id"] != f"R{len(region_sizes)}":
                    raise ValueError(
                        f"{dataset}: record {total} is in {record['region_id']}; "
                        "records must be grouped R0, R1, ... in order"
                    )
                region_sizes.append(0)
            region_sizes[-1] += 1
            digests.append(source_id_digest(record["source_id"]))
            total += 1
    if total == 0:
        raise ValueError(f"{dataset} is empty")
    return total, region_sizes, digests


def repeated_digests(digests: array) -> set[int]:
    try:
        import numpy
    except ImportError:
        ordered = sorted(digests)
        return {left for left, right in zip(ordered, ordered[1:]) if left == right}
    values = numpy.sort(numpy.frombuffer(digests, dtype=numpy.uint64))
    return {int(value) for value in values[:-1][values[1:] == values[:-1]]}


def assert_unique_source_ids(dataset: Path, digests: array) -> None:
    """Reject duplicate source_id values. Digests can collide, so a repeated
    digest is confirmed against the real values in a second pass."""
    repeated = repeated_digests(digests)
    if not repeated:
        return
    seen: set[str] = set()
    with dataset.open(encoding="utf-8") as source:
        for line in source:
            if not line.strip():
                continue
            source_id = json.loads(line)["source_id"]
            if source_id_digest(source_id) not in repeated:
                continue
            if source_id in seen:
                raise ValueError(f"{dataset}: duplicate source_id values")
            seen.add(source_id)


def read_source_ids(dataset: Path, wanted: set[int]) -> dict[int, str]:
    """Read only the source_id values the workloads select."""
    found: dict[int, str] = {}
    ordinal = 0
    with dataset.open(encoding="utf-8") as source:
        for line in source:
            if not line.strip():
                continue
            if ordinal in wanted:
                found[ordinal] = json.loads(line)["source_id"]
            ordinal += 1
    return found


def selected_ordinals(
    placement: str, q: int, total: int, segment_leaves: int,
    region0: int | None = None,
    region1: int | None = None,
) -> list[int]:
    if region0 is not None:
        if region1 is None or not 0 < region0 < total or not 0 < region1 <= total - region0:
            raise ValueError(f"invalid R0/R1 sizes {region0}/{region1} for total {total}")
        start, width = 0, 2 * segment_leaves
        if placement == "scattered":
            start, width = region0, total - region0
        elif placement != "clustered-2":
            raise ValueError(f"unknown query distribution {placement!r}")
        if not 0 < q <= width or 2 * segment_leaves > region0:
            raise ValueError(f"invalid {placement} q={q} for R0={region0}")
        bit_count = (width - 1).bit_length()
        result: list[int] = []
        index = 0
        while len(result) < q:
            value = index
            offset = 0
            for _ in range(bit_count):
                offset = (offset << 1) | (value & 1)
                value >>= 1
            if offset < width:
                result.append(start + offset)
            index += 1
        return result
    if placement.startswith("clustered-"):
        segments = int(placement.removeprefix("clustered-"))
        if q == 1:
            return [0]
        window = segments * segment_leaves
        if q < 16 or q > window or window > total:
            raise ValueError(
                f"invalid {placement} q={q}, segment leaves={segment_leaves}, "
                f"total={total}"
            )
        return [index * (window - 1) // (q - 1) for index in range(q)]
    if placement != "scattered":
        raise ValueError(f"unknown query distribution {placement!r}")
    if q == 1:
        return [0]
    return [index * (total - 1) // (q - 1) for index in range(q)]


def tampered_offsets(batch_size: int, tamper_count: int) -> list[int]:
    if tamper_count <= 0 or tamper_count > batch_size:
        raise ValueError(
            f"tamper count {tamper_count} must be between 1 and {batch_size}"
        )
    return [
        (2 * index + 1) * batch_size // (2 * tamper_count)
        for index in range(tamper_count)
    ]


def build_workload_manifest(
    dataset: Path,
    expected_repetitions: int,
    warmup: int,
    bench_time: str,
    metadata_seed: int,
    segment_leaves: int,
    verification_only: bool = False,
) -> dict[str, Any]:
    total, region_sizes, digests = scan_dataset_records(dataset)
    if verification_only and len(region_sizes) < 2:
        raise ValueError("verification workloads require at least R0 and R1")
    assert_unique_source_ids(dataset, digests)
    del digests

    selections = {
        (placement, q): selected_ordinals(
            placement, q, total, segment_leaves,
            region_sizes[0] if verification_only else None,
            region_sizes[1] if verification_only else None,
        )
        for placement in PLACEMENTS
        for q in REQUEST_COUNTS
    }
    source_ids = read_source_ids(
        dataset, {ordinal for ordinals in selections.values() for ordinal in ordinals}
    )

    workloads: list[dict[str, Any]] = []
    for placement in PLACEMENTS:
        for q in REQUEST_COUNTS:
            ordinals = selections[(placement, q)]
            requested = [source_ids[index] for index in ordinals]
            workload = {
                "workload_id": f"{placement}/q={q}",
                "query_distribution": placement,
                "requested_count_q": q,
                "selected_subset_ordinals": ordinals,
                "selected_record_ids": requested,
                "verification": {
                    "tamper_count": 0,
                    "tampered_record_ids": [],
                },
            }
            if not verification_only:
                tampered_offset = q // 2
                workload["localization"] = {
                    "precondition": "integrity mismatch already established",
                    "tamper_count": 1,
                    "tampered_requested_offset": tampered_offset,
                    "tampered_requested_offsets": [tampered_offset],
                    "tampered_subset_ordinal": ordinals[tampered_offset],
                    "tampered_subset_ordinals": [ordinals[tampered_offset]],
                    "tampered_record_ids": [requested[tampered_offset]],
                }
            workloads.append(workload)

    if not verification_only:
        for placement in PLACEMENTS:
            ordinals = selections[(placement, TAMPER_COUNT_BATCH_SIZE)]
            requested = [source_ids[index] for index in ordinals]
            for tamper_count in TAMPER_COUNTS:
                offsets = tampered_offsets(TAMPER_COUNT_BATCH_SIZE, tamper_count)
                workloads.append(
                {
                    "workload_id": (
                        f"{placement}/q={TAMPER_COUNT_BATCH_SIZE}/"
                        f"tampered={tamper_count}"
                    ),
                    "query_distribution": placement,
                    "requested_count_q": TAMPER_COUNT_BATCH_SIZE,
                    "selected_subset_ordinals": ordinals,
                    "selected_record_ids": requested,
                    "verification": None,
                    "localization": {
                        "precondition": "integrity mismatch already established",
                        "tamper_count": tamper_count,
                        "tampered_requested_offsets": offsets,
                        "tampered_subset_ordinals": [ordinals[offset] for offset in offsets],
                        "tampered_record_ids": [requested[offset] for offset in offsets],
                    },
                }
                )

    dataset_hash = sha256_file(dataset)
    return {
        "schema_version": 2,
        "dataset": {
            "terminology": "deterministic region-grouped benchmark dataset",
            "identifier": f"{dataset.stem}@" + dataset_hash,
            "path": str(dataset.resolve()),
            "sha256": dataset_hash,
            "total_records_N": total,
            "regions": len(region_sizes),
            "records_per_region": region_sizes,
            "metadata_seed": metadata_seed,
            "subset_selection_seed": None,
            "subset_selection_rule": (
                "For each region Ri with M source records, select source "
                "positions floor(k*(M-1)/(n-1)), k=0..n-1, where n is the "
                "region's selected count (n=M keeps every record); "
                "concatenate region blocks R0, R1, ... in order."
            ),
        },
        "benchmark_parameters": {
            "requested_counts_q": list(REQUEST_COUNTS),
            "tamper_count_experiment_batch_size_q": (
                None if verification_only else TAMPER_COUNT_BATCH_SIZE
            ),
            "tamper_count_experiment_values": (
                [] if verification_only else list(TAMPER_COUNTS)
            ),
            "query_distributions": list(PLACEMENTS),
            "measured_repetitions": expected_repetitions,
            "warmup_operations_per_case": warmup,
            "timing_window": bench_time,
            "cpu_allocation": 1,
            "blockchain_network_in_primary_timers": False,
            "persistent_database_reads_in_primary_timers": True,
            "proof_generation_in_primary_timers": True,
        },
        "selection_rules": ({
            "clustered-2": (
                "one contiguous ALL query for q ranks in the first logical "
                "window; their physical ordinals are "
                "a bit-reversal prefix over the first two R0 segments"
            ),
            "scattered": (
                "one contiguous ALL query with the same dimensions and width "
                "in the adjacent logical window; their physical ordinals are "
                "a bit-reversal prefix over the complete R1..last-region suffix"
            ),
        } if verification_only else {
            "clustered-2": (
                "q=1 selects ordinal 0; otherwise floor(i*(2*S-1)/(q-1)), "
                "i=0..q-1, where S is HMF leaves per segment"
            ),
            "scattered": (
                "floor(i*(N-1)/(q-1)), i=0..q-1; q=1 selects ordinal 0"
            ),
            "localization_tamper": "requested offset floor(q/2)",
            "tamper_count_experiment": (
                "select the center of each of t equal-width strata over the "
                "fixed requested batch, offset floor((2*i+1)*q/(2*t))"
            ),
        }),
        "system_semantics": {
            "HMF-Audit verification": (
                "one proof-verified ALL logical range query returning q physical "
                "addresses, followed by HPP subset verification"
                if verification_only else "direct subset verification"
            ),
            "DHT-backed verification": (
                "direct subset verification; proof records fetched concurrently from a Cassandra-backed "
                "DHT, as EVSEB fetches its DHT entries"
            ),
            "BLAS verification": "native three-stage proof generation and verification reference",
            "EVSEB verification": "direct subset verification against the anchored stored_tree root",
            "HMF-Audit localization": (
                "persistent mismatch detection and localization with authenticated evidence"
            ),
            "EVSEB localization": (
                "persistent ciphertext and stored_tree acquisition, mismatch detection, tracing, and verified-result construction"
            ),
            "HMF-Audit fixed-batch tamper-count localization": (
                "native persistent localization at q=16384 while tampered requested logs vary"
            ),
            "EVSEB fixed-batch tamper-count localization": (
                "native persistent localization at q=16384 while tampered requested logs vary"
            ),
        },
        "system_parameters": {
            "HMF-Audit": {
                "regions": len(region_sizes),
                "shards_per_region": 1,
                "segments_per_shard": "ceil(shard records / leaves_per_segment)",
                "leaves_per_segment": segment_leaves,
                "localization_k": 63,
                "randomness": "none in logical workload selection",
            },
            "DHT-backed": {
                "epoch_capacity": total,
                "stride": 8,
                "window": 1024,
                "log_types": ["access", "api", "auth", "error", "security"],
                "proof_store": "Cassandra dhtbacked.proof_entries",
                "lookup_concurrency": int(os.environ.get("FAIR_DHT_CONCURRENCY", "8")),
                "overlay_peers": int(os.environ.get("FAIR_DHT_PEERS", "16")),
                "routing_hops_per_lookup": max(
                    0, (int(os.environ.get("FAIR_DHT_PEERS", "16")) - 1).bit_length()
                ),
                "fixture_aead_key": "32 zero bytes",
                "signing_keys": (
                    "fresh per fixture; logical workload is unaffected"
                ),
            },
            "BLAS": {
                "seed": 1,
                "num_generators": 1,
                "entries_per_file": 128,
                "files_per_block": 8,
                "fanout": 4,
                "segment_size": 8,
                "segment_scan": True,
                "aggregation": True,
            },
            "EVSEB": {
                "merkle_arity": 4,
                "dht_store": "Cassandra evseb.dht_entries",
                "lookup_concurrency": int(os.environ.get("FAIR_EVSEB_DHT_CONCURRENCY", "8")),
                "overlay_peers": int(os.environ.get("FAIR_DHT_PEERS", "16")),
                "routing_hops_per_lookup": max(
                    0, (int(os.environ.get("FAIR_DHT_PEERS", "16")) - 1).bit_length()
                ),
                "cryptographic_randomness": (
                    "fresh OS-CSPRNG keys and IVs per fixture; "
                    "logical workload is unaffected"
                ),
            },
        },
        "workloads": workloads,
    }


def apply_native_manifest(manifest: dict[str, Any], systems: set[str]) -> None:
    """Replace the legacy workloads of a verification manifest with the native query sets."""
    assert NATIVE is not None
    workloads = []
    for filename, schemes in NATIVE_SCHEME_FILES:
        data = json.loads((Path(NATIVE["dir"]) / filename).read_text(encoding="utf-8"))
        for entry in data["queries"]:
            if "note" in entry or entry["scheme"] not in schemes or entry["scheme"] not in systems:
                continue
            if entry["q"] not in request_counts_for(entry["scheme"]):
                continue
            ordinals = entry["record_ordinals"]
            workload = {
                "workload_id": f"{entry['scheme']}/{entry['shape']}/q={entry['q']}/set={entry['set']}",
                "scheme": entry["scheme"],
                "query_distribution": entry["shape"],
                "requested_count_q": entry["q"],
                "query_set": entry["set"],
                "record_count": len(ordinals),
                "record_ordinals_sha256": hashlib.sha256(
                    ",".join(str(value) for value in sorted(ordinals)).encode()
                ).hexdigest(),
                "first_ordinal": min(ordinals),
                "last_ordinal": max(ordinals),
                "verification": {"tamper_count": 0, "tampered_record_ids": []},
            }
            for field in ("service", "log_type", "tenant", "time_range", "keyword_expression",
                          "time_range_ms", "log_types"):
                if field in entry:
                    workload[field] = entry[field]
            workloads.append(workload)
    manifest["schema_version"] = 3
    manifest["workloads"] = workloads
    manifest["benchmark_parameters"]["requested_counts_q"] = sorted(
        {q for (_name, _shape, q) in NATIVE["cells"]}
    )
    manifest["benchmark_parameters"]["query_distributions"] = list(NATIVE_PLACEMENTS)
    manifest["benchmark_parameters"]["native_query_files"] = NATIVE["files"]
    manifest["benchmark_parameters"]["native_query_sets_per_cell"] = {
        f"{name}/{shape}/q={q}": len(sets) for (name, shape, q), sets in sorted(NATIVE["cells"].items())
    }
    manifest["benchmark_parameters"]["system_max_q"] = NATIVE["max_q"]
    manifest["selection_rules"] = {
        "clustered": (
            "HMF-Audit and BLAS: a native query whose q results lie in one 8,192-leaf segment or two "
            "adjacent ones. DHT-backed and EVSEB: q consecutive leaves aligned to q, one perfect subtree."
        ),
        "scattered": (
            "HMF-Audit and BLAS: the widest native query available (spread over as much of region R0 as "
            "possible). DHT-backed: q records in as many different (log type, ancestor) pairs as exist. "
            "EVSEB: q leaves spaced N/q apart, each in a different branch."
        ),
        "sets": "up to five queries per scheme, shape and q; the trials of a cell pool all its sets",
        "source": "systems/experiments/native-queries/README.md",
    }
    manifest["system_semantics"]["HMF-Audit verification"] = (
        "one proof-verified ALL range query over the real service, log type, region and time range "
        "(all tenants; a tenant filter applies after verification), followed by HPP subset verification"
    )
    manifest["system_semantics"]["BLAS verification"] = (
        "native three-stage proof generation and verification of a time-range plus keyword query"
    )


def workload_lookup(manifest: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {item["workload_id"]: item for item in manifest["workloads"]}


def metric_column(unit: str) -> str:
    normalized = unit.lower()
    normalized = normalized.replace("%", "percent")
    normalized = normalized.replace("/", "_per_")
    normalized = re.sub(r"[^a-z0-9]+", "_", normalized).strip("_")
    return "metric_" + normalized


def write_trials(
    path: Path,
    rows: list[dict[str, Any]],
    manifest: dict[str, Any],
) -> None:
    metric_units = sorted(
        {unit for row in rows for unit in row["metrics"]},
        key=metric_column,
    )
    metric_fields = [metric_column(unit) for unit in metric_units]
    if len(set(metric_fields)) != len(metric_fields):
        raise ValueError("metric names collide after CSV normalization")

    fields = [
        "trial_id",
        "figure",
        "system",
        "benchmark",
        "operation",
        "comparison_role",
        "input_semantics",
        "output_semantics",
        "workload_id",
        "query_distribution",
        "query_set",
        "requested_count_q",
        "tamper_count",
        "tampered_record_id",
        "tampered_record_ids",
        "repetition",
        "warmup_operations",
        "iterations_in_timing_window",
        "latency_ns",
        "latency_ms",
        "source_file",
        "source_line",
        *metric_fields,
    ]
    workloads = workload_lookup(manifest)
    with path.open("w", newline="", encoding="utf-8") as destination:
        writer = csv.DictWriter(destination, fieldnames=fields)
        writer.writeheader()
        for row in sorted(
            rows,
            key=lambda item: (
                item["figure"],
                item["system"],
                item["placement"],
                item["q"],
                item.get("query_set") or 0,
                item["tamper_count"],
                item["repetition"],
            ),
        ):
            if row.get("query_set") is not None:
                workload_id = f"{row['system']}/{row['placement']}/q={row['q']}/set={row['query_set']}"
            else:
                workload_id = f"{row['placement']}/q={row['q']}"
            if row["figure"] == 3:
                workload_id += f"/tampered={row['tamper_count']}"
            workload = workloads[workload_id]
            is_localization = row["figure"] in (2, 3)
            localization = workload.get("localization", {})
            tampered_record_ids = (
                localization["tampered_record_ids"] if is_localization else []
            )
            output: dict[str, Any] = {
                "trial_id": (
                    f"{row['system']}:{row['benchmark']}:{workload_id}:"
                    f"trial-{row['repetition']}"
                ),
                "figure": row["figure"],
                "system": row["system"],
                "benchmark": row["benchmark"],
                "operation": row["operation"],
                "comparison_role": row["comparison_role"],
                "input_semantics": row["input_semantics"],
                "output_semantics": row["output_semantics"],
                "workload_id": workload_id,
                "query_distribution": row["placement"],
                "query_set": row.get("query_set") or "",
                "requested_count_q": row["q"],
                "tamper_count": row["tamper_count"],
                "tampered_record_id": (
                    tampered_record_ids[0] if tampered_record_ids else ""
                ),
                "tampered_record_ids": json.dumps(tampered_record_ids),
                "repetition": row["repetition"],
                "warmup_operations": int(row["metrics"]["warmup-ops"]),
                "iterations_in_timing_window": row["iterations_in_timing_window"],
                "latency_ns": f"{row['latency_ns']:.9g}",
                "latency_ms": f"{row['latency_ns'] / 1_000_000:.12g}",
                "source_file": row["source_file"],
                "source_line": row["source_line"],
            }
            for unit, field in zip(metric_units, metric_fields):
                value = row["metrics"].get(unit)
                output[field] = "" if value is None else f"{value:.12g}"
            writer.writerow(output)


def nearest_rank(values: list[float], quantile: float) -> float:
    if not values:
        raise ValueError("cannot calculate a percentile of no values")
    ordered = sorted(values)
    index = max(0, math.ceil(quantile * len(ordered)) - 1)
    return ordered[index]


def summarize(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    grouped: dict[tuple[Any, ...], list[dict[str, Any]]] = defaultdict(list)
    for row in rows:
        key = (
            row["figure"],
            row["system"],
            row["benchmark"],
            row["operation"],
            row["comparison_role"],
            row["input_semantics"],
            row["output_semantics"],
            row["placement"],
            row["q"],
            row["tamper_count"],
        )
        grouped[key].append(row)

    summary: list[dict[str, Any]] = []
    for key, samples in sorted(grouped.items()):
        values_ns = [row["latency_ns"] for row in samples]
        count = len(values_ns)
        metric_units = sorted({unit for row in samples for unit in row["metrics"]})
        metrics = {
            metric_column(unit) + "_mean": statistics.fmean(
                row["metrics"][unit]
                for row in samples
                if unit in row["metrics"]
            )
            for unit in metric_units
        }
        figure, system, benchmark, operation, role, inputs, outputs, placement, q, tamper_count = key
        summary.append(
            {
                "figure": figure,
                "system": system,
                "benchmark": benchmark,
                "operation": operation,
                "comparison_role": role,
                "input_semantics": inputs,
                "output_semantics": outputs,
                "query_distribution": placement,
                "requested_count_q": q,
                "tamper_count": tamper_count,
                "trials": count,
                "query_sets": len({row.get("query_set") for row in samples} - {None}) or 1,
                "mean_ms": statistics.fmean(values_ns) / 1_000_000,
                "stddev_ms": (
                    statistics.stdev(values_ns) / 1_000_000
                    if count >= 2
                    else None
                ),
                "p50_ms": nearest_rank(values_ns, 0.50) / 1_000_000,
                "p95_ms": nearest_rank(values_ns, 0.95) / 1_000_000,
                "p99_ms": (
                    nearest_rank(values_ns, 0.99) / 1_000_000
                    if count >= P99_MINIMUM_TRIALS
                    else None
                ),
                "p99_status": (
                    "reported"
                    if count >= P99_MINIMUM_TRIALS
                    else f"not reported: requires at least {P99_MINIMUM_TRIALS} trials"
                ),
                "min_ms": min(values_ns) / 1_000_000,
                "max_ms": max(values_ns) / 1_000_000,
                **metrics,
            }
        )
    return summary


def write_summary(path: Path, rows: list[dict[str, Any]]) -> None:
    base_fields = [
        "figure",
        "system",
        "benchmark",
        "operation",
        "comparison_role",
        "input_semantics",
        "output_semantics",
        "query_distribution",
        "requested_count_q",
        "tamper_count",
        "trials",
        "query_sets",
        "mean_ms",
        "stddev_ms",
        "p50_ms",
        "p95_ms",
        "p99_ms",
        "p99_status",
        "min_ms",
        "max_ms",
    ]
    metric_fields = sorted(
        {key for row in rows for key in row if key.startswith("metric_")}
    )
    fields = base_fields + metric_fields
    with path.open("w", newline="", encoding="utf-8") as destination:
        writer = csv.DictWriter(destination, fieldnames=fields)
        writer.writeheader()
        for row in rows:
            output = {
                key: (
                    ""
                    if value is None
                    else f"{value:.12g}"
                    if isinstance(value, float)
                    else value
                )
                for key, value in row.items()
            }
            writer.writerow(output)


def figure_styles() -> dict[str, dict[str, Any]]:
    return {
        "HMF-Audit": {
            "color": "#0072B2",
            "marker": "o",
            "linestyle": "-",
            "linewidth": 2.4,
        },
        "DHT-backed": {
            "color": "#009E73",
            "marker": "s",
            "linestyle": "-",
            "linewidth": 2.4,
        },
        "BLAS": {
            "color": "#D55E00",
            "marker": "^",
            "linestyle": "--",
            "linewidth": 1.8,
        },
        "EVSEB": {
            "color": "#E69F00",
            "marker": "v",
            "linestyle": "-",
            "linewidth": 2.2,
        },
    }


def plot_one_figure(
    path: Path,
    summary: list[dict[str, Any]],
    figure_number: int,
    title: str,
    systems: list[str],
    labels: dict[str, str],
    footer: str,
    expected_repetitions: int,
) -> None:
    try:
        import matplotlib

        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
        import matplotlib.ticker as mticker
    except ImportError as error:
        raise SystemExit(
            "matplotlib is required for the benchmark figures"
        ) from error

    styles = figure_styles()
    placements = active_placements()
    ticks = tuple(sorted({q for system in systems for q in request_counts_for(system)})) if NATIVE is not None else REQUEST_COUNTS
    fig, axes = plt.subplots(1, len(placements), figsize=(5.5 * len(placements), 5.8), sharey=True)
    for axis, placement in zip(axes, placements):
        for system in systems:
            selected = sorted(
                (
                    row
                    for row in summary
                    if row["figure"] == figure_number
                    and row["system"] == system
                    and row["query_distribution"] == placement
                ),
                key=lambda row: row["requested_count_q"],
            )
            if len(selected) != len(request_counts_for(system)):
                raise ValueError(
                    f"Figure {figure_number}: {system}/{placement} has "
                    f"{len(selected)} points"
                )
            x_values = [row["requested_count_q"] for row in selected]
            medians = [row["p50_ms"] for row in selected]
            style = styles[system]
            axis.plot(
                x_values,
                medians,
                color=style["color"],
                marker=style["marker"],
                linestyle=style["linestyle"],
                linewidth=style["linewidth"],
                markersize=6,
                label=labels[system],
            )
        axis.set_title(
            (NATIVE_PLACEMENT_TITLES if NATIVE is not None else PLACEMENT_TITLES).get(
                placement, placement.capitalize() + " requested IDs"
            ),
            fontsize=16,
        )
        axis.set_xlabel("Requested records, q", fontsize=14)
        axis.set_xscale("log", base=2)
        axis.set_yscale("log")
        axis.set_xticks(ticks)
        axis.set_xticklabels(
            [f"$2^{{{int(math.log2(value))}}}$" for value in ticks],
            fontsize=13,
        )
        axis.set_ylim(bottom=1.0, top=10**5)
        axis.yaxis.set_major_locator(mticker.LogLocator(base=10.0))
        axis.yaxis.set_minor_locator(mticker.NullLocator())
        axis.tick_params(axis="y", labelsize=13)
        axis.grid(True, which="major", alpha=0.25)
        axis.legend(loc="lower right", fontsize=18, frameon=True)
    axes[0].set_ylabel("Latency (ms, log scale)", fontsize=14)
    fig.suptitle(title, fontsize=19, y=0.98)
    fig.tight_layout(rect=(0.02, 0.03, 0.98, 0.88))
    fig.savefig(path, dpi=220, bbox_inches="tight")
    plt.close(fig)


def plot_tamper_count_figure(
    path: Path,
    summary: list[dict[str, Any]],
    systems: list[str],
    expected_repetitions: int,
) -> None:
    try:
        import matplotlib

        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
        import matplotlib.ticker as mticker
    except ImportError as error:
        raise SystemExit("matplotlib is required for the benchmark figures") from error

    styles = figure_styles()
    labels = {
        "HMF-Audit": "HMF-Audit",
        "EVSEB": "EVSEB",
    }
    fig, axes = plt.subplots(
        1, len(PLACEMENTS), figsize=(5.5 * len(PLACEMENTS), 5.8), sharey=True
    )
    for axis, placement in zip(axes, PLACEMENTS):
        for system in systems:
            selected = sorted(
                (
                    row
                    for row in summary
                    if row["figure"] == 3
                    and row["system"] == system
                    and row["query_distribution"] == placement
                ),
                key=lambda row: row["tamper_count"],
            )
            if len(selected) != len(TAMPER_COUNTS):
                raise ValueError(
                    f"Figure 3: {system}/{placement} has {len(selected)} points"
                )
            x_values = [row["tamper_count"] for row in selected]
            medians = [row["p50_ms"] for row in selected]
            style = styles[system]
            axis.plot(
                x_values,
                medians,
                color=style["color"],
                marker=style["marker"],
                linestyle=style["linestyle"],
                linewidth=style["linewidth"],
                markersize=6,
                label=labels[system],
            )
        axis.set_title(PLACEMENT_TITLES.get(placement, placement.capitalize() + " requested IDs"), fontsize=16)
        axis.set_xlabel("Tampered requested logs, t")
        axis.set_xscale("log", base=2)
        axis.set_yscale("log")
        axis.set_xticks(TAMPER_COUNTS)
        axis.set_xticklabels(
            [f"$2^{{{int(math.log2(value))}}}$" for value in TAMPER_COUNTS]
        )
        axis.set_ylim(bottom=100.0, top=10000.0)
        axis.yaxis.set_major_locator(mticker.LogLocator(base=10.0))
        axis.yaxis.set_minor_locator(mticker.NullLocator())
        axis.grid(True, which="major", alpha=0.25)
        axis.legend(loc="lower right", fontsize=9, frameon=True)
    axes[0].set_ylabel("Latency (ms, log scale)")
    fig.suptitle(
        "Tamper Localization vs. Tampered Logs at Fixed q = 16,384",
        fontsize=15,
        y=0.98,
    )
    fig.tight_layout(rect=(0.02, 0.03, 0.98, 0.88))
    fig.savefig(path, dpi=220, bbox_inches="tight")
    plt.close(fig)


def systems_in_figure(systems: set[str], figure: int) -> list[str]:
    order = ["HMF-Audit", "DHT-backed", "EVSEB", "BLAS"]
    present = {
        metadata["system"]
        for metadata in BENCHMARKS.values()
        if metadata["figure"] == figure and metadata["system"] in systems
    }
    return [system for system in order if system in present]


def direct_verification_phrase(systems: set[str]) -> str:
    direct = [
        system
        for system in systems_in_figure(systems, 1)
        if system in DIRECT_VERIFICATION_SYSTEMS
    ]
    if len(direct) <= 2:
        return " and ".join(direct)
    return ", ".join(direct[:-1]) + ", and " + direct[-1]


def plot_figures(
    results_dir: Path,
    summary: list[dict[str, Any]],
    expected_repetitions: int,
    systems: set[str],
    verification_only: bool = False,
) -> None:
    plot_one_figure(
        results_dir / "figure1_verification_latency.png",
        summary,
        1,
        "Verification Latency",
        systems_in_figure(systems, 1),
        {
            "HMF-Audit": "HMF-Audit",
            "DHT-backed": "DHT-backed",
            "EVSEB": "EVSEB",
            "BLAS": "BLAS",
        },
        (
            f"{direct_verification_phrase(systems)} are directly compared at the "
            "same requested IDs; BLAS retains native verification "
            "semantics. Database reads are inside every timer."
        ),
        expected_repetitions,
    )
    if verification_only:
        return
    plot_one_figure(
        results_dir / "figure2_localization_latency.png",
        summary,
        2,
        "Tamper Localization Latency",
        systems_in_figure(systems, 2),
        {
            "HMF-Audit": "HMF-Audit",
            "EVSEB": "EVSEB",
        },
        (
            "The systems produce different localization evidence; the lines "
            "characterize native procedures and are not strict speedups."
        ),
        expected_repetitions,
    )
    figure3_systems = systems_in_figure(systems, 3)
    if figure3_systems:
        plot_tamper_count_figure(
            results_dir / "figure3_tamper_count_localization_latency.png",
            summary,
            figure3_systems,
            expected_repetitions,
        )


def write_captions(
    path: Path,
    expected_repetitions: int,
    bench_time: str,
    systems: set[str],
    total_records: int,
    verification_only: bool = False,
) -> None:
    direct = direct_verification_phrase(systems)
    p99_note = (
        "P99 is reported in the summary because at least 100 trials were run."
        if expected_repetitions >= P99_MINIMUM_TRIALS
        else (
            "P99 is intentionally blank: fewer than 100 trials do not provide "
            "the configured empirical P99 resolution."
        )
    )
    text = f"""# Primary benchmark figure captions

## Figure 1 — Persistence-Inclusive Verification Latency

P50 latency with P95 upper whiskers over {expected_repetitions} retained
trials per point (timing window {bench_time}) on the deterministic
region-grouped benchmark dataset of {total_records:,} records. {direct} are
directly compared under the same requested log set. BLAS is
shown under its native verification workflow and serves as a system-level
verification reference; its latency values are therefore not interpreted
as strict speedups relative to HMF-Audit. The clustered-2 variant (two adjacent
segments) and the scattered requested-ID rule are shown in separate
panels. Each timer starts when the request
arrives and covers request-dependent PostgreSQL/Cassandra reads, proof
generation, and verification through the final result. Logs and off-chain
authenticated structures are persisted and read per request; on-chain state
(anchors, and BLAS's on-chain indexes) is served by an in-memory chain.
Blockchain communication, dataset loading, ingestion, and one-time
construction are outside the timers.

## Figure 2 — Persistence-Inclusive Tamper Detection and Localization Latency

P50 latency with P95 upper whiskers over {expected_repetitions} retained
trials per point (timing window {bench_time}) with one tampered requested
record persisted before timing. HMF-Audit retrieves current logs from
PostgreSQL and proof/reference state from Cassandra, detects the mismatch,
performs hierarchical pruning, and produces localization evidence. EVSEB
retrieves candidate ciphertexts and the proof paths of its anchored
stored_tree from PostgreSQL, recomputes affected paths,
detects the mismatch, and performs native affected-branch tracing. The systems perform
different types and amounts of authenticated work and produce different
localization evidence. The measurements characterize their persistence-inclusive
native localization procedures, not identical operations or strict
cross-system speedups.

## Figure 3 - Fixed-Batch Localization vs. Tampered-Log Count

P50 latency with P95 upper whiskers over {expected_repetitions} retained
trials per point (timing window {bench_time}) at a fixed requested batch of
16,384 logs. The number of tampered requested logs varies over
1, 2, 4, 8, 16, 32, 64, 128, and 256. For each count, tampered logs are
selected deterministically at the centers of equal-width strata across the
same requested set. HMF-Audit and EVSEB retain the native localization
procedures and output semantics described for Figure 2, so this figure
compares scaling trends rather than strict equivalent-work speedups.

All three figures use the same repetitions, warm-up policy, percentile method, CPU
allocation, and process controls. {p99_note}
"""
    if verification_only and NATIVE is not None:
        capped = ", ".join(f"{name} up to q={cap}" for name, cap in sorted(NATIVE["max_q"].items())) or "none"
        text = f"""# Primary benchmark figure captions

## Figure 1 — Persistence-Inclusive Verification Latency

P50 latency with P95 upper whiskers per point over {expected_repetitions} retained trials
per query set (timing window {bench_time}), pooled over the query sets of each point, on the
deterministic region-grouped benchmark dataset of {total_records:,} records. Each system answers
its own audit batches of q records: a clustered batch lies in one subtree or one or two adjacent
segments, a scattered batch is spread over as many subtrees, branches or segments as possible
(definitions in systems/experiments/native-queries/README.md). The batches of different schemes
are selected by each scheme's own structure and are not the same records. Each timer starts when
the request arrives and covers request-dependent PostgreSQL and Cassandra reads, proof
generation and anchored verification; the chain is in memory. Systems capped at a smaller q for
memory bounds: {capped}. {p99_note}
"""
        path.write_text(text, encoding="utf-8")
        return
    if verification_only:
        text = text.split("\n## Figure 2", maxsplit=1)[0]
        text = text.replace(
            "The clustered-2 variant (two adjacent\nsegments) and the scattered requested-ID rule are shown in separate\npanels.",
            "Each request makes one contiguous logical ALL range query returning exactly q\nauthenticated physical addresses. Both cases share the same ALL key dimensions\nand range width; only the adjacent logical window changes. Clustered-2\naddresses span two adjacent R0 HMF segments; scattered addresses span the\nR1..last physical suffix. The placements are shown in separate panels.",
        )
        text += f"\n\n{p99_note}\n"
    path.write_text(text, encoding="utf-8")


def write_fairness_matrix(
    path: Path, systems: set[str], verification_only: bool = False
) -> None:
    fields = [
        "figure",
        "system_a",
        "system_b",
        "inputs_equivalent",
        "timer_same_stage",
        "preprocessing_and_storage_consistent",
        "outputs_equivalent",
        "classification",
        "wording_guard",
    ]
    rows: list[dict[str, str | int]] = []
    verification_systems = systems_in_figure(systems, 1)
    for left, right in combinations(verification_systems, 2):
        direct = {left, right} <= DIRECT_VERIFICATION_SYSTEMS
        rows.append(
            {
                "figure": 1,
                "system_a": left,
                "system_b": right,
                "inputs_equivalent": (
                    "yes: identical dataset ordinals and source records"
                    if direct
                    else "same logical records; native request representation differs"
                ),
                "timer_same_stage": (
                    "yes: subset verification against trusted commitments"
                    if direct
                    else "both are native verification, but verified work differs"
                ),
                "preprocessing_and_storage_consistent": (
                    "yes: request-dependent persistence reads timed; native work retained"
                ),
                "outputs_equivalent": (
                    "sufficiently equivalent accept/reject integrity result"
                    if direct
                    else "no: native proof/query responsibilities differ"
                ),
                "classification": (
                    "direct comparison" if direct else "native-system reference"
                ),
                "wording_guard": (
                    "latency/scaling comparison allowed at equal q and distribution"
                    if direct
                    else "no strict speedup or fastest-to-slowest ranking"
                ),
            }
        )
    if not verification_only:
        rows.append(
            {
                "figure": 2,
                "system_a": "HMF-Audit",
                "system_b": "EVSEB",
                "inputs_equivalent": "same subset, requested IDs, and one-tamper rule",
                "timer_same_stage": (
                    "both include persistent record acquisition, mismatch detection, and localization"
                ),
                "preprocessing_and_storage_consistent": (
                    "database reads included; ingestion and external network excluded"
                ),
                "outputs_equivalent": "no: authenticated evidence responsibilities differ",
                "classification": "native localization reference",
                "wording_guard": "describe each trend; no strict cross-system speedup",
            }
        )
        rows.append(
            {
                "figure": 3,
                "system_a": "HMF-Audit",
                "system_b": "EVSEB",
                "inputs_equivalent": (
                    "same q=16384 requested IDs and evenly-spread tamper-count rule"
                ),
                "timer_same_stage": (
                    "both include persistent record acquisition, mismatch detection, and localization"
                ),
                "preprocessing_and_storage_consistent": (
                    "database reads included; tamper injection, ingestion, and external network excluded"
                ),
                "outputs_equivalent": "no: authenticated evidence responsibilities differ",
                "classification": "native localization scaling reference",
                "wording_guard": "compare within-system scaling trends; no strict cross-system speedup",
            }
        )
    with path.open("w", newline="", encoding="utf-8") as destination:
        writer = csv.DictWriter(destination, fieldnames=fields)
        writer.writeheader()
        writer.writerows(rows)


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--results-dir", required=True, type=Path)
    parser.add_argument("--dataset", required=True, type=Path)
    parser.add_argument("--expected-repetitions", required=True, type=int)
    parser.add_argument("--warmup", required=True, type=int)
    parser.add_argument("--bench-time", required=True)
    parser.add_argument("--metadata-seed", required=True, type=int)
    parser.add_argument(
        "--systems",
        default=",".join(name.removesuffix(".txt") for name in EXPECTED_BY_FILE),
        help="comma-separated result files to process, e.g. hmf-audit,blas,evseb",
        )
    parser.add_argument(
        "--verification-only",
        action="store_true",
        help="validate and render only the verification-latency experiment",
    )
    parser.add_argument(
        "--query-dir",
        type=Path,
        help="native query sets (verification only): the directory holding R0_queries.json and dht_evseb_queries.json",
    )
    parser.add_argument(
        "--max-q",
        action="append",
        default=[],
        metavar="SYSTEM=Q",
        help="largest request size measured for a system, e.g. DHT-backed=2048 (repeatable)",
    )
    return parser.parse_args()


def main() -> None:
    args = parse_arguments()
    if args.expected_repetitions <= 0 or args.warmup < 0:
        raise SystemExit("repetitions must be positive and warmup nonnegative")
    global NATIVE
    if args.query_dir is not None:
        if not args.verification_only:
            raise SystemExit("--query-dir applies to the verification experiment only (--verification-only)")
        caps: dict[str, int] = {}
        for item in args.max_q:
            name, _, value = item.partition("=")
            if name not in {"HMF-Audit", "BLAS", "DHT-backed", "EVSEB"} or not value.isdigit() or int(value) <= 0:
                raise SystemExit(f"--max-q expects SYSTEM=Q with SYSTEM one of HMF-Audit, BLAS, DHT-backed, EVSEB: {item!r}")
            caps[name] = int(value)
        NATIVE = load_native(args.query_dir.resolve(), caps)
    results_dir = args.results_dir.resolve()
    dataset = args.dataset.resolve()
    environment_path = results_dir / "environment.txt"
    if not environment_path.exists():
        raise SystemExit(f"missing {environment_path}")
    environment = parse_environment(environment_path)

    segment_leaves = int(environment.get("hmf_segment_leaves", "8192"))

    manifest = build_workload_manifest(
        dataset,
        args.expected_repetitions,
        args.warmup,
        args.bench_time,
        args.metadata_seed,
        segment_leaves,
        verification_only=args.verification_only,
    )
    recorded_hash = environment.get("benchmark_subset_sha256")
    actual_hash = manifest["dataset"]["sha256"]
    if recorded_hash != actual_hash:
        raise SystemExit(
            "benchmark subset hash differs between environment and dataset: "
            f"{recorded_hash!r} != {actual_hash!r}"
        )
    # The HMF-Audit layout is a run parameter recorded by the runner.
    hmf_parameters = manifest["system_parameters"]["HMF-Audit"]
    hmf_parameters["shards_per_region"] = int(environment.get("hmf_shards_per_region", "1"))
    hmf_parameters["leaves_per_segment"] = segment_leaves
    if NATIVE is not None:
        apply_native_manifest(manifest, {
            BENCHMARKS[benchmark]["system"]
            for name in args.systems.split(",") if name.strip()
            for benchmark in EXPECTED_BY_FILE.get(f"{name.strip()}.txt", ())
        })
    (results_dir / "workload_manifest.json").write_text(
        json.dumps(manifest, indent=2, sort_keys=True) + "\n",
        encoding="utf-8",
    )

    selected = [name.strip() for name in args.systems.split(",") if name.strip()]
    expected_by_file: dict[str, set[str]] = {}
    for name in selected:
        filename = f"{name}.txt"
        if filename not in EXPECTED_BY_FILE:
            raise SystemExit(f"unknown system {name!r}")
        expected = EXPECTED_BY_FILE[filename]
        if args.verification_only:
            expected = {benchmark for benchmark in expected if BENCHMARKS[benchmark]["figure"] == 1}
        expected_by_file[filename] = expected
    if not expected_by_file:
        raise SystemExit("no systems selected")
    systems = {
        BENCHMARKS[benchmark]["system"]
        for benchmarks in expected_by_file.values()
        for benchmark in benchmarks
    }

    trials: list[dict[str, Any]] = []
    for filename in expected_by_file:
        result_path = results_dir / filename
        if not result_path.exists():
            raise SystemExit(f"missing raw result file {result_path}")
        trials.extend(parse_result_file(result_path))
    validate_trials(
        trials, args.expected_repetitions, args.warmup, expected_by_file,
        verification_only=args.verification_only,
    )

    write_trials(results_dir / "latency_trials.csv", trials, manifest)
    summary = summarize(trials)
    write_summary(results_dir / "latency_summary.csv", summary)
    write_fairness_matrix(
        results_dir / "fairness_matrix.csv", systems,
        verification_only=args.verification_only,
    )
    write_captions(
        results_dir / "figure_captions.md",
        args.expected_repetitions,
        args.bench_time,
        systems,
        manifest["dataset"]["total_records_N"],
        verification_only=args.verification_only,
    )
    plot_figures(
        results_dir, summary, args.expected_repetitions, systems,
        verification_only=args.verification_only,
    )

    print(f"validated trials: {len(trials)}")
    print(f"workload manifest: {results_dir / 'workload_manifest.json'}")
    print(f"trial CSV:         {results_dir / 'latency_trials.csv'}")
    print(f"summary CSV:       {results_dir / 'latency_summary.csv'}")
    print(f"Figure 1:          {results_dir / 'figure1_verification_latency.png'}")
    if not args.verification_only:
        print(f"Figure 2:          {results_dir / 'figure2_localization_latency.png'}")
    if not args.verification_only and systems_in_figure(systems, 3):
        print(
            "Figure 3:          "
            f"{results_dir / 'figure3_tamper_count_localization_latency.png'}"
        )


if __name__ == "__main__":
    main()
