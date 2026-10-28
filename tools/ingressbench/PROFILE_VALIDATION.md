# Listing CPU diagnosis — 2026-10-28

**The tested candidates do not pass the promotion gate. Production was not
changed.** The short runs identify expensive listing JSON work and continued
application CPU contention. They do not establish a complete production fix.
No ten-minute burst/publish or thirty-minute recovery validation was run for
these candidates because the short workload still dropped offered reads.

## Controlled workload and results

Each arm started with four Ready nodes, six Ready ingress controllers and all
four load-balancer backends healthy. The two apps remained on the same original
two nodes, one sharing the registry database. Pools stayed at 60 maximum / 10
minimum per replica; the DB quota stayed at three CPUs. Each arm used two minutes
at 100 reads/s, three minutes at 4,500 reads/s (including 36 updated listings/s and
three searches/s), and two minutes at 100 reads/s. These diagnostic runs contained
no publishes. The same four-vCPU generator, HTTP/1.1 driver, 4,000-key catalog and
80% hot-key mix were used. The catalog SHA-256 is
`120f34ec070fe08acf093e65f13a28631d944cf510915c65233b9eab62c143be`.

| Candidate | App CPU request | Offered reads | Generator-dropped reads | Attempt failures | Page p95 |
| --- | --- | ---: | ---: | ---: | ---: |
| Connection-release baseline + profiling | 250m | 810,000 | 123,015 (15.19%) | 95 | 1,236 ms |
| Bounded in-memory normalization cache | 250m | 810,000 | 257,289 (31.76%) | 9,517 | 1,059 ms |
| Prepared JSON stored with each record | 250m | 810,000 | 56,843 (7.02%) | 2 | 711 ms |
| Same prepared JSON image | 400m | 810,000 | 43,120 (5.32%) | 0 | 729 ms |

Dropped reads never reached an HTTP attempt. Zero attempt errors therefore does
not mean the last arm handled all offered work. Latency percentiles describe
attempted requests and cannot hide the missing work. Each short recovery had
12,000 offered reads with no drops or attempt failures; that is not the full
recovery or natural two-node validation.

The baseline source is commit `2a70d5d`, the rejected memory-cache prototype is
`9e27b14`, and the prepared JSON implementation is `6e77bb2`. The last two arms
used the identical image digest
`sha256:58671aecb4b2e66de0814567a241ca8b4a0fd7749678521bc0658f0b8240c5c2`.
The 400m request was a temporary staging patch, not a production configuration
change. Each arm is one observation, not a repeated capacity estimate.

## What the evidence establishes

CPU profiles identified typed JSON decoding and rebuilding the listing response
as substantial application work. In one baseline capture, JSON unmarshal used
33% of sampled CPU and the Huma response-writing subtree used about 30%; these
are different paths, not percentages to add to nested child functions. The new
checkout tracer showed little time between the final pgx query-end callback and
release entry. Query end includes row consumption: it is not the timestamp at
which PostgreSQL finished executing SQL.

The small normalization cache was ineffective for this workload: origin traffic
largely consisted of records absent from its 8,192-entry cache. Misses added
normalization work. It was rejected and removed from the final source.

Preparing normalized JSON during writes removed that read-time roundtrip without
relying on key reuse. Across both replicas, measured process CPU per origin
completion fell from approximately **4.69 ms to 3.10 ms (34%)** in the 250m
comparison. This is a counter-based interval estimate, not an isolated CPU cost
for a particular query. All 187,108 staging records were backfilled, all 48 direct
API comparisons matched, and the observed listing row counters reported no
fallbacks during the prepared bursts. The prepared payloads total 155,017,950 bytes
before PostgreSQL storage/compression overhead. Source JSON and official metadata
were not modified by the backfill. Ordinary VACUUM/ANALYZE finished before testing.

