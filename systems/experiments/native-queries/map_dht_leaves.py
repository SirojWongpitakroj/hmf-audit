"""Where each of the 10M records lands in DHT-backed's per-log-type MK-trees.

Exactly as the fair fixture (benchmark/fair_fixture.go, ingestFairDHTDataset):
  log type = LogTypes[sha256(source_id)[0] % 5]   (access, api, auth, error, security)
  leaf index = number of earlier records of that log type
EVSEB needs nothing from here: its leaf position is the global ordinal.
"""
import hashlib
import json
import sys
import time

import numpy as np

path, out = sys.argv[1], sys.argv[2]
N = 10_000_000
types = np.zeros(N, np.uint8)
leaf = np.zeros(N, np.uint32)
counts = [0] * 5
started = time.time()
with open(path, encoding="utf-8") as source:
    for n, line in enumerate(source):
        t = hashlib.sha256(json.loads(line)["source_id"].encode()).digest()[0] % 5
        types[n] = t
        leaf[n] = counts[t]
        counts[t] += 1
        if (n + 1) % 2_000_000 == 0:
            print(f"{n + 1:,} records, {time.time() - started:.0f}s", flush=True)
assert n + 1 == N
print("leaves per log type:", dict(zip(["access", "api", "auth", "error", "security"], counts)), flush=True)
np.savez(out, types=types, leaf=leaf, counts=np.array(counts))
print("saved", out, flush=True)
