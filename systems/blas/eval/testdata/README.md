# android_v1_sample.log — provenance

**Source:** LogHub `Android_v1` dataset, as listed in `logpai/loghub`'s README
(https://github.com/logpai/loghub), downloaded from
`https://zenodo.org/records/8196385/files/Android_v1.zip?download=1` on 2026-09-12.

**Extraction:** this file is the first 20,000 lines of the real, unmodified
`Android.log` file inside that archive (`head -n 20000 Android.log`). No
records were duplicated, synthesized, or altered — every line here is a
genuine record from the official dataset. The full dataset is 1,555,005
lines (~192 MB uncompressed); this repository carries a bounded prefix for
test-fixture size and CI runtime reasons, not because the full dataset was
inaccessible (it is: see `TRACKER.md` M12 for the reachability check).

**License:** the LogHub datasets are "freely available for research or
academic work" per the loghub README. This project uses them for exactly
that (SRS reproduction), and cites the source per loghub's own citation
request:

> Jieming Zhu, Shilin He, Pinjia He, Jinyang Liu, Michael R. Lyu. *Loghub: A
> Large Collection of System Log Datasets for AI-driven Log Analytics*. IEEE
> International Symposium on Software Reliability Engineering (ISSRE), 2023.

**Format** (one line per record, no header row):

```
MM-DD HH:MM:SS.mmm  PID  TID LEVEL Component: message
```

The year is not present in the raw log (LogHub's own format), matching
`REQUIREMENTS.md`'s GAP-47 / `DECISIONS.md` H4 ("fill an absent year
deterministically"): `eval/realdata.go`'s parser assigns a single fixed
reference year to every record unless a month rolls backward (12→01)
partway through the file, in which case the year increments — this is the
harness's own documented deterministic filling rule, not a claim about the
dataset's true collection year.
