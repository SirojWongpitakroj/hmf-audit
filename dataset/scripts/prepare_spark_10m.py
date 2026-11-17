#!/usr/bin/env python3
"""
prepare_spark_10m.py - download the Loghub Spark logs and deterministically
derive the 10,000,000-record benchmark subset from them. One command:

    python dataset/scripts/prepare_spark_10m.py

Steps (each is skipped when its output is already present and verified):

  1. raw       download Spark.tar.gz from the Loghub Zenodo record
               (https://zenodo.org/records/8196385, CC BY 4.0, project
               https://github.com/logpai/loghub), check its MD5, extract it
               safely, and put the application_*/ folders in <dest>/Spark/.
               Every one of the 3,852 container logs is then checked against
               the SHA-256 recorded in dataset/combined/spark_uniform_combined.config.json.
  2. combined  dataset/scripts/gen_spark_combined.py (4 regions, 1000 tenants,
               seed 42) -> 33,236,604 records, one per raw log line.
  3. subset    dataset/scripts/select_fair_subset.go: 2,500,000 evenly spaced
               records from each of R0..R3 (floor(k*(M-1)/(n-1)), no random
               numbers) -> 10,000,000 records. The result's SHA-256 is compared
               with the pinned value used by every reported experiment.

Nothing here uses randomness, the clock or the machine, so the same raw logs
always give a byte-identical subset. If the final hash does not match, the file
is NOT put at the output path and the script exits with status 3.

Requirements: Python 3.9+ (standard library only), Go 1.27 (step 3), and about
25 GB of free disk (2.75 GiB raw + 12 GB combined + 3.7 GB subset).

Options:
  --dest-dir DIR     where Spark/, combined/ and downloads/ live (default: dataset/)
  --output FILE      subset path (default: the path the experiment pipeline reads,
                     systems/fair-results-10m-localization/spark_stratified_10000000.jsonl;
                     with --dest-dir: <dest-dir>/spark_stratified_10000000.jsonl)
  --stop-after STEP  raw | combined | subset (default subset)
  --force            replace a Spark/ folder that does not verify (moved aside, not deleted)
"""

import argparse
import hashlib
import json
import os
import shutil
import subprocess
import sys
import tarfile
import time
import urllib.error
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
SCRIPTS = REPO / "dataset" / "scripts"
REFERENCE_CONFIG = REPO / "dataset" / "combined" / "spark_uniform_combined.config.json"
REFERENCE_VALIDATION = REPO / "dataset" / "combined" / "spark_uniform_combined.validation.json"
DEFAULT_SUBSET = REPO / "systems" / "fair-results-10m-localization" / "spark_stratified_10000000.jsonl"

ARCHIVE_URL = "https://zenodo.org/records/8196385/files/Spark.tar.gz?download=1"
ARCHIVE_MD5 = "31ddaff179f6c1ae5203770138156b17"  # shown on the Zenodo record page
RAW_LINES = 33_236_604                             # Loghub README, and our validation.json
REGIONS, TENANTS, SEED = 4, 1000, "42"
PER_REGION = 2_500_000
SUBSET_RECORDS = REGIONS * PER_REGION
SUBSET_SHA256 = "a8fa53db3327ce5b21cc61b249276817d81b2ee29a6facf64f48619d215dba82"
MIN_FREE_GB = 25


def log(message):
    print(f"[{time.strftime('%H:%M:%S')}] {message}", file=sys.stderr, flush=True)


def die(message, code=1):
    print(f"error: {message}", file=sys.stderr)
    sys.exit(code)


def digest_file(path, algorithm):
    h = hashlib.new(algorithm)
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def count_lines(path):
    n = 0
    with open(path, "rb") as source:
        for chunk in iter(lambda: source.read(1 << 22), b""):
            n += chunk.count(b"\n")
    return n


def load_reference():
    if not REFERENCE_CONFIG.is_file() or not REFERENCE_VALIDATION.is_file():
        die(f"reference files are missing: {REFERENCE_CONFIG} / {REFERENCE_VALIDATION}")
    config = json.loads(REFERENCE_CONFIG.read_text(encoding="utf-8"))
    validation = json.loads(REFERENCE_VALIDATION.read_text(encoding="utf-8"))
    return config["input_files"], validation


