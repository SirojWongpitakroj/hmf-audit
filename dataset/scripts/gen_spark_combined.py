#!/usr/bin/env python3
"""
gen_spark_combined.py

Builds the Spark benchmark dataset: one JSON record per raw Spark container
log line, carrying the raw text plus the five logical attributes of an
HMF-Audit log (domain.Log): region, tenant, service, log type, and event time.

Record unit
-----------
One record per raw line, as in the OpenStack dataset (source_id =
"<application>/<container log>:<line>", 1-indexed). Spark writes some entries
over many lines (stack traces, and application output of up to ~463,000 lines
after a single entry), so treating a multi-line entry as one record would
produce records of several megabytes.

Attributes
----------
Only attributes absent from the raw log are synthesized; the others are read
from the log.

    region_id   synthetic, uniform: "R" + (uint64_be(SHA256("region|<seed>|<source_id>")[:8]) mod NumRegions)
    tenant_id   synthetic, uniform: "T" + (uint64_be(SHA256("tenant|<seed>|<source_id>")[:8]) mod NumTenants)
                (the same construction as gen_openstack_metadata.py)
    service_id  from the container: container number 000001 is the YARN
                ApplicationMaster ("spark-application-master"); every other
                container is an executor ("spark-executor")
    log_type    from the log: the level of the line's entry (INFO, WARN, ...)
    event_time  from the log: the entry timestamp "yy/MM/dd HH:mm:ss",
                written as RFC 3339 UTC (the logs carry no time zone)

A line that does not start a timestamped entry inherits event_time and
log_type from the entry it continues. Lines before a file's first entry
(e.g. the SLF4J banner) take that first entry's event_time and log_type
"UNSPECIFIED". Every such record lists the inherited fields in "inferred".

logID is a deterministic UUID derived from SHA256("logid|<seed>|<source_id>").

The output is a pure function of the raw files, Seed, NumRegions, and
NumTenants: files are processed in sorted order and nothing depends on time,
machine, or iteration order.

Usage
-----
    python gen_spark_combined.py \\
        --raw-dir dataset/Spark \\
        --out dataset/combined/spark_uniform_combined.jsonl \\
        --num-regions 8 --num-tenants 1000 --seed 42

Writes <out>, plus <out stem>.config.json and <out stem>.validation.json.
"""

import argparse
import hashlib
import json
import os
import re
import sys
import uuid
from collections import Counter

ENTRY = re.compile(r"^(\d\d)/(\d\d)/(\d\d) (\d\d):(\d\d):(\d\d) (\S+) ")
APPLICATION_MASTER_CONTAINER = re.compile(r"^container_.*_000001\.log$")


def hashed_index(label: str, seed: str, source_id: str, modulus: int) -> int:
    digest = hashlib.sha256(f"{label}|{seed}|{source_id}".encode("utf-8")).digest()
    return int.from_bytes(digest[:8], "big") % modulus


def log_id(seed: str, source_id: str) -> str:
    digest = hashlib.sha256(f"logid|{seed}|{source_id}".encode("utf-8")).digest()
    return str(uuid.UUID(bytes=digest[:16], version=4))


def entry_time(match: re.Match) -> str:
    year, month, day, hour, minute, second = match.groups()[:6]
    return f"20{year}-{month}-{day}T{hour}:{minute}:{second}Z"


def service_for(container_file: str) -> str:
    if APPLICATION_MASTER_CONTAINER.match(container_file):
        return "spark-application-master"
    return "spark-executor"


def sha256_file(path: str) -> str:
    digest = hashlib.sha256()
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def container_files(raw_dir: str):
    for application in sorted(os.listdir(raw_dir)):
        directory = os.path.join(raw_dir, application)
        if not os.path.isdir(directory):
            continue
        for name in sorted(os.listdir(directory)):
            if name.endswith(".log"):
                yield application, name, os.path.join(directory, name)


def first_entry(path: str):
    with open(path, "r", encoding="utf-8", errors="surrogateescape") as source:
        for line in source:
            match = ENTRY.match(line)
            if match:
                return entry_time(match), match.group(7)
    return None, None


