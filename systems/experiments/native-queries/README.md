# Clustered and scattered queries on the 10M logs

HMF-Audit and BLAS (native queries, region R0) come first; DHT-backed and EVSEB (tree locality) are at the end.

Found on the pinned 10,000,000-record subset (SHA-256 `a8fa53db…`), region **R0**: 2,500,000 logs in 306 segments of 8,192 leaves. Each query returns **exactly q logs**. Five queries for each scheme, shape and q in {1, 16, 64, 256, 1024, 4096}: 120 queries. The full machine-readable list is `R0_queries.json`.

## How to read it

* **Shard and segment.** With one shard per region, the shard is the whole region; its 8,192-leaf segments are what "adjacent" means here. **Clustered** = all q results in one segment, or in two adjacent ones. **Scattered** = the q results spread over as much of the region as possible, ranked by span of the region x distinct segments touched / ideal (ideal = min(q, 306)). "Span" is the share of the region between the first and last result.
* **HMF-Audit query** = (region, service, log type, time range `[from, to]`, optional tenant). A time range returns every log of the group in the range, so no window splits logs that share an event time. Tenant "any" means the new single all-tenant query.
* **BLAS query** = time range + keyword expression over BLAS's vocabulary: the 16 most frequent tokens (`info, task, executor, stage, tid, storage, block, bytes, update, row, rdd, python, pythonrunner, init, times, total`) plus `other` for logs with none of them. The fair fixture timestamps a log as 2017-01-01 + its global ordinal in milliseconds (no log text carries a timestamp), so a BLAS time range is an ordinal range.
* **q = 1** is one log: "clustered" takes logs from the first segments, "scattered" takes the log nearest 10%, 30%, 50%, 70% and 90% of the region.
* **Not run through the real systems.** Counts and positions were computed from the dataset with the same rules the fixtures use. The queries have not been executed against HMF-Audit or BLAS; run a few before relying on them.

## Summary (segments touched / span of region, per query)

| scheme | shape | q=1 | q=16 | q=64 | q=256 | q=1024 | q=4096 |
|---|---|---|---|---|---|---|---|
| HMF-Audit | clustered | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 2/0% | 1/0%, 1/0%, 2/0%, 1/0%, 1/0% | 1/0%, 1/0%, 2/1%, 1/0%, 1/0% |
| HMF-Audit | scattered | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 12/100%, 12/99%, 13/91%, 13/91%, 13/91% | 30/99%, 30/99%, 30/98%, 31/94%, 29/99% | 56/89%, 44/55%, 52/28%, 52/28%, 50/28% | 147/65%, 145/66%, 146/66%, 146/66%, 145/66% | 65/91%, 47/70%, 4/18%, 22/8%, 7/18% |
| BLAS | clustered | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% |
| BLAS | scattered | 1/0%, 1/0%, 1/0%, 1/0%, 1/0% | 11/17%, 11/17%, 9/20%, 9/20%, 10/17% | 13/26%, 14/17%, 14/17%, 14/17%, 14/17% | 11/34%, 11/34%, 11/34%, 16/18%, 16/18% | 20/50%, 20/50%, 20/50%, 32/27%, 32/27% | 60/88%, 61/73%, 61/73%, 35/27%, 35/27% |

**Clustered:** every query sits in one segment, except one query each at HMF-Audit q = 256, 1024 and 4096, which uses two adjacent segments.

**Scattered, whole region:** HMF-Audit reaches 91–100% of the region at q = 16 and 94–99% at q = 64; at q = 256 only one query reaches 89%, at q = 1024 the best is 66%, and at q = 4096 one reaches 91%. BLAS reaches at most 88% (one query at q = 4096); at q <= 1024 its best is 50%. The reason is the data: a time range returns a whole run of one group, so exactly q results can only spread widely when the group has about q members spread across the region (HMF-Audit), and BLAS's rarest vocabulary expressions (for example `task AND pythonrunner`, 21 matches in R0) occur only early in the region.

## HMF-Audit

### HMF-Audit, clustered

**q = 1**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2015-09-01T18:14:40Z | 2015-09-01T18:14:40Z | 1 (segment 0) | 0.0% |
| 2 | spark-executor | UNSPECIFIED | any | 2015-09-01T18:14:44Z | 2015-09-01T18:14:44Z | 1 (segment 0) | 0.0% |
| 3 | spark-executor | WARN | any | 2016-04-07T10:45:52Z | 2016-04-07T10:45:52Z | 1 (segment 0) | 0.0% |
| 4 | spark-executor | ERROR | any | 2016-04-07T10:46:40Z | 2016-04-07T10:46:40Z | 1 (segment 0) | 0.0% |
| 5 | spark-application-master | INFO | any | 2016-04-07T10:46:17Z | 2016-04-07T10:46:17Z | 1 (segment 0) | 0.0% |

