# Production read-trace comparison — 2026-10-29

The prepared-listing app reduced measured CPU use and startup delivery losses.
The coordinated infrastructure candidate scaled from two to four nodes, but
neither candidate passed the short delivery gate. Production was not changed.
This is a four-minute broad-crawl comparison, not a completed production-cycle
or publish validation. A separate thirty-minute low-load recovery completed.

[Aggregate comparison](evidence/workload-comparison-2026-10-29/broad-comparison.json),
[read-mix qualification](evidence/workload-comparison-2026-10-29/baseline-trace-assessment.json),
and [source coverage](evidence/workload-comparison-2026-10-29/read-trace-summary.json)
are checked in. Request catalogs, search text and raw access logs are excluded.

## Controlled comparison

The same ordered trace offered 274,116 public GET reads over four minutes,
including 270,766 page reads, 2,235 detail reads, 974 searches and 141 updated
listings. It preserves relative arrivals and reuse of 34,542 raw cache keys
from the production interval 18:27–18:31 UTC on October 28. Catalog and trace
hashes match across all arms; private request paths and search parameters are
not published. Forced generator connection rotation was disabled throughout.

| Setting | Production-aligned baseline | App only | App + coordinated infra |
| --- | --- | --- | --- |
| App image | v1.8.6 (production digest verified) | Prepared-listing candidate | Same candidate |
| App replicas / CPU request each | 2 / 250m | 2 / 250m | HPA 2–4 / 800m |
| Ingress replicas / CPU request each | 2 / 300m | 2 / 300m | HPA 2–6 / 800m |
| Ingress memory request each | 90Mi | 90Mi | 512Mi |
| PostgreSQL CPU request / limit | 200m / 3 CPUs | 200m / 3 CPUs | 1500m / 3 CPUs |
| App pool max/min each | 60/10 | 60/10 | 30/5 |
| Nodes | 2 | 2 | Autoscaling 2–4 |

The infrastructure arm also restores the candidate's 60-second ingress
connection lifetime. Its changes are a package; this experiment cannot assign
an individual benefit to CPU reservations, pool sizes or HPA settings.

All arms start with two Ready app and ingress replicas and empty ingress
caches after a normal rollout. No old terminating ingress pods remained in
the saved start snapshots. PostgreSQL was warm for baseline/app; changing its
CPU request restarted it before the infra arm, followed by two minutes of
100-read/s conditioning. That does not establish identical DB cache state.

## Delivery results

| Measurement | Baseline | App only | App + infra |
| --- | ---: | ---: | ---: |
| Offered | 274,116 | 274,116 | 274,116 |
| Attempted | 268,352 | 271,813 | 272,191 |
| Dropped before dispatch | 5,764 (2.10%) | 2,303 (0.84%) | 1,925 (0.70%) |
| Transport failures | 48 | 0 | 7 |
| Page p95, attempted requests | 3,322 ms | 1,597 ms | 1,667 ms |
| Last window containing drops ends by | 78.1 s | 43.9 s | 83.1 s |

The generator used 1,024 read workers and a bounded 1,024-job queue.
Drops are generator-side queue rejections before dispatch, not HTTP server
errors; they show unmet offered load under this concurrency policy. Attempted
latency excludes that work.
The final infra minute had no drops and ten-second-window mean HTTP durations
of roughly 7–24 ms. This supports a transition problem rather than proving
that sustained demand exceeds the scaled cluster's capacity. Empty caches and
an abrupt replay start make this harsher than entering the middle of a warm
production cycle; these results do not isolate cold cache as the sole cause.

The baseline and app arms each returned 28 detail 404s: those objects were
absent in the older staging corpus. Infra returned 22 detail 404s and 42 detail
429s. Only the bulk-read ingress exempted the generator from rate limits, so
those 429s are a single-source test artifact, reported separately. Infra also
had six page body connection resets and one body timeout. Their cause is not
established by the aggregate result.