# --------------------------------------------------------------------- step 1
def download(dest):
    if dest.is_file() and digest_file(dest, "md5") == ARCHIVE_MD5:
        log(f"archive already downloaded and verified: {dest}")
        return
    dest.parent.mkdir(parents=True, exist_ok=True)
    part = dest.with_name(dest.name + ".part")
    attempts = 0
    while True:
        have = part.stat().st_size if part.exists() else 0
        headers = {"User-Agent": "spark-dataset-prep/1.0"}
        if have:
            headers["Range"] = f"bytes={have}-"
        try:
            log(f"downloading {ARCHIVE_URL}" + (f" (resuming at {have >> 20} MB)" if have else ""))
            with urllib.request.urlopen(urllib.request.Request(ARCHIVE_URL, headers=headers), timeout=60) as resp:
                resumed = have > 0 and resp.status == 206
                length = int(resp.headers.get("Content-Length") or 0)
                total = (have + length) if resumed else length
                done = have if resumed else 0
                last = time.time()
                with open(part, "ab" if resumed else "wb") as out:
                    while True:
                        chunk = resp.read(1 << 20)
                        if not chunk:
                            break
                        out.write(chunk)
                        done += len(chunk)
                        if time.time() - last > 10:
                            last = time.time()
                            log(f"  {done >> 20} MB" + (f" of {total >> 20} MB" if total else ""))
            if total and part.stat().st_size != total:
                raise OSError(f"short read: {part.stat().st_size} of {total} bytes")
            break
        except urllib.error.HTTPError as err:
            if err.code == 416 and have:  # the partial file is already complete
                break
            attempts += 1
            if attempts >= 6:
                die(f"download failed: {err}")
            log(f"download error ({err}); retrying")
            time.sleep(min(30, 2 ** attempts))
        except (urllib.error.URLError, OSError) as err:
            attempts += 1
            if attempts >= 6:
                die(f"download failed after {attempts} attempts: {err}")
            log(f"download error ({err}); retrying")
            time.sleep(min(30, 2 ** attempts))
    part.replace(dest)
    got = digest_file(dest, "md5")
    if got != ARCHIVE_MD5:
        dest.unlink()
        die(f"archive MD5 is {got}, expected {ARCHIVE_MD5}; the file was deleted")
    log("archive MD5 verified")


def safe_extract(archive, staging):
    """Extract only regular files and directories that stay inside staging."""
    staging.mkdir(parents=True)
    root = os.path.realpath(staging)
    count = 0
    with tarfile.open(archive, "r:gz") as tar:
        for member in tar:
            if not (member.isfile() or member.isdir()):
                die(f"refusing archive member of unsupported type: {member.name}")
            target = os.path.realpath(os.path.join(root, member.name))
            if os.path.commonpath([root, target]) != root:
                die(f"refusing archive member outside the target folder: {member.name}")
            if hasattr(tarfile, "data_filter"):
                tar.extract(member, root, filter="data")
            else:
                tar.extract(member, root)
            count += 1
            if count % 1000 == 0:
                log(f"  extracted {count} entries")
    log(f"extracted {count} entries")


def find_application_dirs(staging):
    found = []
    for current, dirs, _ in os.walk(staging):
        keep = []
        for name in sorted(dirs):
            if name.startswith("application_"):
                found.append(Path(current) / name)
            else:
                keep.append(name)
        dirs[:] = keep
    return found


def verify_raw(raw_dir, reference):
    expected = {item["file"] for item in reference}
    ok, missing, bad = 0, [], []
    for index, item in enumerate(reference, 1):
        path = raw_dir / item["file"]
        if not path.is_file():
            missing.append(item["file"])
        elif digest_file(path, "sha256") != item["sha256"]:
            bad.append(item["file"])
        else:
            ok += 1
        if index % 1000 == 0:
            log(f"  verified {index} of {len(reference)} raw files")
    extra = []
    if raw_dir.is_dir():
        for app in sorted(os.listdir(raw_dir)):
            folder = raw_dir / app
            if folder.is_dir():
                extra += [f"{app}/{n}" for n in os.listdir(folder)
                          if n.endswith(".log") and f"{app}/{n}" not in expected]
    return ok, missing, bad, extra