**q = 16**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2016-11-18T22:30:09Z | 2016-11-18T22:51:12Z | 1 (segment 2) | 0.0% |
| 2 | spark-executor | ERROR | any | 2017-02-01T19:15:40Z | 2017-02-01T19:15:41Z | 1 (segment 6) | 0.0% |
| 3 | spark-executor | WARN | any | 2017-02-01T22:06:49Z | 2017-02-01T22:10:55Z | 1 (segment 11) | 0.0% |
| 4 | spark-application-master | INFO | any | 2016-04-07T10:46:15Z | 2016-04-07T10:46:43Z | 1 (segment 0) | 0.0% |
| 5 | spark-application-master | ERROR | any | 2017-03-29T10:08:40Z | 2017-03-29T10:08:56Z | 1 (segment 25) | 0.0% |

**q = 64**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2016-11-18T22:49:44Z | 2016-11-18T22:53:08Z | 1 (segment 2) | 0.0% |
| 2 | spark-application-master | INFO | any | 2016-04-07T10:46:40Z | 2016-04-07T10:49:25Z | 1 (segment 0) | 0.0% |
| 3 | spark-application-master | ERROR | any | 2017-03-29T10:08:40Z | 2017-03-29T10:10:04Z | 1 (segment 25) | 0.0% |
| 4 | spark-executor | ERROR | any | 2017-03-30T00:03:20Z | 2017-03-30T00:03:40Z | 1 (segment 27) | 0.0% |
| 5 | spark-executor | WARN | any | 2017-03-30T00:20:36Z | 2017-03-30T00:22:46Z | 1 (segment 27) | 0.0% |

**q = 256**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2016-09-09T10:06:53Z | 2016-11-18T23:01:37Z | 1 (segment 2) | 0.0% |
| 2 | spark-application-master | INFO | any | 2016-04-07T10:46:10Z | 2016-04-07T10:52:24Z | 1 (segment 0) | 0.0% |
| 3 | spark-executor | ERROR | any | 2017-03-30T00:27:47Z | 2017-03-30T00:29:12Z | 1 (segment 28) | 0.0% |
| 4 | spark-application-master | WARN | any | 2017-03-30T00:01:51Z | 2017-03-30T00:41:57Z | 1 (segment 27) | 0.1% |
| 5 | spark-executor | WARN | any | 2017-03-29T23:53:25Z | 2017-03-30T00:37:46Z | 2 (segment 27–28) | 0.4% |

**q = 1024**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2016-09-09T10:06:53Z | 2016-11-18T23:20:48Z | 1 (segment 2) | 0.0% |
| 2 | spark-application-master | INFO | any | 2016-04-07T10:46:15Z | 2016-04-07T11:14:16Z | 1 (segment 0) | 0.0% |
| 3 | spark-executor | ERROR | any | 2017-03-30T00:21:41Z | 2017-03-30T00:29:13Z | 2 (segment 27–28) | 0.2% |
| 4 | spark-executor | INFO | any | 2016-11-18T23:20:50Z | 2016-11-18T23:29:52Z | 1 (segment 2) | 0.0% |
| 5 | spark-executor | INFO | any | 2016-11-18T23:30:02Z | 2016-11-18T23:41:42Z | 1 (segment 2) | 0.0% |

**q = 4096**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2017-06-08T13:41:43Z | 2017-06-08T13:45:08Z | 1 (segment 115) | 0.2% |
| 2 | spark-application-master | INFO | any | 2016-07-26T12:00:56Z | 2016-07-26T12:14:25Z | 1 (segment 1) | 0.2% |
| 3 | spark-executor | ERROR | any | 2017-03-29T23:37:55Z | 2017-03-30T00:29:03Z | 2 (segment 27–28) | 0.5% |
| 4 | spark-executor | INFO | any | 2017-06-08T13:50:33Z | 2017-06-08T13:54:16Z | 1 (segment 116) | 0.2% |
| 5 | spark-executor | INFO | any | 2017-06-08T15:03:10Z | 2017-06-08T15:05:38Z | 1 (segment 122) | 0.2% |

### HMF-Audit, scattered

**q = 1**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | any | 2017-04-13T19:52:03Z | 2017-04-13T19:52:03Z | 1 (segment 29) | 0.0% |
| 2 | spark-executor | INFO | T780 | 2017-06-07T15:12:23Z | 2017-06-07T15:12:23Z | 1 (segment 91) | 0.0% |
| 3 | spark-executor | INFO | T453 | 2017-06-08T20:25:43Z | 2017-06-08T20:25:43Z | 1 (segment 152) | 0.0% |
| 4 | spark-executor | INFO | T473 | 2017-06-09T13:02:16Z | 2017-06-09T13:02:16Z | 1 (segment 213) | 0.0% |
| 5 | spark-executor | INFO | T57 | 2017-06-09T19:52:03Z | 2017-06-09T19:52:03Z | 1 (segment 274) | 0.0% |

