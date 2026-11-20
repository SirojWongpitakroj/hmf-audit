#!/usr/bin/env python3
"""
retenant_subset.py - derive the skewed-tenant benchmark subset from the existing
uniform one, without regenerating the 33.2M-record combined file.

    python dataset/scripts/retenant_subset.py \
        --input  systems/fair-results-10m-localization/spark_stratified_10000000.jsonl \
        --output systems/fair-results-10m-skewed/spark_skewed_stratified_10000000.jsonl

A record's tenant is a pure function of its source_id (gen_spark_combined.py), and the
subset selection depends only on each record's position within its region, never on its
tenant. So the skewed subset is the uniform subset with every tenant_id replaced by
skewed_tenant_index(seed, source_id). Checked on a 3-application sample against the real
generator (--tenant-model skewed): byte-identical lines. The output is written to a
.partial file and renamed when complete; its SHA-256 is written to <output>.sha256.
The input is never modified and the pinned output path is refused.
"""
import argparse
import hashlib
import importlib.util
import json
import os
import sys
from pathlib import Path

SCRIPTS = Path(__file__).resolve().parent
PINNED = SCRIPTS.parents[1] / "systems" / "fair-results-10m-localization" / "spark_stratified_10000000.jsonl"


def load_generator():
    spec = importlib.util.spec_from_file_location("gen_spark_combined", SCRIPTS / "gen_spark_combined.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--input", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--seed", default="42")
    args = parser.parse_args()
    if args.output.resolve() == PINNED.resolve() or args.output.resolve() == args.input.resolve():
        sys.exit("error: the output must be a new file, not the input or the pinned uniform subset")
    gen = load_generator()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    partial = args.output.with_name(args.output.name + ".partial")
    digest, count = hashlib.sha256(), 0
    with open(args.input, "r", encoding="utf-8", errors="surrogateescape", newline="") as source, \
            open(partial, "w", encoding="utf-8", errors="surrogateescape", newline="") as target:
        for line in source:
            record = json.loads(line)
            old = f'"tenant_id":"{record["tenant_id"]}"'
            new = f'"tenant_id":"T{gen.skewed_tenant_index(args.seed, record["source_id"])}"'
            if line.count(old) != 1:
                sys.exit(f"error: record {count}: the tenant field is not in the expected compact form")
            line = line.replace(old, new, 1)
            target.write(line)
            digest.update(line.encode("utf-8", errors="surrogateescape"))
            count += 1
    os.replace(partial, args.output)
    args.output.with_name(args.output.name + ".sha256").write_text(
        digest.hexdigest() + "  " + args.output.name + "\n", encoding="ascii")
    print(f"wrote {count} records to {args.output}, sha256 {digest.hexdigest()}")


if __name__ == "__main__":
    main()
