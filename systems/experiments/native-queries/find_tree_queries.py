"""Clustered and scattered audit batches for DHT-backed and EVSEB on the 10M logs.

A batch is a set of q record IDs (record ID = global ordinal, 12-digit zero-padded;
EVSEB's LID is the same string). Five batches per scheme, shape and q.

DHT-backed (five MK-trees, one per log type; capacity 10M -> 24 levels; stride 8, so
256 level-8 ancestors of 65,536 leaf slots each; micro-root windows of 1,024 leaves):
  clustered : q consecutive leaves of ONE log type, aligned to q, so they form one
              perfect subtree inside one ancestor and share every reusable proof record
              above their lowest common node.
  scattered : q records in as many different (log type, ancestor) pairs as possible
              (all populated ancestors once q reaches their number), spread over the
              ancestor's leaf range, so they share almost nothing.
EVSEB (one quaternary Merkle tree over all 10M leaves in dataset order, 13 levels):
  clustered : q consecutive leaves aligned to q (a power of 4): one perfect subtree.
  scattered : q leaves spaced N/q apart: every leaf in a different branch.
q = 1 is one record: clustered = five consecutive records, scattered = the records
nearest 10/30/50/70/90% of the tree.
"""
import json
import sys

import numpy as np

N = 10_000_000
QS = (1, 16, 64, 256, 1024, 4096)
FRACTIONS = (0.1, 0.3, 0.5, 0.7, 0.9)
PHASES = (0.1, 0.3, 0.5, 0.7, 0.9)   # stratum-centred: never on a branch boundary
LEVELS, STRIDE, WINDOW = 24, 8, 1024
ANCESTOR_SPAN = 1 << (LEVELS - STRIDE)          # 65,536 leaf slots per ancestor
TYPES = ["access", "api", "auth", "error", "security"]

d = np.load(sys.argv[1])
types, leaf, counts = d["types"], d["leaf"], [int(c) for c in d["counts"]]
out_path = sys.argv[2]
ord_of = [np.flatnonzero(types == t) for t in range(5)]   # leaf index -> global ordinal
assert all(len(ord_of[t]) == counts[t] for t in range(5))