def step_raw(dest, reference, force):
    raw_dir = dest / "Spark"
    if raw_dir.is_dir() and any(raw_dir.iterdir()):
        log(f"checking the existing raw logs in {raw_dir}")
        ok, missing, bad, extra = verify_raw(raw_dir, reference)
        if ok == len(reference) and not (missing or bad or extra):
            log(f"raw logs verified: {ok} files match the recorded SHA-256 values")
            return raw_dir
        log(f"existing raw logs do NOT verify: ok={ok} missing={len(missing)} "
            f"mismatch={len(bad)} extra={len(extra)}")
        if not force:
            die(f"{raw_dir} does not match the recorded dataset; move it away or pass --force")
        aside = raw_dir.with_name(f"Spark.replaced-{time.strftime('%Y%m%d-%H%M%S')}")
        raw_dir.rename(aside)
        log(f"moved the old folder aside: {aside}")

    archive = dest / "downloads" / "Spark.tar.gz"
    download(archive)
    staging = dest / ".spark-extract"
    if staging.exists():
        shutil.rmtree(staging)
    safe_extract(archive, staging)
    apps = find_application_dirs(staging)
    if not apps:
        die("no application_* folders found inside the archive; its layout is not what was expected")
    log(f"found {len(apps)} application folders; placing them in {raw_dir}")
    raw_dir.mkdir(parents=True, exist_ok=True)
    for app in apps:
        shutil.move(str(app), str(raw_dir / app.name))
    shutil.rmtree(staging)

    ok, missing, bad, extra = verify_raw(raw_dir, reference)
    if ok != len(reference) or missing or bad or extra:
        die(f"the downloaded logs differ from the recorded dataset: ok={ok} missing={len(missing)} "
            f"mismatch={len(bad)} extra={len(extra)} (first problems: {(missing + bad + extra)[:3]})", 3)
    log(f"raw logs verified: {ok} files match the recorded SHA-256 values")
    return raw_dir


# --------------------------------------------------------------------- step 2
def step_combined(dest, raw_dir, reference, validation, force):
    combined_dir = dest / "combined"
    combined = combined_dir / "spark_uniform_combined.jsonl"
    if combined.is_file() and not force:
        lines = count_lines(combined)
        if lines == RAW_LINES:
            log(f"combined file already present with {lines} records: {combined}")
            return combined
        log(f"existing combined file has {lines} records, expected {RAW_LINES}; rebuilding")
    build = combined_dir / f".build-{os.getpid()}"
    build.mkdir(parents=True, exist_ok=True)
    out = build / "spark_uniform_combined.jsonl"
    log("generating the combined file (33.2M records; this takes a while)")
    subprocess.run([sys.executable, "-I", str(SCRIPTS / "gen_spark_combined.py"),
                    "--raw-dir", str(raw_dir), "--out", str(out),
                    "--num-regions", str(REGIONS), "--num-tenants", str(TENANTS), "--seed", SEED],
                   check=True, stdout=subprocess.DEVNULL)
    new_validation = json.loads((build / "spark_uniform_combined.validation.json").read_text(encoding="utf-8"))
    new_config = json.loads((build / "spark_uniform_combined.config.json").read_text(encoding="utf-8"))
    problems = []
    if new_validation["total_records"] != RAW_LINES:
        problems.append(f"total_records {new_validation['total_records']} != {RAW_LINES}")
    if new_validation["record_count_per_region"] != validation["record_count_per_region"]:
        problems.append("per-region record counts differ from the recorded validation")
    if new_config["input_files"] != reference:
        problems.append("input file list or hashes differ from the recorded config")
    if problems:
        die("the regenerated combined file does not match the recorded one: " + "; ".join(problems), 3)
    combined_dir.mkdir(parents=True, exist_ok=True)
    os.replace(out, combined)
    for name in ("spark_uniform_combined.config.json", "spark_uniform_combined.validation.json"):
        if not (combined_dir / name).exists():
            shutil.copy2(build / name, combined_dir / name)
    shutil.rmtree(build)
    log(f"combined file written and cross-checked against the recorded validation: {combined}")
    return combined