**q = 16**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | UNSPECIFIED | any | 2015-09-01T18:14:44Z | 2017-07-27T21:24:56Z | 12 (segment 0–304) | 99.9% |
| 2 | spark-executor | ERROR | T69 | 2016-07-26T12:05:04Z | 2017-07-27T21:32:50Z | 12 (segment 2–304) | 99.1% |
| 3 | spark-application-master | INFO | T957 | 2017-03-29T19:41:19Z | 2017-07-27T11:52:15Z | 13 (segment 25–304) | 91.2% |
| 4 | spark-application-master | INFO | T775 | 2017-03-29T21:29:00Z | 2017-07-27T14:40:28Z | 13 (segment 26–304) | 91.0% |
| 5 | spark-application-master | INFO | T840 | 2017-03-29T19:41:46Z | 2017-06-10T15:37:27Z | 13 (segment 25–302) | 90.8% |

**q = 64**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-application-master | INFO | T612 | 2016-04-07T12:12:24Z | 2017-07-27T14:49:25Z | 30 (segment 0–304) | 99.4% |
| 2 | spark-application-master | INFO | T338 | 2016-07-26T12:10:03Z | 2017-07-27T14:45:10Z | 30 (segment 1–304) | 99.2% |
| 3 | spark-application-master | INFO | T345 | 2016-04-07T11:57:17Z | 2017-06-10T15:15:54Z | 30 (segment 0–298) | 97.6% |
| 4 | spark-application-master | INFO | T482 | 2017-03-21T10:09:52Z | 2017-07-27T21:29:31Z | 31 (segment 17–304) | 94.0% |
| 5 | spark-application-master | INFO | T649 | 2016-07-26T12:06:19Z | 2017-07-27T21:13:24Z | 29 (segment 1–304) | 99.3% |

**q = 256**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | ERROR | any | 2017-04-14T01:09:47Z | 2017-06-10T15:20:40Z | 56 (segment 29–299) | 88.7% |
| 2 | spark-executor | WARN | any | 2017-06-06T21:52:07Z | 2017-06-09T17:33:44Z | 44 (segment 84–253) | 55.3% |
| 3 | spark-executor | INFO | T953 | 2017-06-08T21:41:07Z | 2017-06-09T12:10:15Z | 52 (segment 142–228) | 28.2% |
| 4 | spark-executor | INFO | T413 | 2017-06-08T21:32:10Z | 2017-06-09T12:01:19Z | 52 (segment 142–228) | 28.1% |
| 5 | spark-executor | INFO | T116 | 2017-06-08T21:49:43Z | 2017-06-09T12:20:06Z | 50 (segment 142–229) | 28.3% |

**q = 1024**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | INFO | T965 | 2017-04-13T20:56:22Z | 2017-06-09T12:17:28Z | 147 (segment 29–228) | 65.4% |
| 2 | spark-executor | INFO | T715 | 2017-04-13T22:19:56Z | 2017-06-09T12:45:57Z | 145 (segment 29–231) | 66.2% |
| 3 | spark-executor | INFO | T951 | 2017-04-13T20:57:18Z | 2017-06-09T12:20:37Z | 146 (segment 29–229) | 65.6% |
| 4 | spark-executor | INFO | T773 | 2017-04-13T20:57:18Z | 2017-06-09T12:19:04Z | 146 (segment 29–229) | 65.5% |
| 5 | spark-executor | INFO | T397 | 2017-04-13T22:06:57Z | 2017-06-09T12:54:30Z | 145 (segment 29–229) | 65.8% |

**q = 4096**

| # | service | log type | tenant | from | to | segments | span |
|---|---|---|---|---|---|---|---|
| 1 | spark-executor | ERROR | any | 2017-03-29T23:42:16Z | 2017-07-27T15:16:09Z | 65 (segment 27–304) | 90.8% |
| 2 | spark-application-master | INFO | any | 2017-06-06T22:05:02Z | 2017-06-10T15:26:33Z | 47 (segment 86–301) | 70.5% |
| 3 | spark-executor | INFO | any | 2017-04-13T23:55:05Z | 2017-05-02T14:35:05Z | 4 (segment 29–82) | 17.5% |
| 4 | spark-executor | ERROR | any | 2015-09-01T18:19:50Z | 2017-03-29T17:26:48Z | 22 (segment 0–25) | 8.4% |
| 5 | spark-application-master | INFO | any | 2017-03-29T23:41:08Z | 2017-04-13T21:26:32Z | 7 (segment 26–80) | 17.6% |

## BLAS

### BLAS, clustered

