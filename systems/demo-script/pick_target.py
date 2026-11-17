#!/usr/bin/env python3
"""Choose which matching log tamper.sh modifies, from the auditor's output.

Prints the chosen log ID. Exit codes: 0 chosen, 2 the auditor produced no
verified result, 3 no log matches the query, 4 a matching log already fails
verification, 5 --index is out of range.
"""
import argparse
import json
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stdout", required=True)
    parser.add_argument("--index", type=int, default=0)
    args = parser.parse_args()

    rows, verified = [], False
    with open(args.stdout, encoding="utf-8", errors="replace") as handle:
        for line in handle:
            line = line.rstrip("\r\n")
            if line.startswith("{"):
                rows.append(json.loads(line))
            elif line.startswith("locator proof verified:"):
                verified = True
    if not verified:
        print("error: the auditor produced no verified result", file=sys.stderr)
        return 2
    if not rows:
        print("error: no log matches this query, so there is nothing to tamper with", file=sys.stderr)
        return 3
    failing = [row["log_id"] for row in rows if row["status"] != "VALID"]
    if failing:
        print(f"error: {len(failing)} matching log(s) already fail verification (e.g. {failing[0]}); "
              "run tamper.sh --restore, or re-ingest, before tampering again", file=sys.stderr)
        return 4
    if not 0 <= args.index < len(rows):
        print(f"error: --index {args.index} is out of range; the query matches {len(rows)} log(s) (0..{len(rows) - 1})",
              file=sys.stderr)
        return 5
    print(len(rows), rows[args.index]["log_id"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