# --------------------------------------------------------------------- step 3
def step_subset(combined, output, force):
    if output.is_file() and not force:
        if digest_file(output, "sha256") == SUBSET_SHA256:
            log(f"subset already present and matches the pinned SHA-256: {output}")
            return
        log("existing subset does not match the pinned SHA-256; rebuilding")
    if shutil.which("go") is None:
        die("go is not installed (needed to select the subset)")
    output.parent.mkdir(parents=True, exist_ok=True)
    partial = output.with_name(output.name + ".partial")
    log(f"selecting {PER_REGION} evenly spaced records from each of {REGIONS} regions")
    subprocess.run(["go", "run", str(SCRIPTS / "select_fair_subset.go"), "-input", str(combined),
                    "-output", str(partial), "-regions", str(REGIONS), "-per-region", str(PER_REGION)],
                   check=True, env={**os.environ, "CGO_ENABLED": "0"})
    lines = count_lines(partial)
    got = digest_file(partial, "sha256")
    if lines != SUBSET_RECORDS or got != SUBSET_SHA256:
        mismatch = output.with_name(output.name + ".mismatch")
        os.replace(partial, mismatch)
        die(f"the subset does not match the pinned one: {lines} records, sha256 {got}\n"
            f"       expected {SUBSET_RECORDS} records, sha256 {SUBSET_SHA256}\n"
            f"       the file was kept as {mismatch} and NOT used", 3)
    os.replace(partial, output)
    log(f"subset written: {lines} records, sha256 {got} == pinned value  [REPRODUCED]")


def main():
    parser = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    parser.add_argument("--dest-dir", type=Path, default=None)
    parser.add_argument("--output", type=Path, default=None)
    parser.add_argument("--stop-after", choices=("raw", "combined", "subset"), default="subset")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    if sys.version_info < (3, 9):
        die("Python 3.9 or newer is required")
    dest = (args.dest_dir or REPO / "dataset").resolve()
    if args.output:
        output = args.output.resolve()
    elif args.dest_dir:
        output = dest / "spark_stratified_10000000.jsonl"
    else:
        output = DEFAULT_SUBSET
    reference, validation = load_reference()
    dest.mkdir(parents=True, exist_ok=True)
    free_gb = shutil.disk_usage(dest).free / 2 ** 30
    if free_gb < MIN_FREE_GB and args.stop_after != "raw":
        die(f"only {free_gb:.0f} GB free in {dest}; about {MIN_FREE_GB} GB are needed")
    log(f"destination {dest}; subset output {output}; {free_gb:.0f} GB free")

    # The 10M subset is the only thing the experiments read. If it is already in
    # place and matches the pinned SHA-256, the raw and combined data are not needed.
    if args.stop_after == "subset" and not args.force and output.is_file():
        if digest_file(output, "sha256") == SUBSET_SHA256:
            log(f"subset already present and matches the pinned SHA-256: {output}")
            log("nothing to download or rebuild (use --force to rebuild from the raw logs)")
            return
        log("existing subset does not match the pinned SHA-256; rebuilding from the raw logs")

    # Only the combined file is wanted (e.g. a 100k run): if it is already complete
    # there is no need for the raw logs, so do not download them again.
    combined_file = dest / "combined" / "spark_uniform_combined.jsonl"
    if args.stop_after == "combined" and not args.force and combined_file.is_file():
        if count_lines(combined_file) == RAW_LINES:
            log(f"combined file already present with {RAW_LINES} records: {combined_file}")
            return

    raw_dir = step_raw(dest, reference, args.force)
    if args.stop_after == "raw":
        return
    combined = step_combined(dest, raw_dir, reference, validation, args.force)
    if args.stop_after == "combined":
        return
    step_subset(combined, output, args.force)
    log("done. Next: cd systems/experiments/scripts && ./run_pipeline.sh   (or .\\run_pipeline.ps1)")


if __name__ == "__main__":
    main()