**q = 1**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `other` | 2017-01-01T00:00:00.000Z | 2017-01-01T00:00:00.000Z | 18,631 | 1 (segment 0) | 0.0% |
| 2 | `info` | 2017-01-01T00:00:00.001Z | 2017-01-01T00:00:00.001Z | 2,060,015 | 1 (segment 0) | 0.0% |
| 3 | `executor` | 2017-01-01T00:00:00.002Z | 2017-01-01T00:00:00.002Z | 831,565 | 1 (segment 0) | 0.0% |
| 4 | `task` | 2017-01-01T00:00:00.004Z | 2017-01-01T00:00:00.004Z | 888,845 | 1 (segment 0) | 0.0% |
| 5 | `stage` | 2017-01-01T00:00:00.006Z | 2017-01-01T00:00:00.006Z | 556,185 | 1 (segment 0) | 0.0% |

**q = 16**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `info` | 2017-01-01T00:00:00.001Z | 2017-01-01T00:00:00.016Z | 2,060,015 | 1 (segment 0) | 0.0% |
| 2 | `task` | 2017-01-01T00:00:00.214Z | 2017-01-01T00:00:00.229Z | 888,845 | 1 (segment 0) | 0.0% |
| 3 | `executor` | 2017-01-01T00:00:00.214Z | 2017-01-01T00:00:00.229Z | 831,565 | 1 (segment 0) | 0.0% |
| 4 | `stage` | 2017-01-01T00:03:44.009Z | 2017-01-01T00:03:44.024Z | 556,185 | 1 (segment 27) | 0.0% |
| 5 | `tid` | 2017-01-01T00:03:44.009Z | 2017-01-01T00:03:44.024Z | 556,022 | 1 (segment 27) | 0.0% |

**q = 64**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `info` | 2017-01-01T00:00:00.038Z | 2017-01-01T00:00:00.101Z | 2,060,015 | 1 (segment 0) | 0.0% |
| 2 | `storage` | 2017-01-01T00:00:11.039Z | 2017-01-01T00:00:11.102Z | 523,125 | 1 (segment 1) | 0.0% |
| 3 | `block` | 2017-01-01T00:02:22.984Z | 2017-01-01T00:02:23.047Z | 438,232 | 1 (segment 17) | 0.0% |
| 4 | `bytes` | 2017-01-01T00:02:22.984Z | 2017-01-01T00:02:23.047Z | 421,203 | 1 (segment 17) | 0.0% |
| 5 | `update` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:03:58.471Z | 418,065 | 1 (segment 29) | 0.0% |

**q = 256**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `info` | 2017-01-01T00:00:00.319Z | 2017-01-01T00:00:00.574Z | 2,060,015 | 1 (segment 0) | 0.0% |
| 2 | `storage` | 2017-01-01T00:19:29.745Z | 2017-01-01T00:19:30.000Z | 523,125 | 1 (segment 142) | 0.0% |
| 3 | `update` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:03:58.663Z | 418,065 | 1 (segment 29) | 0.0% |
| 4 | `row` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:03:58.663Z | 418,025 | 1 (segment 29) | 0.0% |
| 5 | `rdd` | 2017-01-01T00:19:29.746Z | 2017-01-01T00:19:30.001Z | 414,984 | 1 (segment 142) | 0.0% |

**q = 1024**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `info` | 2017-01-01T00:00:01.043Z | 2017-01-01T00:00:02.066Z | 2,060,015 | 1 (segment 0) | 0.0% |
| 2 | `update` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:03:59.431Z | 418,065 | 1 (segment 29) | 0.0% |
| 3 | `row` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:03:59.431Z | 418,025 | 1 (segment 29) | 0.0% |
| 4 | `storage` | 2017-01-01T00:20:21.513Z | 2017-01-01T00:20:22.539Z | 523,125 | 1 (segment 149) | 0.0% |
| 5 | `rdd` | 2017-01-01T00:23:04.448Z | 2017-01-01T00:23:05.475Z | 414,984 | 1 (segment 169) | 0.0% |

**q = 4096**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `info` | 2017-01-01T00:00:25.869Z | 2017-01-01T00:00:29.964Z | 2,060,015 | 1 (segment 3) | 0.2% |
| 2 | `update` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:04:02.503Z | 418,065 | 1 (segment 29) | 0.2% |
| 3 | `row` | 2017-01-01T00:03:58.408Z | 2017-01-01T00:04:02.503Z | 418,025 | 1 (segment 29) | 0.2% |
| 4 | `storage` | 2017-01-01T00:29:21.280Z | 2017-01-01T00:29:26.748Z | 523,125 | 1 (segment 215) | 0.2% |
| 5 | `rdd` | 2017-01-01T00:29:21.280Z | 2017-01-01T00:29:26.943Z | 414,984 | 1 (segment 215) | 0.2% |

