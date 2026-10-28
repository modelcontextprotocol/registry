# Production workload qualification — 2026-10-28

**The earlier 4,500-read/s test is a stress scenario, not a qualified replay of
the production incident.** It reproduced CPU contention, but material workload
differences prevent choosing the final capacity configuration from it alone.
Production and staging configuration were not changed during this audit. No
generator VM was created.

Follow-up: [the ordered read-trace comparison](WORKLOAD_COMPARISON.md) qualifies
a final-minute broad-crawl read mix and compares the app and infrastructure
candidates. Its cold-start limitations prevent treating it as a full-cycle pass.

## Evidence

The [portable profile](evidence/workload-profile-2026-10-28/profile-summary.json)
combines four hours of historical application metrics (16:30–20:30 UTC) with
17 complete, one-minute Cloud Logging queries spaced 15 minutes apart. These
queries returned 962,375 records, including 959,671 parsed access records and
870,686 client records. The 2,704 unparsed records are retained as a coverage
limitation; they are not assumed to be successful requests.

Each query was paginated to completion. These are samples, not continuous
ingress history or a request-arrival trace. Rates below count completions within
the selected minute. Zero-request-length records, used as a proxy for NGINX
background subrequests, are counted separately. Neither application request
rate nor all ingress log records should be equated with client demand.

| Sample start (UTC) | Client requests/s | Client cache HIT | Top 128 page URI share | Distinct page URIs/min |
| --- | ---: | ---: | ---: | ---: |
| 16:30 | 308 | 30.0% | 6.0% | 9,524 |
| 17:00 | 1,783 | 93.9% | 98.5% | 1,547 |
| 17:45 | 1,984 | 87.2% | 17.9% | 8,560 |
| 18:30 | 911 | 69.3% | 13.6% | 11,833 |
| 19:00 | 478 | 40.6% | 10.9% | 12,231 |
| 19:30 | 187 | 17.0% | 12.4% | 7,310 |
| 20:30 | 147 | 15.6% | 9.0% | 5,059 |

The cycle changes locality as well as rate. A short sample can be particularly
misleading: the first ten seconds at 17:00 contained 165 client records, whereas
the full minute contained 106,975. An isolated quiet slice is not a baseline.

Application metrics independently show CPU saturation during the busy period:
at 17:45 both nodes were around 98–100% CPU, and v0.1 listing p95 was about
405 ms. At 20:30 they were around 25–35%, listing p95 about 86 ms, and the sampled
pending-acquisition gauge was zero. These are minute snapshots and rolling
histogram estimates, not proof that every intervening request recovered.

## What the old test got wrong

The catalog is ordered by API version and limit. Selecting 80% of page requests
from its first 128 entries made **85.02% of offered pages use `/v0` with limit
50**, with only 4.99% using `/v0.1` with limit 100. At 18:30, **92.9% of production
page requests used `/v0.1` with limit 100**. During the main observed wave that
share was roughly 93–99%. The test therefore exercised a different response
shape, not merely a higher rate of the same work.

Other differences remain material:

- The production minute at 18:30 contained 11,833 distinct page URIs, versus a
  total test catalog of 4,000. Its top-128 share was 13.6%, compared with the
  deliberate 80% hot selection in the test. Production locality varies sharply
  across the cycle, so replacing 80% with one other fixed percentage is also
  insufficient.
- That minute included 8.4 client searches/s and about 0.93 incremental reads/s.
  The old test offered 3 and 36 respectively. Origin DB query rates are a
  separate measurement because caches and background refreshes intervene.
- About 97.7% of client records in that minute used HTTP/1.1, but HTTP/2's share
  changes during recovery. The logs do not expose TCP connection IDs or reuse
  lifetimes. Synchronized 30-second generator rotation is an assumption.
- The old short gate jumps to a constant rate for three minutes. Production's
  rise, sustained work and recovery span hours. Warm four-app/six-ingress tests
  also do not reproduce scale-up from the production two-node baseline.
- Staging's recent DB requests, ingress requests and candidate image differ
  from production. These are deliberate candidate changes, but require a
  production-aligned baseline for causal comparison.

## Implemented correction

The driver now accepts explicit page classes and anonymous per-key frequencies,
validates distinct catalog capacity, records the selected profile hash and
attempted request shapes, and supports disabling forced connection rotation.
The legacy mode remains available for reproducibility and is labeled stress.

The [measured broad-crawl page model](evidence/workload-profile-2026-10-28/measured-pages.json)
contains 53,463 page observations from the 18:30 minute, excluding 16 unsupported
page requests. A read-only staging catalog query mapped its 11,346 normalized
key ranks to distinct valid staging cursors. Query ordering/default limits are
normalized, explaining part of the difference from 11,833 raw URI cache keys.
This model is not a chronological client-session replay. It does not preserve
exact cursor positions, first-page frequency or response-size distribution.

A local HTTPS fixture completed 300/300 requests with no drops or failures.
The small live staging check scheduled 200 reads at 10/s with four workers:
199 were attempted and returned HTTP 200; one was dropped by the bounded local
queue. Mean decoded body size was about 102 KB. This verifies usable requests,
**not** a capacity or fidelity pass; production log bytes are wire sizes and
cannot be equated with decoded response bytes. No publish requests were made.

Go race tests, parser/privacy/pagination tests, and repository lint pass. Tests
specifically cover catalog-order bias, insufficient distinct keys, filter
isolation, page sequencing across search/update slots, bounded result labels,
background/client separation and incomplete pagination.

## Gate before another capacity comparison

First calibrate a baseline using the production release and resource settings.
Compare both client demand and work reaching the app/DB. Proposed calibration
tolerances are engineering criteria, not an established production SLO:

1. Match API version, limit/filter and protocol shares within five percentage
   points for each phase; include observed searches, updates, details and writes.
2. Match distinct keys/minute and response-size p50/p95 within 20%, and top-128
   popularity/cache-outcome shares within five percentage points. Measure both
   wire and decoded bytes before using their distributions interchangeably.
3. Match origin page/search/update query rates within 20% over comparable
   rolling windows. Reproduce CPU pressure and acquisition-delay symptoms;
   matching total client RPS alone is insufficient.
4. Preserve the real duration and changing locality, or explicitly label a
   shortened experiment as stress. Account for initial cache state and the
   actual transition from idle capacity.
5. Treat unmeasured connection reuse/retries and search selectivity explicitly.
   A bounded sensitivity comparison can test assumptions, but cannot turn them
   into observed production facts.

Only after the baseline passes workload qualification should the candidate run
against the same workload and recovery criteria. The full baseline/candidate
capacity comparison has **not** run as part of this audit. No production
promotion is justified by the fixture or low-rate staging check.
