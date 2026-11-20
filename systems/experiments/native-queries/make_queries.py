"""Generate the clustered/scattered query sets for one dataset.

    python make_queries.py --dataset spark_stratified_100000.jsonl --out 100000

writes <out>/R0_queries.json (HMF-Audit and BLAS native queries, region R0) and
<out>/dht_evseb_queries.json (DHT-backed and EVSEB record sets). The sets depend on the
dataset (record positions, keyword vocabulary, tree geometry), so every dataset size
needs its own directory; the experiment scripts read systems/experiments/native-queries/<records>/.

Takes about 6 minutes for the 10M subset and under a minute for 100k. Needs Python 3.9+ and numpy.
"""
import argparse
import hashlib
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent


def run(*arguments: str) -> None:
    print("+", " ".join(arguments), flush=True)
    subprocess.run([sys.executable, *arguments], check=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--dataset", required=True, help="the benchmark subset (JSONL, regions grouped R0, R1, ...)")
    parser.add_argument("--out", required=True, help="directory for the two query files")
    parser.add_argument("--shards-per-region", type=int, default=4,
                        help="HMF-Audit shards per region (must match FAIR_HMF_SHARDS_PER_REGION of the run)")
    args = parser.parse_args()
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    scratch = Path(tempfile.mkdtemp(prefix="native-queries-"))
    try:
        run(str(HERE / "parse_dataset.py"), args.dataset, str(scratch / "arrays.npz"))
        run(str(HERE / "map_dht_leaves.py"), args.dataset, str(scratch / "leaves.npz"))
        run(str(HERE / "find_queries.py"), str(scratch / "arrays.npz"), str(out / "R0_queries.json"), "R0",
            str(args.shards_per_region))
        run(str(HERE / "find_tree_queries.py"), str(scratch / "leaves.npz"), str(out / "dht_evseb_queries.json"))
    finally:
        shutil.rmtree(scratch, ignore_errors=True)
    # The experiment scripts compare this with the dataset they run on.
    digest = hashlib.sha256()
    with open(args.dataset, "rb") as source:
        for chunk in iter(lambda: source.read(1 << 20), b""):
            digest.update(chunk)
    (out / "dataset.sha256").write_text(digest.hexdigest() + "\n", encoding="ascii")
    print("wrote", out / "R0_queries.json", "and", out / "dht_evseb_queries.json", "for dataset", digest.hexdigest()[:12])


if __name__ == "__main__":
    main()