### BLAS, scattered

**q = 1**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `update` | 2017-01-01T00:04:10.000Z | 2017-01-01T00:04:10.000Z | 418,065 | 1 (segment 30) | 0.0% |
| 2 | `info` | 2017-01-01T00:12:30.000Z | 2017-01-01T00:12:30.000Z | 2,060,015 | 1 (segment 91) | 0.0% |
| 3 | `task` | 2017-01-01T00:20:50.001Z | 2017-01-01T00:20:50.001Z | 888,845 | 1 (segment 152) | 0.0% |
| 4 | `executor` | 2017-01-01T00:29:10.004Z | 2017-01-01T00:29:10.004Z | 831,565 | 1 (segment 213) | 0.0% |
| 5 | `stage` | 2017-01-01T00:37:30.001Z | 2017-01-01T00:37:30.001Z | 556,185 | 1 (segment 274) | 0.0% |

**q = 16**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `stage OR total` | 2017-01-01T00:03:58.011Z | 2017-01-01T00:10:58.357Z | 892,726 | 11 (segment 29–80) | 16.8% |
| 2 | `tid OR total` | 2017-01-01T00:03:58.011Z | 2017-01-01T00:10:58.357Z | 892,563 | 11 (segment 29–80) | 16.8% |
| 3 | `task AND pythonrunner` | 2017-01-01T00:02:32.145Z | 2017-01-01T00:11:03.608Z | 21 | 9 (segment 18–81) | 20.5% |
| 4 | `task AND python AND pythonrunner` | 2017-01-01T00:02:32.145Z | 2017-01-01T00:11:03.608Z | 21 | 9 (segment 18–81) | 20.5% |
| 5 | `task AND executor` | 2017-01-01T00:03:57.081Z | 2017-01-01T00:10:58.862Z | 828,254 | 10 (segment 28–80) | 16.9% |

**q = 64**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `task AND python` | 2017-01-01T00:00:12.406Z | 2017-01-01T00:11:03.608Z | 76 | 13 (segment 1–81) | 26.1% |
| 2 | `executor` | 2017-01-01T00:03:55.504Z | 2017-01-01T00:11:00.864Z | 831,565 | 14 (segment 28–80) | 17.0% |
| 3 | `executor OR rdd` | 2017-01-01T00:03:55.504Z | 2017-01-01T00:11:00.497Z | 1,246,549 | 14 (segment 28–80) | 17.0% |
| 4 | `task OR executor` | 2017-01-01T00:03:55.501Z | 2017-01-01T00:10:59.203Z | 892,156 | 14 (segment 28–80) | 17.0% |
| 5 | `executor OR stage` | 2017-01-01T00:03:55.507Z | 2017-01-01T00:10:58.951Z | 835,797 | 14 (segment 28–80) | 16.9% |

**q = 256**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `other` | 2017-01-01T00:19:29.739Z | 2017-01-01T00:33:51.941Z | 18,631 | 11 (segment 142–248) | 34.5% |
| 2 | `update OR other` | 2017-01-01T00:19:29.739Z | 2017-01-01T00:33:51.941Z | 436,696 | 11 (segment 142–248) | 34.5% |
| 3 | `row OR other` | 2017-01-01T00:19:29.739Z | 2017-01-01T00:33:51.941Z | 436,656 | 11 (segment 142–248) | 34.5% |
| 4 | `executor` | 2017-01-01T00:03:55.504Z | 2017-01-01T00:11:15.316Z | 831,565 | 16 (segment 28–82) | 17.6% |
| 5 | `executor OR rdd` | 2017-01-01T00:03:55.504Z | 2017-01-01T00:11:13.532Z | 1,246,549 | 16 (segment 28–82) | 17.5% |

**q = 1024**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `other` | 2017-01-01T00:19:29.739Z | 2017-01-01T00:40:30.974Z | 18,631 | 20 (segment 142–296) | 50.4% |
| 2 | `update OR other` | 2017-01-01T00:19:29.739Z | 2017-01-01T00:40:30.974Z | 436,696 | 20 (segment 142–296) | 50.4% |
| 3 | `row OR other` | 2017-01-01T00:19:29.739Z | 2017-01-01T00:40:30.974Z | 436,656 | 20 (segment 142–296) | 50.4% |
| 4 | `storage AND rdd` | 2017-01-01T00:00:14.617Z | 2017-01-01T00:11:21.697Z | 358,216 | 32 (segment 1–83) | 26.7% |
| 5 | `info AND storage AND rdd` | 2017-01-01T00:00:14.617Z | 2017-01-01T00:11:21.697Z | 358,216 | 32 (segment 1–83) | 26.7% |

**q = 4096**

