# Staging broad-search validation — 2026-10-27

The tested combination passes this workload: select page keys using covering
indexes before fetching records, plan searches for their actual parameter values,
and give PostgreSQL a **3-CPU quota on the existing nodes**. The query change alone
at 1.5 CPUs did not pass. Increasing the connection pool is not part of the fix.

## Repeated comparison

Both versions used the same 3-CPU quota, 4-GiB database memory limit, 512-MB shared
buffers, 200 PostgreSQL connections, and two application pools of max 60/min 10.
Both had the new covering indexes. The control is the previous staging image at
`b96c5b255026fe45319c786a61d07095e2a49d9f`, including the earlier namespace fix;
it is not the older production v1.8.5 image. Order was A/B/B/A.

| Trial | Successful searches | Dropped searches | Complete walks | Publishes | Publish p95 | Peak pool waiters | DB CPU time |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| A1 existing | 11,762/12,000 | 238 | 30/30 | 90/90 | 604 ms | 37 | 224.5 s |
| B1 candidate | 12,000/12,000 | 0 | 30/30 | 90/90 | 290 ms | 7 | 189.3 s |
| B2 candidate | 12,000/12,000 | 0 | 30/30 | 90/90 | 450 ms | 9 | 197.7 s |
| A2 existing | 8,716/12,000 | 3,284 | 30/30 | 90/90 | 705 ms | 64 | 233.0 s |

Both candidate repetitions had **zero request failures and zero driver drops**.
All 83 complete HTTP-response hashes matched the control across 37 search terms,
both API versions, latest/all-version filters and pagination. Database and handler
unit/race tests also cover literal escaping, case, version, latest=false, updated
and deleted filters, cursor progression and full response metadata.

The control was CPU-throttled in 374 and 525 scheduling periods; the candidate in
2 and 6. The driver had zero throttled periods in every primary run. Final complete
metric samples showed zero pool waiters in all four runs. Origin request counts
were approximately 50/50 across the app replicas.

## Workload and observation window

- Fixed production-data snapshot: 187,108 versions and 52,366 distinct names.
  Only the public servers table was copied; production authentication material
  was not copied.
- Two existing e2-standard-4 nodes, two real app replicas, two real ingress caches.
  No additional nodes or machine-size changes were made for this test.
- 200 uncached searches/second for 60 seconds: 12,000 scheduled searches, 128
  workers and a bounded 128-request queue. Half used varied real full names;
  the rest included broad, sparse, numeric and absent terms. Search requests
  bypassed ingress caches and were balanced across both apps. No short-search
  rejections were credited as throughput.
- 30 real full walks in three waves at 0/20/40 seconds, across both API versions,
  latest/all/incremental filters, default 30/50/96/100 page sizes and varied query
  parameter order. Walks followed real cursors and checked ordering/duplicates.
  A shared 1,200-request/second ceiling paced crawler starts; this is not a claim
  of sustained server throughput.
- 90 authenticated publishes at 1/second, through the genuine handler and database
  transaction, restricted to `io.benchmark/*` and removed afterward.
- Fresh app processes and cold ingress caches for each run. PostgreSQL was not
  restarted between the four primary trials. Candidate static Linux/amd64 binaries
  were loaded through temporary init containers into the same base image with
  unchanged app resources/environment; their SHA-256 was verified before launch.

The original **100-second observation** was retained in the initial experiments.
A crawl-only control with no searches still completed only24/30 walks by that
cutoff, despite a peak of one pool waiter and publish p95 of 91 ms. Its remaining
six default 30-page walks were still progressing. Therefore the repeated suite
extended only observation to 180 seconds. Search arrivals, 90 publishes, crawl waves,
the crawl pacing ceiling and each request's10-second deadline were unchanged.
Searches still had to complete within their original deadlines; the extra time
only observed outstanding walks and recovery. Original cutoff failures remain in
the machine-readable results and are not reclassified as successful requests.

## Why the fix helps

The old broad plans visited roughly39,000 cached database blocks while rejecting
about42,000 names before the first page. A covering ordered index permits the key
selection to reject names without heap visits; the payload join fetches only the
requested page, within the same SQL statement and MVCC snapshot. A second covering
index handles the existing namespace-suffix predicate without combining a large
trigram scan with heap access.

A materialized candidate-key query plans for at least 8,192 keys. The separate outer
page limit preserves the API limit. This avoids the most optimistic small-LIMIT
plans for sparse searches; on PostgreSQL 17 the ordered CTE can stop when the outer
page has enough rows. Searches use pgx's unnamed extended-query mode so PostgreSQL
plans with the actual term instead of reusing one generic plan across vastly
different selectivities. Values remain bound parameters.

A separate sampled auto_explain pass captured generic ordered scans in the rejected
automatic-plan variant, including numeric searches. The final candidate diagnostic
captured seven search plans, all parameter-specific, including the covering-index
paths. It was outside the primary A/B/B/A measurements and its role settings were
removed afterward. The planner evidence does not imply that every earlier incident
was caused by generic plans: expensive custom broad scans were also observed.

## Rejected experiments and limits

At1.5 CPU, simple key-first lookup completed8,454/12,000 searches, the extra-index
variant7,797, the materialized variant without parameter-specific planning6,268,
and the final query variant9,366. Each failed the offered-work gate. A separate
max 8/min 2 pool experiment completed7,393; a smaller pool alone was not sufficient.
These exploratory runs changed different factors and are not an isolated causal
comparison of pool sizes. The pool remains max 60/min 10 per app.

With3 CPU and the original100-second cutoff, the final candidate completed all
12,000 searches and90 publishes with zero search drops/failures, while15/30 walks
finished before cutoff. The repeated180-second comparison above then verified
complete walks and recovery, without increasing offered traffic.

This validates the specified burst on this fixed corpus and infrastructure, not
unlimited future traffic or a captured replay of every production client. Broad
substring filtering still performs work proportional to the names examined;
larger datasets or substantially higher uncached search rates need new capacity
measurements. The first control followed a database restart; the second control
was warm and still dropped3,284 searches. Candidate-only diagnostics and initial
screening runs are not mixed into the primary comparison.

## Rollout

Prebuild and verify both migration 017 indexes concurrently on production before
application migration; see [deployment instructions](../../deploy/README.md#search-page-covering-index).
The tested production promotion must include the3-CPU database budget; the code
change alone at 1.5 CPU was insufficient. This PR opts staging into3 CPU and adds an
optional setting with a1.5 CPU default, leaving production configuration unchanged.
Changing the current singleton's resources causes a managed restart and needs an
explicit rollout plan. The node types/count, memory, storage and pools stay fixed.

After validation, temporary drivers, cache-bypass annotations, diagnostic settings
and test publications were removed, and staging was restored to its previous
application and1.5 CPU quota. No production writes, release or promotion occurred
as part of these experiments. Staging's permanent node/ingress parity was retained.

Machine-readable results: [2026-10-27-staging-search.json](results/2026-10-27-staging-search.json).