# -------------------------------------------------------------- DHT-backed
def dht_metrics(ordinals):
    o = np.asarray(ordinals, np.int64)
    t = types[o].astype(np.int64)
    lf = leaf[o].astype(np.int64)
    keys = []
    for j in range(LEVELS, STRIDE, -1):                       # P_j,(idx^1) for j = 24..9
        keys.append(((t * 64 + j) << 40) | ((lf >> (LEVELS - j)) ^ 1))
    anc = lf >> (LEVELS - STRIDE)
    keys.append(((t * 64 + 63) << 40) | anc)                  # one certificate per ancestor
    distinct = len(np.unique(np.concatenate(keys))) + 5       # + the root record of every log type
    # lowest level at which every record is under one node, per tree touched
    spans = []
    for tt in np.unique(t):
        x = lf[t == tt]
        lvl = 0
        while len(np.unique(x >> lvl)) > 1:
            lvl += 1
        spans.append(lvl)
    return {"trees_touched": int(len(np.unique(t))), "ancestors": int(len(np.unique(t * 4096 + anc))),
            "micro_root_windows": int(len(np.unique(t * (1 << 20) + (lf // WINDOW)))),
            "proof_lookups": int(distinct), "lowest_common_node_height": int(max(spans))}


def dht_clustered(q, i):
    """Five placements: log type i, an aligned block at fraction i of that tree's leaves."""
    t = i % 5
    if q == 1:                                                # query i = record i of five consecutive leaves of one subtree
        s = (int(0.5 * counts[1]) // 8) * 8
        return [int(ord_of[1][s + i])]
    blocks = counts[t] // q
    s = min(int(FRACTIONS[i] * blocks), blocks - 1) * q
    return [int(x) for x in ord_of[t][s: s + q]]


# populated (type, ancestor) pairs, interleaved across the five trees so that the
# first q of them already span as many trees as possible
anc_list = []
per_type = [list(range((counts[t] + ANCESTOR_SPAN - 1) // ANCESTOR_SPAN)) for t in range(5)]
for k in range(max(len(p) for p in per_type)):
    for t in range(5):
        if k < len(per_type[t]):
            anc_list.append((t, per_type[t][k]))


def dht_scattered(q, i):
    phase = PHASES[i]
    if q == 1:
        t = i % 5
        a = min(int(FRACTIONS[i] * len(per_type[t])), len(per_type[t]) - 1)
        lo = a * ANCESTOR_SPAN
        hi = min(lo + ANCESTOR_SPAN, counts[t])
        return [int(ord_of[t][lo + int(phase * (hi - lo))])]
    A = len(anc_list)
    chosen = anc_list if q >= A else [anc_list[int((m + phase) * A / q) % A] for m in range(q)]
    base, extra = divmod(q, len(chosen)) if q >= A else (1, 0)
    ords = []
    for n, (t, a) in enumerate(chosen):
        k = base + (1 if n < extra else 0)
        lo = a * ANCESTOR_SPAN
        hi = min(lo + ANCESTOR_SPAN, counts[t])
        for m in range(k):
            ords.append(int(ord_of[t][lo + min(int((m + phase) * (hi - lo) / k), hi - lo - 1)]))
    assert len(set(ords)) == q, (q, len(set(ords)))
    return ords


# ------------------------------------------------------------------- EVSEB
WIDTHS = [N]
while WIDTHS[-1] > 1:
    WIDTHS.append((WIDTHS[-1] + 3) // 4)
TOP = len(WIDTHS) - 1


def evseb_metrics(ordinals):
    o = np.unique(np.asarray(ordinals, np.int64))
    nodes = 1                                                  # the root
    for level in range(TOP):
        parents = np.unique(o >> (2 * (level + 1)))
        children = np.minimum(4, WIDTHS[level] - 4 * parents)  # every child of every parent on a path
        nodes += int(children.sum())
    nodes -= len(o)                                            # candidate leaves come from the DHT, not PostgreSQL
    lvl = 0
    while len(np.unique(o >> (2 * lvl))) > 1:
        lvl += 1
    return {"stored_tree_nodes_loaded": nodes, "dht_lookups": int(len(o)), "lowest_common_node_height": lvl,
            "distinct_parents_at_level_1": int(len(np.unique(o >> 2)))}


def evseb_clustered(q, i):
    if q == 1:                                                # query i = record i of five consecutive leaves
        return [4_000_000 + i]
    blocks = N // q
    s = min(int(FRACTIONS[i] * blocks), blocks - 1) * q
    return list(range(s, s + q))


def evseb_scattered(q, i):
    if q == 1:
        return [int(FRACTIONS[i] * (N - 1))]
    return [int((m + PHASES[i]) * N / q) for m in range(q)]


queries = []
for scheme, clustered, scattered, metrics in (("DHT-backed", dht_clustered, dht_scattered, dht_metrics),
                                              ("EVSEB", evseb_clustered, evseb_scattered, evseb_metrics)):
    for shape, make in (("clustered", clustered), ("scattered", scattered)):
        for q in QS:
            for i in range(5):
                ords = make(q, i)
                assert len(ords) == q, (scheme, shape, q, len(ords))
                entry = {"scheme": scheme, "shape": shape, "q": q, "set": i + 1, "returns": len(ords),
                         "first_ordinal": min(ords), "last_ordinal": max(ords)}
                if scheme == "DHT-backed":
                    entry["log_types"] = [TYPES[t] for t in sorted(set(int(types[x]) for x in ords))]
                entry.update(metrics(ords))
                entry["record_ordinals"] = ords
                queries.append(entry)
    print(scheme, "done", flush=True)

json.dump({"records": N, "dht": {"levels": LEVELS, "stride": STRIDE, "window": WINDOW, "ancestor_leaf_slots": ANCESTOR_SPAN,
                                   "leaves_per_log_type": dict(zip(TYPES, counts)), "populated_ancestors": len(anc_list)},
           "evseb": {"levels": TOP + 1, "arity": 4}, "queries": queries}, open(out_path, "w"))
print("wrote", out_path, flush=True)