App-only sampled CPU use fell from about 2.13 to 1.47 cores across both apps
(approximately 31%), despite attempting more requests. CPU full-stall rates
also fell, from 17–30% to 10–18% across the apps. These are sampled intervals
within one run, not repeated-run confidence intervals. The maximum sampled
page pool-wait gauge fell from 397 to 189, and rolling page acquisition p95
from 4.72 s to 1.21 s. Histograms use rolling windows and app scrapes are 60
seconds apart, so these are not exact phase boundaries. Forty-eight saved public
response fixtures matched their reference hashes. No publish traffic was run.

## What matches production, and what remains unproven

The initial frequency-based approximation failed cache/origin-mix checks.
Using the ordered trace corrected that mismatch: all twelve selected final-
minute checks passed the declared tolerances. Cache outcome shares were within
five percentage points; wire response sizes, key diversity and time-aligned
origin page/search/update rates were within 20%. This qualifies that minute's
read mix. It does not retroactively qualify the startup cache state or the full
rise, peak and recovery of the production cycle.

Only successful source reads were replayed. The source contains about 1.8%
HTTP/2 reads; the generator used HTTP/1.1. Client TCP sessions, retries and
connection lifetimes cannot be recovered from these logs. Arrival times are
estimated from completion timestamps minus request durations. A separate
1,227,778-read, ten-minute peak trace was collected but was not executed after
the short gate failed. Write contention and the longer peak remain untested.

The next acceptance run should start with the preceding observed traffic to
establish cache state, then continue into the selected peak, with corpus and
rate-limit mismatches resolved before comparing configurations. Keep the
empty-cache case as a separate resilience test. Repeating an abrupt cold start
and calling it the full production incident would not improve confidence.
Do not increase the connection pool or promote the infrastructure package on
these results alone.

## Recovery and restoration

The infrastructure arm completed **180,000/180,000 recovery reads at 100/s**
without drops, transport failures or non-200 responses. Page p95 was 11.38 ms.
The final five minutes had zero sampled pending-acquisition and in-flight
counts; these are 60-second app scrapes, not proof of zero instantaneous work.

HPA first reported two app replicas after 12.8 minutes and two ingress replicas
after 15.8 minutes. Nodes scaled from two to four during the trace and back to
three by the final candidate snapshot at 23:33:44 UTC. The autoscaler was
Healthy with bounds 2–4 and one remaining scale-down candidate. **Return to
two nodes within thirty minutes was not demonstrated.** No forced eviction or
node removal was used to manufacture a pass. The final candidate snapshot
precedes restoration; later observations of the restored configuration must
not be credited to this candidate.

PostgreSQL recorded only two throttled periods and 24.1 ms of throttled time
across the candidate observation, including recovery. The measured VM CPU
averages were approximately 10.0%, 9.4% and 8.4% for baseline, app and infra,
with zero network-device error/drop increments. Aggregate utilization does
not exclude single-thread or external-network limits. All collected node
cAdvisor snapshots succeeded and contained telemetry.

[Recovery results](evidence/workload-comparison-2026-10-29/recovery-comparison.json)
and the [final verdict](evidence/workload-comparison-2026-10-29/final-summary.json)
retain the delivery and footprint gates separately.

[Restoration verification](evidence/workload-comparison-2026-10-29/cleanup-summary.json)
confirmed the original app and ingress Deployment specs, ingress HPA/ConfigMap,
DB resources (2-CPU request / 3-CPU limit) and bulk whitelist. The experimental
app HPA was removed; the primary ingress was unchanged. App, ingress and DB
were Ready. All 48 response hashes matched, the corpus remained 52,366 names /
187,108 versions, and no test publications existed. The owned generator VM and
boot disk were verified absent, and the temporary publish token was removed.

The remaining spare node began removal at 23:34:04 UTC, overlapping the start
of restoration, so that is not an isolated two-node candidate observation.
During restoration an ingress pod triggered another scale-up: scheduler events
reported insufficient CPU and topology-spread constraints while a node was
unschedulable. The restored snapshot at 23:39 UTC had four Ready nodes within
the unchanged 2–4 bounds. This is separate rollout-induced capacity, not the
candidate's recovery result. Natural consolidation remains pending; restoring
configuration does not mean immediate restoration of idle compute cost.