| # | keyword expression | from | to | matches in R0 | segments | span |
|---|---|---|---|---|---|---|
| 1 | `other` | 2017-01-01T00:03:55.361Z | 2017-01-01T00:40:36.049Z | 18,631 | 60 (segment 28–297) | 88.0% |
| 2 | `update OR other` | 2017-01-01T00:11:11.010Z | 2017-01-01T00:41:28.033Z | 436,696 | 61 (segment 81–303) | 72.7% |
| 3 | `row OR other` | 2017-01-01T00:11:11.010Z | 2017-01-01T00:41:28.033Z | 436,656 | 61 (segment 81–303) | 72.7% |
| 4 | `info AND rdd` | 2017-01-01T00:00:00.730Z | 2017-01-01T00:11:27.506Z | 414,424 | 35 (segment 0–83) | 27.5% |
| 5 | `rdd` | 2017-01-01T00:00:00.012Z | 2017-01-01T00:11:24.573Z | 414,984 | 35 (segment 0–83) | 27.4% |

## Reproducing

```bash
python parse_dataset.py <10M jsonl> arrays.npz        # about 2 minutes: one pass over the file
python find_queries.py arrays.npz R0_queries.json R0  # about 3 minutes for BLAS
```
Needs Python 3.9+ and numpy. Pass another region (`R1`, `R2`, `R3`) to search it instead.

---

# DHT-backed and EVSEB (tree locality)

