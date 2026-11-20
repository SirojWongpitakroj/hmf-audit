"""Derive BLAS queries that select exactly what each HMF-Audit query selects.

    python derive_blas_attribute_queries.py <source set dir> <new set dir> --dataset <jsonl>

An HMF-Audit query is (tenant, service, log type, region, time range [from, to] in whole seconds) and
returns every log of that group whose event time lies in the range. BLAS identifies logs by keywords, and
the attribute fixture of the fair BLAS benchmark (FAIR_BLAS_ATTRIBUTES, default on) stores the four
attributes as keywords of every entry (tenant=T3, service=spark-executor, type=INFO, region=R0) and stamps
every log with its dataset event time in milliseconds, ingesting each region in event-time order. The same
selection in BLAS is therefore the AND of the four keywords and the time range [from * 1000, to * 1000 + 999].

The new set directory holds the HMF-Audit entries and the DHT-backed/EVSEB file of the source set
unchanged, plus one BLAS entry per HMF-Audit entry with the same shape, q, set and record list. A BLAS
entry carries "layout": "event_time" and the event time of every record it must return
(record_times_ms), which the benchmark compares with what the query returns.

The script re-reads the dataset and checks, for every query, that the group's logs in the time range are
exactly the HMF-Audit record list; it stops at the first query that is not.
"""
import argparse
import collections
import datetime
import json
import re
import shutil
import sys
from pathlib import Path

LEAVES = 8192
FIELDS = {name: re.compile(rb'"%s":"([^"]*)"' % name.encode())
          for name in ("tenant_id", "service_id", "log_type", "region_id", "event_time")}


def parse_seconds(text):
    return int(datetime.datetime.strptime(text, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.timezone.utc).timestamp())


def read_region(dataset, region_records):
    """Event time (Unix seconds) of every record of region R0 and the ordinals of each group, in file order."""
    times, groups = [], collections.defaultdict(list)
    with open(dataset, "rb") as source:
        for ordinal, line in enumerate(source):
            if ordinal >= region_records:
                break
            values = {name: pattern.search(line) for name, pattern in FIELDS.items()}
            if not all(values.values()):
                sys.exit(f"record {ordinal} lacks a tenant, service, log type, region or event time")
            if values["region_id"].group(1) != b"R0":
                sys.exit(f"record {ordinal} is not in region R0; this tool covers R0 (the first region) only")
            times.append(parse_seconds(values["event_time"].group(1).decode()))
            groups[(values["tenant_id"].group(1).decode(), values["service_id"].group(1).decode(),
                    values["log_type"].group(1).decode())].append(ordinal)
    return times, groups


def blas_entry(hmf, times, groups):
    key = (hmf["tenant"], hmf["service"], hmf["log_type"])
    start, end = parse_seconds(hmf["time_range"][0]), parse_seconds(hmf["time_range"][1])
    selected = [ordinal for ordinal in groups[key] if start <= times[ordinal] <= end]
    ordinals = hmf["record_ordinals"]
    if selected != sorted(ordinals):
        sys.exit(f"HMF-Audit {hmf['shape']} q={hmf['q']} set {hmf['set']}: the group's logs in its time range "
                 f"are {len(selected)}, but the query lists {len(ordinals)}")
    terms = [f"tenant={hmf['tenant']}", f"service={hmf['service']}", f"type={hmf['log_type']}", f"region={hmf['region']}"]
    window = [start * 1000, end * 1000 + 999]
    return {
        "scheme": "BLAS", "region": hmf["region"], "shape": hmf["shape"], "q": hmf["q"], "set": hmf["set"],
        "returns": hmf["returns"], "layout": "event_time", "keyword_expression": " AND ".join(terms),
        "keyword_operator": "AND", "keyword_terms": terms, "time_range_ms": window,
        "time_range": [hmf["time_range"][0], hmf["time_range"][1]], "matches_in_whole_region": hmf["group_size"],
        "record_ordinals": ordinals, "record_times_ms": [times[ordinal] * 1000 for ordinal in ordinals],
    }


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("source")
    parser.add_argument("target")
    parser.add_argument("--dataset", required=True)
    args = parser.parse_args()
    source, target = Path(args.source), Path(args.target)
    if source.resolve() == target.resolve():
        sys.exit("the new set directory must differ from the source")
    document = json.loads((source / "R0_queries.json").read_text())
    times, groups = read_region(args.dataset, document["records"])
    kept, derived = [], []
    for entry in document["queries"]:
        if entry["scheme"] == "BLAS":
            continue
        kept.append(entry)
        if entry["scheme"] == "HMF-Audit":
            if entry.get("ranges") or entry.get("note"):
                sys.exit("an HMF-Audit entry is a multi-range batch or a note: it has no single BLAS equivalent")
            derived.append(blas_entry(entry, times, groups))
    if not derived:
        sys.exit("no HMF-Audit queries in the source set")
    target.mkdir(parents=True, exist_ok=True)
    document["queries"] = kept + derived
    document["blas_queries"] = ("derived from the HMF-Audit queries: tenant, service, type and region keywords AND "
                                "an event-time range, for the attribute fixture (layout event_time)")
    (target / "R0_queries.json").write_text(json.dumps(document))
    for name in ("dht_evseb_queries.json", "dataset.sha256"):
        if (source / name).exists():
            shutil.copy2(source / name, target / name)
    print(f"wrote {target}: {len(derived)} BLAS queries, each verified on the dataset to select exactly its HMF-Audit record list")


if __name__ == "__main__":
    main()