The prepared 250m arm still reached 893 sampled pool waiters on the database node;
the other app had none. In the 400m arm the affected replica switched to the other
original node: it reached 900 waiters and spent approximately 39% of the captured
cAdvisor interval fully CPU-stalled. Its peer had no sampled waiters. Origin
completion counts were nearly equal (40,863 and 40,893), so work was reaching both
replicas. A busy app can queue despite a healthy peer and spare cluster capacity.

Both app nodes also hosted busy ingress controllers. Their captured ingress CPU
averages in the 400m burst were approximately 1.74 cores across two controllers on
one node and 1.42 cores on the database node's controller. These exclude system
pods and kernel overhead and use slightly different sample intervals. CPU
contention is directly observed; the precise contribution of every neighboring
workload is not isolated by these runs.

For the 400m run, the dominant statement present in both bounded top-25 database
snapshots averaged **1.54 ms over 79,884 calls**, with no shared-block reads or
temporary-block writes in that delta. The interval includes preparation and
collection padding, and the bounded snapshots do not represent every statement.
The captured DB throttling counter did not increase during either prepared run;
the database stayed Ready with no app/DB restarts. This supports application CPU
contention and pool congestion rather than a demonstrated need for more database
connections. It does not prove that every slow request has the same cause.

The 400m generator had 58% mean / 67% peak aggregate CPU busy, approximately
2.03 Gbit/s mean received traffic, and no recorded interface drops. This argues
against aggregate generator CPU exhaustion; it does not exclude a single-thread,
scheduling or network limitation. The extra generator telemetry was collected
only for this arm.

## Decision and remaining work

Keep the PR in draft and do not promote these settings on the basis of this
result. Preparing JSON is a measured improvement, not the complete solution.
A larger connection pool and repeated full-length runs of this configuration
are not justified by the evidence.

The next capacity decision should address how origin-serving app CPU is shared
with ingress, rather than another database index. A bounded app-replica/placement
control can establish whether distributing origin work onto spare CPU removes
queues. If that control passes, app autoscaling needs a fixed total database
connection budget, a signal that detects queued demand even when CPU-starved,
and validation of both the scale-up delay and return to two idle nodes. Those
changes have not been implemented or proven here. A remaining JSON reformatting
pass also appears in profiles, but removing it is not yet a demonstrated solution
to the node contention.

The 400m request fits the observed original-node scheduling budget, while 500m
would exceed the database node's then-current allocatable CPU by about 6m. That
arithmetic does not prove natural two-node recovery or adequate runtime capacity.
The candidate was restored before a full recovery gate, so no such claim is made.

## Validation and cleanup

- `go test -race ./internal/... ./cmd/... ./pkg/... ./tools/prepare-listing-json`
  and lint passed. [CI at the measured implementation](https://github.com/modelcontextprotocol/registry/actions/runs/40680509055)
  passed.
- Tests cover typed/prepared API and OpenAPI equivalence, current metadata,
  changed payloads, large publisher numbers, old-writer invalidation, unknown
  codec fallback, conditional backfill rejection after a concurrent change,
  legacy identity mismatch fallback, empty listings and cancellation.
- Migration 018 adds nullable derived payload/codec columns. New writes update
  source and derived data atomically; an older writer changing source JSON
  invalidates the derived bytes. A codec bump is required if wire normalization
  changes. Backfill is an explicit, bounded maintenance tool, not startup work.
- The original staging app image, resource requests, affinity and HPA spec were
  restored. Temporary loopback profiling, the generator IP's rate-limit
  exemption, generator VM/disk and local test PostgreSQL were removed/stopped.
- The additive migration and derived payloads remain in staging; the original
  app ignores them. The corpus remains 187,108 versions / 52,366 names, with no
  benchmark publications from these diagnostic runs. All 48 response checks
  also matched after restoration, and the DB postmaster start was unchanged.
- Portable aggregate evidence is in
  [evidence/app-profile-2026-10-28](evidence/app-profile-2026-10-28). Raw profiles,
  per-pod samples, client results and capture status remain in the local
  `app-profile-diagnosis-2026-10-28` evidence directory. Raw profiles were not
  uploaded to the repository.