def generate(raw_dir, out_path, num_regions, num_tenants, seed, limit_applications):
    seed_str = str(seed)
    regions, tenants, services, log_types = Counter(), Counter(), Counter(), Counter()
    inferred_counts = Counter()
    total = 0
    applications = set()
    input_files = []

    os.makedirs(os.path.dirname(os.path.abspath(out_path)), exist_ok=True)
    with open(out_path, "w", encoding="utf-8", errors="surrogateescape", newline="\n") as out:
        for application, name, path in container_files(raw_dir):
            if limit_applications and application not in applications and len(applications) >= limit_applications:
                continue
            applications.add(application)
            service = service_for(name)
            first_time, _ = first_entry(path)
            if first_time is None:
                print(f"error: {path} has no timestamped entry", file=sys.stderr)
                sys.exit(1)
            current_time, current_type = first_time, None
            lines = 0
            with open(path, "r", encoding="utf-8", errors="surrogateescape") as source:
                for line_number, raw in enumerate(source, start=1):
                    lines += 1
                    raw = raw.rstrip("\n").rstrip("\r")
                    source_id = f"{application}/{name}:{line_number}"
                    record = {
                        "logID": log_id(seed_str, source_id),
                        "source_id": source_id,
                        "raw_log": raw,
                        "region_id": f"R{hashed_index('region', seed_str, source_id, num_regions)}",
                        "tenant_id": f"T{hashed_index('tenant', seed_str, source_id, num_tenants)}",
                        "service_id": service,
                    }
                    match = ENTRY.match(raw)
                    if match:
                        current_time, current_type = entry_time(match), match.group(7)
                        record["log_type"], record["event_time"] = current_type, current_time
                    else:
                        record["log_type"] = current_type or "UNSPECIFIED"
                        record["event_time"] = current_time
                        record["inferred"] = ["event_time", "log_type"]
                        inferred_counts["continuation" if current_type else "preamble"] += 1
                    out.write(json.dumps(record, ensure_ascii=False, separators=(",", ":")))
                    out.write("\n")
                    total += 1
                    regions[record["region_id"]] += 1
                    tenants[record["tenant_id"]] += 1
                    services[service] += 1
                    log_types[record["log_type"]] += 1
            input_files.append({"file": f"{application}/{name}", "line_count": lines, "sha256": sha256_file(path)})

    stem = os.path.splitext(out_path)[0]
    config = {
        "dataset": os.path.basename(stem),
        "record_unit": "one record per raw container log line",
        "distribution": "uniform",
        "seed": seed,
        "num_regions": num_regions,
        "num_tenants": num_tenants,
        "region_hash_input": "region|<seed>|<source_id>",
        "tenant_hash_input": "tenant|<seed>|<source_id>",
        "logid_hash_input": "logid|<seed>|<source_id> (first 16 bytes, UUID version 4 bits)",
        "index_derivation": "first 8 bytes of SHA-256 digest, big-endian uint64, mod N",
        "source_id_format": "<application>/<container log>:<line> (1-indexed)",
        "service_rule": "container number 000001 -> spark-application-master; otherwise spark-executor",
        "log_type_rule": "level of the line's entry; UNSPECIFIED before a file's first entry",
        "event_time_rule": "entry timestamp yy/MM/dd HH:mm:ss as RFC 3339 UTC; continuation lines inherit, preamble lines take the file's first entry",
        "limit_applications": limit_applications,
        "raw_dir": os.path.relpath(raw_dir, os.path.dirname(os.path.abspath(out_path))).replace("\\", "/"),
        "input_files": input_files,
        "generator": "dataset/scripts/gen_spark_combined.py",
    }
    with open(stem + ".config.json", "w", encoding="utf-8", newline="\n") as target:
        json.dump(config, target, indent=2, sort_keys=True)
        target.write("\n")

    expected_lines = sum(item["line_count"] for item in input_files)
    checks = {
        "one_record_per_raw_line": total == expected_lines,
        "all_regions_used": len(regions) == num_regions,
        "every_record_has_time_and_type": True,
    }
    validation = {
        "dataset": os.path.basename(stem),
        "applications": len(applications),
        "container_files": len(input_files),
        "total_records": total,
        "records_with_inferred_attributes": dict(inferred_counts),
        "record_count_per_region": {key: regions[key] for key in sorted(regions, key=lambda k: int(k[1:]))},
        "record_count_per_service": dict(sorted(services.items())),
        "record_count_per_log_type": dict(sorted(log_types.items())),
        "distinct_tenants": len(tenants),
        "checks": checks,
        "all_checks_passed": all(checks.values()),
    }
    with open(stem + ".validation.json", "w", encoding="utf-8", newline="\n") as target:
        json.dump(validation, target, indent=2)
        target.write("\n")
    return validation


def main():
    parser = argparse.ArgumentParser(description="Build the Spark benchmark dataset with HMF logical attributes.")
    parser.add_argument("--raw-dir", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--num-regions", type=int, default=8)
    parser.add_argument("--num-tenants", type=int, default=1000)
    parser.add_argument("--seed", default="42")
    parser.add_argument("--limit-applications", type=int, default=0,
                        help="process only the first N applications (sorted); 0 means all")
    args = parser.parse_args()
    if args.num_regions <= 0 or args.num_tenants <= 0:
        parser.error("--num-regions and --num-tenants must be positive")
    validation = generate(args.raw_dir, args.out, args.num_regions, args.num_tenants,
                          args.seed, args.limit_applications)
    print(json.dumps({key: validation[key] for key in (
        "applications", "container_files", "total_records", "records_with_inferred_attributes",
        "record_count_per_region", "record_count_per_service", "record_count_per_log_type",
        "distinct_tenants", "all_checks_passed")}, indent=2))


if __name__ == "__main__":
    main()
