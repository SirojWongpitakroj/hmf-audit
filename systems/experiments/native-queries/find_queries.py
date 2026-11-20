"""Five native queries per (scheme, shape, q) on one region of the 10M logs.

Positions are region-local ordinals; segment = ordinal // 8192 (one shard per
region, so the shard is the whole region and its segments are the 8,192-leaf
trees). Every query returns EXACTLY q logs.

clustered: all q results lie in one segment (preferred) or in two adjacent ones (q = 4096: up to
           four adjacent ones: 4,096 records of a tenant that owns a fifth of the region need ~2.6 segments).
           Segments belong to a shard: with SHARDS shards per region (argv[4]) a region is split into SHARDS
           contiguous shards of near-equal size, each with its own 8,192-leaf segments, and segments of
           different shards are never adjacent (exactly the layout of benchmarks/latency/fixture.go).
scattered: the q results are spread over as much of the region as possible, ranked
           by span/region x distinct segments/ideal (ideal = min(q, segments)).
q = 1 is a single log: "clustered" picks logs from the first segments, "scattered"
picks logs near 10%, 30%, 50%, 70% and 90% of the region.

HMF-Audit query: (tenant, service, log type, region, [t_start, t_end]); the ALL key order is
  tenant, service, log type, region, event time, log ID, so all five attributes are required.
  A time range returns every log of that (tenant, service, log type, region) group in the
  range, so a window may not split logs that share an event_time.
  Where no single (tenant, service, log type, region) group has a run of exactly q logs
  in the shape (HMF-Audit clustered q >= 64, scattered q = 4096), the query is a batch of
  several such ranges ("ranges": one per group, each with its own tenant and time range)
  whose results together are exactly q logs and satisfy the shape. HMF-Audit resolves and
  verifies every range, joins the addresses, and proves them with one HPP proof.
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
SHARDS = int(sys.argv[4]) if len(sys.argv) > 4 else 1   # HMF-Audit shards per region
CLUSTER_SEGMENTS = {4096: 4}   # clustered = at most this many adjacent segments (default 2)
QS = (1, 16, 64, 256, 1024, 4096)
K = 5
MAX_RANGES = 256  # most ALL ranges one HMF-Audit batch may use
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


def plain_seg(o):
    return o // LEAVES


def spread(o, q, size, nseg, seg=plain_seg):
    """How much of the region the q results cover: span / region x distinct segments / ideal."""
    d = len(np.unique(seg(o)))
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


def stats(o, size, hmf=False):
    seg = hmf_seg(o) if hmf else plain_seg(o)
    out = {"segments_touched": int(len(np.unique(seg))), "first_segment": int(seg.min()), "last_segment": int(seg.max()),
           "first_ordinal": int(o.min()), "last_ordinal": int(o.max()),
           "span_fraction_of_region": round(float(o.max() - o.min()) / size, 4)}
    if hmf:
        out["shards_touched"] = int(len(np.unique(shard_of(o))))
    return out


def with_score(e, o, q, hmf=False):
    if hmf:
        e["spread_score"] = round(spread(o, q, size, nseg, hmf_seg)[0], 4)
    else:
        e["spread_score"] = round(spread(o, q, size, nseg_blas)[0], 4)
    return e


starts = np.flatnonzero(np.r_[True, np.diff(region) != 0])
ends = np.r_[starts[1:], len(region)]
rid = region_names.index(wanted_region)
lo_g, hi_g = next((int(s), int(e)) for s, e in zip(starts, ends) if int(region[s]) == rid)
size = hi_g - lo_g
nseg_blas = (size + LEAVES - 1) // LEAVES   # BLAS's statistics keep the one-shard segment numbering
shard_base, shard_extra = divmod(size, SHARDS)
shard_off = np.array([k * shard_base + min(k, shard_extra) for k in range(SHARDS + 1)], dtype=np.int64)
shard_nseg = (np.diff(shard_off) + LEAVES - 1) // LEAVES
seg_start = np.r_[0, np.cumsum(shard_nseg)[:-1]].astype(np.int64)
nseg = int(shard_nseg.sum())                # HMF-Audit segments of the region, all shards


def shard_of(o):
    return np.searchsorted(shard_off[1:], o, side="right")


def hmf_seg(o):
    """Flat segment number of region-local ordinals (shard by shard)."""
    k = shard_of(o)
    return seg_start[k] + (o - shard_off[k]) // LEAVES


def hmf_gid(o):
    """Like hmf_seg, with a gap between shards: ids differ by 1 only for adjacent segments of one shard."""
    return hmf_seg(o) + shard_of(o)


max_gid = int(hmf_gid(np.array([size - 1]))[0])
t0 = time.time()
queries = []

# ============================================================== HMF-Audit
t_all = etime[lo_g:hi_g]
svc, typ, ten = (a[lo_g:hi_g].astype(np.int64) for a in (service, logtype, tenant))
idx = np.arange(size, dtype=np.int64)
families = {"tenant": (svc * 64 + typ) * 4096 + ten}
cl = {q: [] for q in QS}
sc = {q: [] for q in QS}
for family, key in families.items():
    rank = 0
    order = np.lexsort((idx, t_all, key))
    sk = key[order]
    bounds = np.r_[0, np.flatnonzero(np.diff(sk)) + 1, size]
    for a, b in zip(bounds[:-1], bounds[1:]):
        n = int(b - a)
        o = order[a:b]
        t = t_all[o]
        gid = int(sk[a])
        sname, tname, tn = service_names[gid // (64 * 4096)], type_names[(gid // 4096) % 64], tenant_names[gid % 4096]
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
            seg_diff = hmf_gid(hi_o) - hmf_gid(lo_o)
            good = np.flatnonzero(ok & (seg_diff <= CLUSTER_SEGMENTS.get(q, 2) - 1))
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
            scored = sorted(((*spread(o[i: i + q], q, size, nseg, hmf_seg), int(i)) for i in top), reverse=True)
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
         "service": c["service"], "log_type": c["log_type"], "tenant": c["tenant"],
         "time_range": [iso(c["start"]), iso(c["end"])], "group_size": c["group_size"]}
    e.update(stats(c["o"], size, hmf=True))
    if "target" in c:
        e["target_position"] = c["target"]
    e["record_ordinals"] = sorted(int(x) + lo_g for x in c["o"])
    return with_score(e, c["o"], q, hmf=True)


# ---- HMF-Audit batches of several ranges (cells with no single-range query) ----------
pos_o = order                                   # region-local ordinal, in (group, time) order
pos_t = t_all[order]
group_of = np.repeat(np.arange(len(bounds) - 1), np.diff(bounds))
group_lo, group_hi = bounds[:-1], bounds[1:]
pos_seg = hmf_gid(pos_o)


def component(i, j):
    """The window of positions [i, j) of one group, as one ALL range."""
    gid = int(sk[i])
    return {"service": service_names[gid // (64 * 4096)], "log_type": type_names[(gid // 4096) % 64],
            "tenant": tenant_names[gid % 4096], "start": int(pos_t[i]), "end": int(pos_t[j - 1]),
            "n": j - i, "o": pos_o[i:j].copy()}


def clustered_batch(segments, q):
    """q logs, all in `segments`, as ranges of whole runs of group members in time order.
    A run may not start or end between logs that share an event time."""
    inside = np.isin(pos_seg, segments)
    first = inside & ~(np.r_[False, inside[:-1]] & np.r_[False, group_of[1:] == group_of[:-1]])
    last = inside & ~(np.r_[inside[1:], False] & np.r_[group_of[1:] == group_of[:-1], False])
    runs = []
    for a, b in zip(np.flatnonzero(first), np.flatnonzero(last) + 1):
        g = group_of[a]
        lo, hi = int(group_lo[g]), int(group_hi[g])
        while a < b and a > lo and pos_t[a - 1] == pos_t[a]:
            a += 1
        while a < b and b < hi and pos_t[b] == pos_t[b - 1]:
            b -= 1
        if a < b:
            runs.append((int(b - a), int(a), int(b), lo, hi))
    if sum(r[0] for r in runs) < q:
        return None
    runs.sort(key=lambda r: (-r[0], r[1]))
    chosen, total, rest = [], 0, []
    for run in runs:
        if total + run[0] <= q:
            chosen.append(component(run[1], run[2]))
            total += run[0]
        else:
            rest.append(run)
        if total == q or len(chosen) > MAX_RANGES:
            break
    if total < q:
        need = q - total                       # one more window of exactly `need` logs
        for length, a, b, lo, hi in (r for r in rest if r[0] >= need):
            i = np.arange(a, b - need + 1)
            j = i + need
            ok = ((i == lo) | (pos_t[i] != pos_t[i - 1])) & ((j == hi) | (pos_t[j - 1] != pos_t[np.minimum(j, size - 1)]))
            hit = np.flatnonzero(ok)
            if len(hit):
                chosen.append(component(int(i[hit[0]]), int(i[hit[0]]) + need))
                total = q
                break
    if total != q or len(chosen) > MAX_RANGES:
        return None
    return chosen


def multi_clustered(q, count):
    """`count` batches of q logs, each inside one segment or two adjacent ones; fewest ranges first."""
    options = []
    width = CLUSTER_SEGMENTS.get(q, 2)
    for segment in range(max_gid + 1):
        for segments in (tuple(range(segment, segment + w)) for w in (1, width)):
            if segments[-1] > max_gid:
                continue
            batch = clustered_batch(segments, q)
            if batch is not None:
                touched = len(np.unique(hmf_gid(np.concatenate([c["o"] for c in batch]))))
                options.append(((touched, len(batch), segment), segments, batch))
    options.sort(key=lambda x: x[0])
    chosen, used = [], set()
    for _, segments, batch in options:
        if used.isdisjoint(segments):
            chosen.append(batch)
            used.update(segments)
            if len(chosen) == count:
                break
    return chosen


def multi_scattered(q, count):
    """`count` batches of q logs spread over the region: ranges of the best scattered
    windows of a smaller size, from different groups, chosen to maximise the spread."""
    part = max((b for b in QS if b < q and len(sc[b]) >= q // b), default=None)
    if part is None or q % part:
        return []
    need = q // part
    pool = sorted(sc[part], key=lambda c: c["key"])
    chosen, used = [], set()
    for _ in range(count):
        batch, groups = [], set()
        for _ in range(need):
            best = None
            for c in (c for c in pool[:200] if c["identity"] not in used and c["identity"] not in groups):
                union = np.concatenate([x["o"] for x in batch] + [c["o"]])
                if len(np.unique(union)) != len(union):
                    continue
                score = spread(union, len(union), size, nseg, hmf_seg)[0]
                if best is None or score > best[0]:
                    best = (score, c)
            if best is None:
                break
            batch.append(best[1])
            groups.add(best[1]["identity"])
        if len(batch) < need:
            break
        used |= groups
        chosen.append([{"service": c["service"], "log_type": c["log_type"], "tenant": c["tenant"],
                        "start": c["start"], "end": c["end"], "n": part, "o": c["o"]} for c in batch])
    return chosen


def hmf_batch_entry(shape, q, number, batch):
    o = np.concatenate([c["o"] for c in batch])
    assert len(o) == q and len(np.unique(o)) == q
    e = {"scheme": "HMF-Audit", "region": wanted_region, "shape": shape, "q": q, "set": number, "returns": q,
         "range_count": len(batch),
         "ranges": [{"tenant": c["tenant"], "service": c["service"], "log_type": c["log_type"],
                     "time_range": [iso(c["start"]), iso(c["end"])], "returns": int(c["n"])} for c in batch]}
    e.update(stats(o, size, hmf=True))
    e["record_ordinals"] = sorted(int(x) + lo_g for x in o)
    return with_score(e, o, q, hmf=True)



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
        if not chosen:                                       # no single group has q logs in this shape
            batches = (multi_clustered if shape == "clustered" else multi_scattered)(q, K)
            for number, batch in enumerate(batches, 1):
                queries.append(hmf_batch_entry(shape, q, number, batch))
            print(f"HMF-Audit {shape} q={q}: {len(batches)} multi-range batches, "
                  f"ranges per batch {[len(b) for b in batches]}", flush=True)
            chosen = batches
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
        score, distinct, i = max((*spread(h[j: j + q], q, size, nseg_blas), int(j)) for j in top)
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
    json.dump({"region": wanted_region, "records": size, "segments": nseg, "shards_per_region": SHARDS, "queries": queries}, f, indent=1)
print("wrote", out_path, f"{time.time() - t0:.0f}s", flush=True)
