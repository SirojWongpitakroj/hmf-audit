#!/usr/bin/env python3
"""Report the logs that failed verification: correct content vs tampered content.

Reads the auditor's stdout (JSON rows plus status lines). For every REJECTED log
it prints:
  * the correct content   - the log as originally ingested, looked up by log ID in
                            the source dataset (the log generator's copy);
  * the tampered content  - the row the provider stores now, decrypted WITHOUT
                            authentication (only possible with the key);
  * the correct and tampered digests - the digest the hierarchy committed versus
                            the digest of the stored row. These are always shown:
                            they are what the verification actually compares, and
                            they differ whenever the content was changed.
Exit code: 0 no tampered log, 1 at least one found (or the locator proof failed),
2 the auditor produced no verified result.
"""
import argparse
import json
import os
import sys


def show(data):
    """Bytes as text; undecodable bytes are shown as \\xNN."""
    return data.decode("utf-8", errors="backslashreplace")


def load_originals(path, wanted):
    """log_id -> original raw_log, from the source dataset (one pass)."""
    found = {}
    if not wanted or not os.path.exists(path):
        return found
    with open(path, encoding="utf-8", errors="replace") as handle:
        for line in handle:
            if not any(log_id in line[:80] for log_id in wanted):
                continue
            try:
                record = json.loads(line)
            except ValueError:
                continue
            if record.get("logID") in wanted:
                found[record["logID"]] = record.get("raw_log", "")
                if len(found) == len(wanted):
                    break
    return found


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--stdout", required=True, help="file holding the auditor's stdout")
    parser.add_argument("--query", required=True)
    parser.add_argument("--dataset", required=True, help="source dataset holding the original logs")
    parser.add_argument("--json", action="store_true")
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
        print("error: the auditor produced no verified result; nothing was checked", file=sys.stderr)
        return 2

    rejected = [row for row in rows if row["status"] != "VALID"]
    originals = load_originals(args.dataset, {row["log_id"] for row in rejected})

    report = []
    for row in rejected:
        stored = bytes.fromhex(row["stored_content_hex"]) if row.get("stored_content_hex") else None
        original = originals.get(row["log_id"])
        original_bytes = original.encode("utf-8") if original is not None else None
        differs = None
        if stored is not None and original_bytes is not None:
            differs = [i for i in range(min(len(stored), len(original_bytes))) if stored[i] != original_bytes[i]]
            if len(stored) != len(original_bytes):
                differs.append(f"length {len(original_bytes)} -> {len(stored)}")
        report.append({
            "log_id": row["log_id"], "reason": row.get("reason", ""),
            "tenant_id": row["tenant_id"], "service_id": row["service_id"], "log_type": row["log_type"],
            "region_id": row["region_id"], "event_time": row["event_time"],
            "position": f"shard {row['shard']}, segment {row['segment']}, leaf {row['leaf']}",
            "correct_content": original,
            "tampered_content": show(stored) if stored is not None else None,
            "differs_at_byte_offsets": differs,
            "correct_digest": row.get("committed_digest"),
            "tampered_digest": row.get("stored_digest"),
        })

    if args.json:
        print(json.dumps({"query": args.query, "log_count": len(rows), "tampered_count": len(report),
                          "locator_proof_failed": proof_failed, "tampered": report}, indent=2))
        return 1 if (report or proof_failed) else 0

    print(f"Query:        {args.query}")
    if proof_failed:
        print("Result:       the locator proof does not verify, so the list of matching logs cannot be trusted")
        return 1
    print(f"Logs checked: {len(rows)}")
    if not report:
        print(f"Result:       no tampered log found (all {len(rows)} log(s) verified)")
        return 0
    print(f"Result:       {len(report)} tampered log(s) found")
    for number, item in enumerate(report, 1):
        print()
        print(f"Tampered log {number} of {len(report)}")
        print(f"  Log ID:    {item['log_id']}")
        print(f"  Location:  {item['position']}  (tenant={item['tenant_id']} service={item['service_id']} "
              f"type={item['log_type']} region={item['region_id']} time={item['event_time']})")
        print(f"  Reason:    {item['reason']}")
        if item["correct_content"] is not None:
            print("  Correct content (the log as ingested, from the source dataset):")
            print(f"      {item['correct_content']}")
        else:
            print("  Correct content: not available (the original is not in the source dataset); see the digest below")
        if item["tampered_content"] is not None:
            print("  Tampered content (the stored row, decrypted without authentication):")
            print(f"      {item['tampered_content']}")
        else:
            print("  Tampered content: cannot be read (no key, or the row is missing); see the digest below")
        if item["differs_at_byte_offsets"]:
            print(f"  Differs at byte offset(s): {', '.join(str(o) for o in item['differs_at_byte_offsets'][:10])}")
        print(f"  Correct digest  (committed under the anchored root): {item['correct_digest'] or '(not available)'}")
        print(f"  Tampered digest (digest of the row stored now):      {item['tampered_digest'] or '(row missing)'}")
    return 1


if __name__ == "__main__":
    sys.exit(main())
