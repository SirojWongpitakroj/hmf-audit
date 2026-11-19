"""Five native queries per (scheme, shape, q) on one region of the 10M logs.

Positions are region-local ordinals; segment = ordinal // 8192 (one shard per
region, so the shard is the whole region and its segments are the 8,192-leaf
trees). Every query returns EXACTLY q logs.

clustered: all q results lie in one segment (preferred) or in two adjacent ones.
scattered: the q results are spread over as much of the region as possible, ranked
           by span/region x distinct segments/ideal (ideal = min(q, segments)).
q = 1 is a single log: "clustered" picks logs from the first segments, "scattered"
picks logs near 10%, 30%, 50%, 70% and 90% of the region.

HMF-Audit query: (service, log type, region, [t_start, t_end], optional tenant).
  A time range returns every log of that group in the range, so a window may not
  split logs that share an event_time.
BLAS query: time range + keyword expression over its 16-token vocabulary (+"other").
  Fixture timestamps are 2017-01-01 + global ordinal ms, so a range is an ordinal range.
"""
import datetime
import itertools
import json
import sys
import time

import numpy as np

LEAVES = 8192
QS = (1, 16, 64, 256, 1024, 4096)
K = 5
TARGETS = (0.1, 0.3, 0.5, 0.7, 0.9)
arrays = np.load(sys.argv[1])
out_path, wanted_region = sys.argv[2], sys.argv[3]
region = arrays["region"]
service, logtype, tenant, etime, mask = (arrays[k] for k in ("service", "logtype", "tenant", "etime", "mask"))
keywords = [str(k) for k in arrays["keywords"]]
region_names = [str(x) for x in arrays["region_names"]]
service_names = [str(x) for x in arrays["service_names"]]
type_names = [str(x) for x in arrays["type_names"]]
tenant_names = [str(x) for x in arrays["tenant_names"]]
assert int(arrays["ts_ok"]) == 0
BASE = datetime.datetime(2017, 1, 1, tzinfo=datetime.timezone.utc)
BASE_MS = int(BASE.timestamp() * 1000)


def iso(e):
    day, sec = divmod(int(e), 86400)
    return f"{datetime.date.fromordinal(day)}T{sec // 3600:02d}:{(sec // 60) % 60:02d}:{sec % 60:02d}Z"


def iso_ms(ms):
    d = BASE + datetime.timedelta(milliseconds=ms - BASE_MS)
    return d.strftime("%Y-%m-%dT%H:%M:%S.") + f"{d.microsecond // 1000:03d}Z"


def sliding_minmax(a, q):
    if q == 1:
        return a.copy(), a.copy()
    n = len(a)
    pad = (-n) % q
    lo_in = np.concatenate([a, np.full(pad, a.max())]).reshape(-1, q)
    hi_in = np.concatenate([a, np.full(pad, a.min())]).reshape(-1, q)
    pre_lo = np.minimum.accumulate(lo_in, axis=1).ravel()
    suf_lo = np.minimum.accumulate(lo_in[:, ::-1], axis=1)[:, ::-1].ravel()
    pre_hi = np.maximum.accumulate(hi_in, axis=1).ravel()
    suf_hi = np.maximum.accumulate(hi_in[:, ::-1], axis=1)[:, ::-1].ravel()
    m = n - q + 1
    return np.minimum(suf_lo[:m], pre_lo[q - 1:q - 1 + m]), np.maximum(suf_hi[:m], pre_hi[q - 1:q - 1 + m])