For these two schemes an audit batch is a set of **q record IDs** (record ID = global ordinal, zero-padded to 12 digits; EVSEB's LID is the same string). Clustered and scattered are defined by where the records sit in each scheme's Merkle tree. Five batches per scheme, shape and q in {1, 16, 64, 256, 1024, 4096}: 120 batches. All record ordinals are in `dht_evseb_queries.json`; the scripts are `map_dht_leaves.py` (about 30 s) and `find_tree_queries.py`.

| Scheme | Clustered (high locality) | Scattered (low locality) |
|---|---|---|
| DHT-backed | q consecutive leaves of one log type's MK-Tree, aligned to q: one perfect subtree inside one level-8 ancestor, sharing reusable ancestor certificates and parent nodes | q records in as many different (log type, ancestor) pairs as possible, spread over each ancestor's leaves: almost nothing shared |
| EVSEB | q consecutive leaf positions aligned to q: one perfect subtree of the quaternary tree | q leaves spaced N/q apart, each at the centre of its stratum: every leaf in a different branch |

**Tree geometry (computed from the fixtures' rules).** DHT-backed has five MK-Trees, one per log type (`access, api, auth, error, security`; a record's type is `sha256(source_id)[0] mod 5`, its leaf index is its rank within the type, about 2.0M leaves each). Capacity 10M gives 24 levels; stride 8 gives 256 ancestor slots of 65,536 leaves, of which **155 are populated** across the five trees; micro-root windows hold 1,024 leaves. EVSEB has one quaternary tree over all 10M records in dataset order: 13 levels.

**Metrics reported per batch**

* DHT-backed: trees touched, distinct level-8 ancestors, distinct 1,024-leaf windows, and **proof lookups** = the distinct DHT records the batch needs (the rule in `verifier.prefetch`: the sibling at each of levels 24..9 for every leaf, one ancestor certificate per ancestor, and the root record of each of the 5 log types).
* EVSEB: **tree nodes loaded** from PostgreSQL (every child of every parent on every path, plus the root, minus the candidate leaves, which come from the DHT; the rule in `loadFairEVBESStoredTree`), and the DHT lookups (= q).
* Both: the height of the lowest tree node that covers all q records (0 = a single record; larger = further apart).

## Result per q (values for the five batches of each cell)

| scheme | shape | q | trees | ancestors | windows | proof lookups |
|---|---|---|---|---|---|---|
| DHT-backed | clustered | 1 | 1 | 1 | 1 | 22 |
| DHT-backed | clustered | 16 | 1 | 1 | 1 | 48 |
| DHT-backed | clustered | 64 | 1 | 1 | 1 | 142 |
| DHT-backed | clustered | 256 | 1 | 1 | 1 | 524 |
| DHT-backed | clustered | 1024 | 1 | 1 | 1 | 2058 |
| DHT-backed | clustered | 4096 | 1 | 1 | 4 | 8200 |
| DHT-backed | scattered | 1 | 1 | 1 | 1 | 22 |
| DHT-backed | scattered | 16 | 5 | 16 | 16 | 277 |
| DHT-backed | scattered | 64 | 5 | 64 | 64 | 1093 |
| DHT-backed | scattered | 256 | 5 | 155 | 256 | 4256 |
| DHT-backed | scattered | 1024 | 5 | 155 | 1024 | 15402, 15403, 15404, 15404, 15405 |
| DHT-backed | scattered | 4096 | 5 | 155 | 4092, 4092, 4092, 4094, 4094 | 53837, 53837, 53837, 53845, 53846 |

| scheme | shape | q | tree nodes loaded from PostgreSQL | common-node height |
|---|---|---|---|---|
| EVSEB | clustered | 1 | 47 | 0 |
| EVSEB | clustered | 16 | 44, 44, 44, 44, 42 | 2 |
| EVSEB | clustered | 64 | 56, 56, 56, 56, 54 | 3 |
| EVSEB | clustered | 256 | 116, 116, 116, 116, 114 | 4 |
| EVSEB | clustered | 1024 | 368, 368, 368, 368, 366 | 5 |
| EVSEB | clustered | 4096 | 1388, 1388, 1388, 1388, 1386 | 6 |
| EVSEB | scattered | 1 | 47, 47, 47, 47, 45 | 0 |
| EVSEB | scattered | 16 | 613 | 12 |
| EVSEB | scattered | 64 | 2189 | 12 |
| EVSEB | scattered | 256 | 7729, 7729, 7729, 7729, 7727 | 12 |
| EVSEB | scattered | 1024 | 26809, 26809, 26811, 26811, 26809 | 12 |
| EVSEB | scattered | 4096 | 90847, 90847, 90849, 90849, 90848 | 12 |

**What the numbers say.**

* Locality has a large effect for both schemes. DHT-backed at q = 1024 needs 2,058 proof lookups clustered and about 15,400 scattered; at q = 4096, 8,200 against about 53,800. EVSEB at q = 4096 loads 1,388 tree nodes clustered and about 90,800 scattered.
* **DHT-backed memory bounds.** `cassandradht.DefaultLimits` allows 32,768 requests and 8 MiB of proof bytes per call; the README measured the byte bound failing at about 21,000 lookups. Under these definitions the clustered q = 4096 batches (8,200 lookups) should fit, but the scattered q = 4096 batches (about 53,800 lookups) exceed both bounds. Scattered q = 1024 (about 15,400) is below the 21,000 failure point but I have not run it.
* The scattered DHT-backed batches reach the most trees and ancestors possible: all 5 trees, and all 155 populated ancestors once q >= 155 (so at q = 16 and 64 each record is in a different ancestor).

## Where the clustered batches are

| scheme | q | set | log type | leaves / ordinals |
|---|---|---|---|---|
| DHT-backed | 1 | 1 | api | leaves 996,496–996,496 (ordinals 5,000,630–5,000,630) |
| DHT-backed | 1 | 2 | api | leaves 996,497–996,497 (ordinals 5,000,637–5,000,637) |
| DHT-backed | 1 | 3 | api | leaves 996,498–996,498 (ordinals 5,000,660–5,000,660) |
| DHT-backed | 1 | 4 | api | leaves 996,499–996,499 (ordinals 5,000,671–5,000,671) |
| DHT-backed | 1 | 5 | api | leaves 996,500–996,500 (ordinals 5,000,676–5,000,676) |
| DHT-backed | 16 | 1 | access | leaves 203,088–203,103 (ordinals 997,789–997,856) |
| DHT-backed | 16 | 2 | api | leaves 597,888–597,903 (ordinals 2,999,577–2,999,668) |
| DHT-backed | 16 | 3 | auth | leaves 996,960–996,975 (ordinals 5,002,200–5,002,300) |
| DHT-backed | 16 | 4 | error | leaves 1,393,760–1,393,775 (ordinals 6,999,616–6,999,713) |
| DHT-backed | 16 | 5 | security | leaves 1,791,872–1,791,887 (ordinals 9,002,447–9,002,501) |
| DHT-backed | 64 | 1 | access | leaves 203,072–203,135 (ordinals 997,717–997,999) |
| DHT-backed | 64 | 2 | api | leaves 597,888–597,951 (ordinals 2,999,577–2,999,859) |
| DHT-backed | 64 | 3 | auth | leaves 996,928–996,991 (ordinals 5,002,022–5,002,395) |
| DHT-backed | 64 | 4 | error | leaves 1,393,728–1,393,791 (ordinals 6,999,494–6,999,803) |
| DHT-backed | 64 | 5 | security | leaves 1,791,872–1,791,935 (ordinals 9,002,447–9,002,732) |
| DHT-backed | 256 | 1 | access | leaves 203,008–203,263 (ordinals 997,328–998,576) |
| DHT-backed | 256 | 2 | api | leaves 597,760–598,015 (ordinals 2,998,945–3,000,160) |
| DHT-backed | 256 | 3 | auth | leaves 996,864–997,119 (ordinals 5,001,729–5,003,045) |
| DHT-backed | 256 | 4 | error | leaves 1,393,408–1,393,663 (ordinals 6,998,006–6,999,212) |
| DHT-backed | 256 | 5 | security | leaves 1,791,744–1,791,999 (ordinals 9,001,894–9,002,991) |
| DHT-backed | 1024 | 1 | access | leaves 202,752–203,775 (ordinals 995,950–1,001,120) |
| DHT-backed | 1024 | 2 | api | leaves 596,992–598,015 (ordinals 2,995,129–3,000,160) |
| DHT-backed | 1024 | 3 | auth | leaves 996,352–997,375 (ordinals 4,999,022–5,004,399) |
| DHT-backed | 1024 | 4 | error | leaves 1,392,640–1,393,663 (ordinals 6,994,118–6,999,212) |
| DHT-backed | 1024 | 5 | security | leaves 1,790,976–1,791,999 (ordinals 8,997,925–9,002,991) |
| DHT-backed | 4096 | 1 | access | leaves 200,704–204,799 (ordinals 985,867–1,006,239) |
| DHT-backed | 4096 | 2 | api | leaves 593,920–598,015 (ordinals 2,979,685–3,000,160) |
| DHT-backed | 4096 | 3 | auth | leaves 995,328–999,423 (ordinals 4,993,678–5,014,649) |
| DHT-backed | 4096 | 4 | error | leaves 1,392,640–1,396,735 (ordinals 6,994,118–7,014,359) |
| DHT-backed | 4096 | 5 | security | leaves 1,789,952–1,794,047 (ordinals 8,992,685–9,013,304) |
| EVSEB | 1 | 1 | – | leaves/ordinals 4,000,000–4,000,000 |
| EVSEB | 1 | 2 | – | leaves/ordinals 4,000,001–4,000,001 |
| EVSEB | 1 | 3 | – | leaves/ordinals 4,000,002–4,000,002 |
| EVSEB | 1 | 4 | – | leaves/ordinals 4,000,003–4,000,003 |
| EVSEB | 1 | 5 | – | leaves/ordinals 4,000,004–4,000,004 |
| EVSEB | 16 | 1 | – | leaves/ordinals 1,000,000–1,000,015 |
| EVSEB | 16 | 2 | – | leaves/ordinals 3,000,000–3,000,015 |
| EVSEB | 16 | 3 | – | leaves/ordinals 5,000,000–5,000,015 |
| EVSEB | 16 | 4 | – | leaves/ordinals 7,000,000–7,000,015 |
| EVSEB | 16 | 5 | – | leaves/ordinals 9,000,000–9,000,015 |
| EVSEB | 64 | 1 | – | leaves/ordinals 1,000,000–1,000,063 |
| EVSEB | 64 | 2 | – | leaves/ordinals 3,000,000–3,000,063 |
| EVSEB | 64 | 3 | – | leaves/ordinals 5,000,000–5,000,063 |
| EVSEB | 64 | 4 | – | leaves/ordinals 7,000,000–7,000,063 |
| EVSEB | 64 | 5 | – | leaves/ordinals 9,000,000–9,000,063 |
| EVSEB | 256 | 1 | – | leaves/ordinals 999,936–1,000,191 |
| EVSEB | 256 | 2 | – | leaves/ordinals 2,999,808–3,000,063 |
| EVSEB | 256 | 3 | – | leaves/ordinals 4,999,936–5,000,191 |
| EVSEB | 256 | 4 | – | leaves/ordinals 6,999,808–7,000,063 |
| EVSEB | 256 | 5 | – | leaves/ordinals 8,999,680–8,999,935 |
| EVSEB | 1024 | 1 | – | leaves/ordinals 999,424–1,000,447 |
| EVSEB | 1024 | 2 | – | leaves/ordinals 2,999,296–3,000,319 |
| EVSEB | 1024 | 3 | – | leaves/ordinals 4,999,168–5,000,191 |
| EVSEB | 1024 | 4 | – | leaves/ordinals 6,999,040–7,000,063 |
| EVSEB | 1024 | 5 | – | leaves/ordinals 8,998,912–8,999,935 |
| EVSEB | 4096 | 1 | – | leaves/ordinals 999,424–1,003,519 |
| EVSEB | 4096 | 2 | – | leaves/ordinals 2,998,272–3,002,367 |
| EVSEB | 4096 | 3 | – | leaves/ordinals 4,997,120–5,001,215 |
| EVSEB | 4096 | 4 | – | leaves/ordinals 6,995,968–7,000,063 |
| EVSEB | 4096 | 5 | – | leaves/ordinals 8,994,816–8,998,911 |

Record IDs for any batch: `str(ordinal).zfill(12)`, for example ordinal 4,000,000 is `000004000000`.

**Checked independently.** The raw dataset was re-read and every batch's lookups, ancestors, windows and tree nodes were recomputed with plain set-based code; the clustered and scattered definitions were checked on every batch. **Not run through the real systems:** the batches have not been executed against DHT-backed or EVSEB.

