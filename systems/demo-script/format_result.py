#!/usr/bin/env python3
"""Turn hmf-auditor's output into the demo result: log count, log IDs, status.

Reads the auditor's stdout (JSON rows, one per log, plus status lines) and its
exit code. Prints the result and exits 0 when the verification passed (VALID),
1 when it failed (REJECTED).
"""
import argparse
import json
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stdout", required=True, help="file holding the auditor's stdout")
    parser.add_argument("--query", required=True, help="one-line description of the query")
    parser.add_argument("--max-ids", type=int, default=100, help="log IDs to print; -1 for all")
    parser.add_argument("--json", action="store_true", help="print one JSON object")
    parser.add_argument("--records", type=int, default=0, help="records in the ingested dataset")
    args = parser.parse_args()

    rows, text = [], []
    with open(args.stdout, encoding="utf-8", errors="replace") as handle:
        for line in handle:
            line = line.rstrip("\r\n")
            if line.startswith("{"):
                rows.append(json.loads(line))
            elif line.strip():
                text.append(line)

    proof_failed = any(line.startswith("REJECTED: the locator proof") for line in text)
    verified = any(line.startswith("locator proof verified:") for line in text)
    if not proof_failed and not verified:
        # The auditor never reached a verified locator result: this is an error,
        # not an empty (and therefore valid) answer.
        print("error: the auditor produced no verified result; nothing was audited", file=sys.stderr)
        return 2

    rejected = [row for row in rows if row["status"] != "VALID"]
    if proof_failed:
        status, detail = "REJECTED", "the locator proof does not verify, so the list of matching logs cannot be trusted"
    elif rejected:
        status, detail = "REJECTED", f"{len(rejected)} of {len(rows)} log(s) failed verification"
    elif rows:
        status, detail = "VALID", f"all {len(rows)} log(s) verified against the anchored root"
    else:
        status, detail = "VALID", "no log matches; the locator proof verified that none was left out"

    ids = [row["log_id"] for row in rows]
    if args.json:
        print(json.dumps({
            "query": args.query, "log_count": len(rows), "status": status, "detail": detail,
            "log_ids": ids,
            "rejected": [{"log_id": row["log_id"], "reason": row.get("reason", "")} for row in rejected],
        }, indent=2))
        return 0 if status == "VALID" else 1

    print(f"Query:     {args.query}")
    if args.records:
        print(f"Dataset:   {args.records:,} ingested logs")
    print(f"Log count: {len(rows)}")
    if ids:
        shown = ids if args.max_ids < 0 else ids[: args.max_ids]
        print("Log IDs:")
        for log_id in shown:
            print(f"  {log_id}")
        if len(shown) < len(ids):
            print(f"  ... {len(ids) - len(shown)} more (use --max-ids -1 for all)")
    print(f"Status:    {status}  ({detail})")
    for row in rejected:
        print(f"  rejected: {row['log_id']}  {row.get('reason', '')}")
    return 0 if status == "VALID" else 1


if __name__ == "__main__":
    sys.exit(main())