def spread(o, q, size, nseg):
    """How much of the region the q results cover: span / region x distinct segments / ideal."""
    d = len(np.unique(o // LEAVES))
    return (float(o.max() - o.min()) / size) * (d / min(q, nseg)), d


def pick(cands, k=K):
    """Best k by key: first one window per distinct query, then more windows of the
    same queries if there are too few, never two overlapping windows of one query."""
    cands = sorted(cands, key=lambda c: c["key"])
    chosen, seen = [], set()
    for c in cands:
        if c["identity"] not in seen:
            chosen.append(c)
            seen.add(c["identity"])
            if len(chosen) == k:
                return chosen
    for c in cands:
        if c in chosen:
            continue
        if all(not (c["identity"] == d["identity"] and c["lo"] <= d["hi"] and d["lo"] <= c["hi"]) for d in chosen):
            chosen.append(c)
            if len(chosen) == k:
                break
    return chosen


def stats(o, size):
    seg = o // LEAVES
    return {"segments_touched": int(len(np.unique(seg))), "first_segment": int(seg.min()), "last_segment": int(seg.max()),
            "first_ordinal": int(o.min()), "last_ordinal": int(o.max()),
            "span_fraction_of_region": round(float(o.max() - o.min()) / size, 4)}


def with_score(e, o, q):
    e["spread_score"] = round(spread(o, q, size, nseg)[0], 4)
    return e


starts = np.flatnonzero(np.r_[True, np.diff(region) != 0])
ends = np.r_[starts[1:], len(region)]
rid = region_names.index(wanted_region)
lo_g, hi_g = next((int(s), int(e)) for s, e in zip(starts, ends) if int(region[s]) == rid)
size = hi_g - lo_g
nseg = (size + LEAVES - 1) // LEAVES
t0 = time.time()
queries = []

# ============================================================== HMF-Audit
t_all = etime[lo_g:hi_g]
svc, typ, ten = (a[lo_g:hi_g].astype(np.int64) for a in (service, logtype, tenant))
idx = np.arange(size, dtype=np.int64)
families = {"no tenant": svc * 64 + typ, "with tenant": (svc * 64 + typ) * 4096 + ten}
cl = {q: [] for q in QS}
sc = {q: [] for q in QS}
for family, key in families.items():
    rank = 0 if family == "no tenant" else 1
    order = np.lexsort((idx, t_all, key))
    sk = key[order]
    bounds = np.r_[0, np.flatnonzero(np.diff(sk)) + 1, size]
    for a, b in zip(bounds[:-1], bounds[1:]):
        n = int(b - a)
        o = order[a:b]
        t = t_all[o]
        gid = int(sk[a])
        if family == "with tenant":
            sname, tname, tn = service_names[gid // (64 * 4096)], type_names[(gid // 4096) % 64], tenant_names[gid % 4096]
        else:
            sname, tname, tn = service_names[gid // 64], type_names[gid % 64], None
        identity = (sname, tname, tn)

        def make(i, q, key_):
            return {"key": key_, "identity": identity, "lo": int(o[i: i + q].min()), "hi": int(o[i: i + q].max()),
                    "service": sname, "log_type": tname, "tenant": tn, "group_size": n,
                    "start": int(t[i]), "end": int(t[i + q - 1]), "o": o[i: i + q].copy()}

        for q in QS:
            if n < q:
                break
            m = n - q + 1
            ok = np.ones(m, bool)
            ok[1:] &= t[1:m] != t[: m - 1]
            ok[: m - 1] &= t[q: q + m - 1] != t[q - 1: q - 1 + m - 1]
            if not ok.any():
                continue
            if q == 1:
                cand = np.flatnonzero(ok)
                i = int(cand[np.argmin(o[cand])])           # clustered: earliest record of the group
                cl[1].append(make(i, 1, (0, rank, int(o[i]))))
                for tgt in TARGETS:                          # scattered: nearest record to each target position
                    j = int(cand[np.argmin(np.abs(o[cand] - tgt * size))])
                    entry = make(j, 1, (round(abs(float(o[j]) - tgt * size) / size, 4), rank, tgt))
                    entry["target"] = tgt
                    sc[1].append(entry)
                continue
            lo_o, hi_o = sliding_minmax(o, q)
            span = hi_o - lo_o
            seg_diff = hi_o // LEAVES - lo_o // LEAVES
            good = np.flatnonzero(ok & (seg_diff <= 1))
            if len(good):
                picked = []
                for i in good[np.lexsort((good, span[good], seg_diff[good]))][:4000]:
                    if all(abs(int(i) - j) >= q for j in picked):
                        picked.append(int(i))
                        cl[q].append(make(int(i), q, (int(seg_diff[i]), rank, int(span[i]))))
                        if len(picked) == 3:
                            break
            cand = np.flatnonzero(ok)
            top = cand[np.argsort(-span[cand], kind="stable")[:60]]
            scored = sorted(((*spread(o[i: i + q], q, size, nseg), int(i)) for i in top), reverse=True)
            picked = []
            for score, distinct, i in scored:
                if all(abs(i - j) >= q for j in picked):
                    picked.append(i)
                    sc[q].append(make(i, q, (-round(score, 4), -distinct, rank)))
                    if len(picked) == 3:
                        break
print(f"HMF-Audit {wanted_region} searched, {time.time() - t0:.0f}s", flush=True)


def hmf_entry(shape, q, c, number):
    e = {"scheme": "HMF-Audit", "region": wanted_region, "shape": shape, "q": q, "set": number, "returns": q,
         "service": c["service"], "log_type": c["log_type"], "tenant": c["tenant"] or "any (no tenant filter)",
         "time_range": [iso(c["start"]), iso(c["end"])], "group_size": c["group_size"]}
    e.update(stats(c["o"], size))
    if "target" in c:
        e["target_position"] = c["target"]
    e["record_ordinals"] = sorted(int(x) + lo_g for x in c["o"])
    return with_score(e, c["o"], q)


for shape, table in (("clustered", cl), ("scattered", sc)):
    for q in QS:
        if q == 1 and shape == "scattered":
            chosen, used = [], set()
            for tgt in TARGETS:                              # one record per target, distinct queries preferred
                options = sorted((c for c in table[1] if c["target"] == tgt), key=lambda c: c["key"])
                c = next((c for c in options if c["identity"] not in used), options[0])
                used.add(c["identity"])
                chosen.append(c)
        else:
            chosen = pick(table[q])
        for number, c in enumerate(chosen, 1):
            queries.append(hmf_entry(shape, q, c, number))
        if len(chosen) < K:
            queries.append({"scheme": "HMF-Audit", "region": wanted_region, "shape": shape, "q": q,
                            "note": f"only {len(chosen)} distinct queries exist"})

# ================================================================== BLAS
exprs = []
for i, k in enumerate(keywords):
    exprs.append((k, 1 << i, "and", 1))
for i, j in itertools.combinations(range(17), 2):
    exprs.append((f"{keywords[i]} AND {keywords[j]}", (1 << i) | (1 << j), "and", 2))
    exprs.append((f"{keywords[i]} OR {keywords[j]}", (1 << i) | (1 << j), "or", 2))
for i, j, k in itertools.combinations(range(17), 3):
    exprs.append((f"{keywords[i]} AND {keywords[j]} AND {keywords[k]}", (1 << i) | (1 << j) | (1 << k), "and", 3))
exprs.append(("OR of all 17 keywords (matches every log)", (1 << 17) - 1, "or", 17))

mrg = mask[lo_g:hi_g]
bcl = {q: [] for q in QS}
bsc = {q: [] for q in QS}
for text, bits, kind, terms in exprs:
    hit = ((mrg & np.uint32(bits)) == np.uint32(bits)) if kind == "and" else ((mrg & np.uint32(bits)) != 0)
    h = np.flatnonzero(hit).astype(np.int64)
    for q in QS:
        if len(h) < q:
            break

        def make(i, q, key_):
            return {"key": key_, "identity": text, "lo": int(h[i]), "hi": int(h[i + q - 1]), "text": text,
                    "matches": int(len(h)), "o": h[i: i + q].copy()}

        if q == 1:
            bcl[1].append(make(0, 1, (0, terms, int(h[0]))))
            for tgt in TARGETS:
                j = int(np.argmin(np.abs(h - tgt * size)))
                entry = make(j, 1, (round(abs(float(h[j]) - tgt * size) / size, 4), terms, tgt))
                entry["target"] = tgt
                bsc[1].append(entry)
            continue
        span = h[q - 1:] - h[: len(h) - q + 1]
        seg_diff = h[q - 1:] // LEAVES - h[: len(h) - q + 1] // LEAVES
        good = np.flatnonzero(seg_diff <= 1)
        if len(good):
            i = int(good[np.lexsort((good, span[good], seg_diff[good]))[0]])
            bcl[q].append(make(i, q, (int(seg_diff[i]), terms, int(span[i]))))
        top = np.argsort(-span, kind="stable")[:40]
        score, distinct, i = max((*spread(h[j: j + q], q, size, nseg), int(j)) for j in top)
        bsc[q].append(make(i, q, (-round(score, 4), -distinct, terms)))
print(f"BLAS {wanted_region} searched, {time.time() - t0:.0f}s", flush=True)


def blas_terms(text):
    if text.startswith("OR of all"):
        return "ALL", list(keywords)
    if " AND " in text:
        return "AND", text.split(" AND ")
    if " OR " in text:
        return "OR", text.split(" OR ")
    return "AND", [text]


def blas_entry(shape, q, c, number):
    first, last = lo_g + c["lo"], lo_g + c["hi"]
    op, terms = blas_terms(c["text"])
    e = {"scheme": "BLAS", "region": wanted_region, "shape": shape, "q": q, "set": number, "returns": q,
         "keyword_expression": c["text"], "keyword_operator": op, "keyword_terms": terms, "time_range_ms": [BASE_MS + first, BASE_MS + last],
         "time_range": [iso_ms(BASE_MS + first), iso_ms(BASE_MS + last)],
         "matches_in_whole_region": c["matches"]}
    e.update(stats(c["o"], size))
    if "target" in c:
        e["target_position"] = c["target"]
    e["record_ordinals"] = [int(x) + lo_g for x in c["o"]]
    return with_score(e, c["o"], q)


for shape, table in (("clustered", bcl), ("scattered", bsc)):
    for q in QS:
        if q == 1 and shape == "scattered":
            chosen, used = [], set()
            for tgt in TARGETS:
                options = sorted((c for c in table[1] if c["target"] == tgt), key=lambda c: c["key"])
                c = next((c for c in options if c["identity"] not in used), options[0])
                used.add(c["identity"])
                chosen.append(c)
        else:
            chosen = pick(table[q])
        for number, c in enumerate(chosen, 1):
            queries.append(blas_entry(shape, q, c, number))
        if len(chosen) < K:
            queries.append({"scheme": "BLAS", "region": wanted_region, "shape": shape, "q": q,
                            "note": f"only {len(chosen)} distinct queries exist"})

with open(out_path, "w", encoding="utf-8") as f:
    json.dump({"region": wanted_region, "records": size, "segments": nseg, "queries": queries}, f, indent=1)
print("wrote", out_path, f"{time.time() - t0:.0f}s", flush=True)
