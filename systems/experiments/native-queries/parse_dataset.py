"""Pass 1: stream the 10M file once into compact arrays for the native-query search.

Per record: region, service, log type, tenant, event time (epoch seconds), and for
BLAS a bitmask of keyword hits. BLAS keywords are the 16 most frequent tokens by
document frequency (exactly as the fair fixture builds its vocabulary), plus
"other" for records that contain none of them. The true top-16 is only known after
the whole file has been read, so 64 candidates (the top 64 of the first 300,000
records) are tracked while reading, and the result is checked against them.
"""
import datetime
import json
import re
import sys
import time
from collections import Counter

import numpy as np

path, out = sys.argv[1], sys.argv[2]
N = 10_000_000
TOKEN = re.compile(r"[^\W\d_]+")
PREFIX = 300_000
CANDIDATES = 64


def tokens(raw):
    return {t for t in TOKEN.findall(raw.lower()) if 3 <= len(t) <= 24}


region = np.zeros(N, np.uint8)
service = np.zeros(N, np.uint8)
logtype = np.zeros(N, np.uint8)
tenant = np.zeros(N, np.uint16)
etime = np.zeros(N, np.int64)
cand_mask = np.zeros(N, np.uint64)

names = {"region": {}, "service": {}, "type": {}, "tenant": {}}


def ident(table, value):
    ids = names[table]
    if value not in ids:
        ids[value] = len(ids)
    return ids[value]


days = {}
df = Counter()
cand_index = {}
ts_ok = 0
started = time.time()
n = 0
with open(path, encoding="utf-8") as source:
    for line in source:
        r = json.loads(line)
        region[n] = ident("region", r["region_id"])
        service[n] = ident("service", r["service_id"])
        logtype[n] = ident("type", r["log_type"])
        tenant[n] = ident("tenant", r["tenant_id"])
        e = r["event_time"]  # 2015-09-01T18:14:40Z
        d = days.get(e[:10])
        if d is None:
            d = days[e[:10]] = datetime.date(int(e[0:4]), int(e[5:7]), int(e[8:10])).toordinal() * 86400
        etime[n] = d + int(e[11:13]) * 3600 + int(e[14:16]) * 60 + int(e[17:19])
        raw = r["raw_log"]
        toks = tokens(raw)
        df.update(toks)
        if n == PREFIX:
            cand_index = {t: i for i, (t, _) in enumerate(df.most_common(CANDIDATES))}
        if cand_index:
            m = 0
            for t in toks:
                i = cand_index.get(t)
                if i is not None:
                    m |= 1 << i
            cand_mask[n] = m
        f = raw.split()
        if len(f) >= 3 and f[1][:4].isdigit() and f[1][4:5] == "-":
            ts_ok += 1  # a BLAS timestamp would be parsed from the log text
        n += 1
        if n % 500_000 == 0:
            print(f"{n:,} records, {time.time() - started:.0f}s, ts_ok={ts_ok}", flush=True)

assert n == N, n
vocab = [t for t, _ in sorted(df.items(), key=lambda kv: (-kv[1], kv[0]))[:16]]
print("BLAS vocabulary (top 16 by document frequency):", vocab, flush=True)
print("document frequencies:", [df[t] for t in vocab], flush=True)
print("records whose log text carries a parseable timestamp:", ts_ok, flush=True)
missing = [t for t in vocab if t not in cand_index]

mask = np.zeros(N, np.uint32)
position_of = {t: p for p, t in enumerate(vocab)}
if missing:
    # Some vocabulary token was not among the candidates: recompute every mask.
    print("vocabulary tokens outside the candidates:", missing, "-> second full pass", flush=True)
    with open(path, encoding="utf-8") as source:
        for k, line in enumerate(source):
            m = 0
            for t in tokens(json.loads(line)["raw_log"]):
                p = position_of.get(t)
                if p is not None:
                    m |= 1 << p
            mask[k] = m
            if (k + 1) % 1_000_000 == 0:
                print(f"pass 2: {k + 1:,} records, {time.time() - started:.0f}s", flush=True)
else:
    # The first PREFIX records were read before the candidates existed: redo them.
    cand_mask[: PREFIX + 1] = 0
    with open(path, encoding="utf-8") as source:
        for k, line in enumerate(source):
            if k > PREFIX:
                break
            for t in tokens(json.loads(line)["raw_log"]):
                i = cand_index.get(t)
                if i is not None:
                    cand_mask[k] |= np.uint64(1 << i)
    for t in vocab:
        mask |= (((cand_mask >> np.uint64(cand_index[t])) & np.uint64(1)).astype(np.uint32)) << np.uint32(position_of[t])
mask |= (mask == 0).astype(np.uint32) << np.uint32(16)  # bit 16 = "other"
keywords = vocab + ["other"]

np.savez_compressed(
    out, region=region, service=service, logtype=logtype, tenant=tenant, etime=etime, mask=mask,
    keywords=np.array(keywords), ts_ok=np.array(ts_ok),
    region_names=np.array(sorted(names["region"], key=names["region"].get)),
    service_names=np.array(sorted(names["service"], key=names["service"].get)),
    type_names=np.array(sorted(names["type"], key=names["type"].get)),
    tenant_names=np.array(sorted(names["tenant"], key=names["tenant"].get)),
)
print(f"saved {out} after {time.time() - started:.0f}s", flush=True)
