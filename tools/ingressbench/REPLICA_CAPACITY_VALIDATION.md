# Fixed-budget app replica comparison — 2026-10-28

**Four apps substantially improved origin latency and sampled pool queues, but
the short offered-load gate did not pass. Do not promote or treat this as an
autoscaling validation.** Production was unchanged. The conditional ten-minute
burst with publishes and thirty-minute recovery was not run.

## Controlled comparison

Both arms used commit `43e17e0993e1afed08950da75b461a1e4966fab3`, prepared listing
JSON, the same image digest
`sha256:60fb8b7d7931a257542848460f7d1c76a99f16c46ddad795249fb5c25b1c9b0e`,
400m CPU request per app, no app CPU limit, four Ready nodes and the same six
ingress controller UIDs and placements. The DB quota remained three CPUs.
The control placed two apps on the original two nodes; the candidate placed one
app on each of the four nodes. Total app CPU requests therefore rose from 800m
to 1,600m: this tests the proposed replica/placement package, not replica count
in isolation.

The new pool configuration kept the aggregate budget fixed: two apps at 60/10
maximum/minimum connections versus four at 30/5, both totaling 120/20. Live
per-app pool maximum gauges were checked before each arm. Scaling transitions
were sequenced to avoid exceeding that budget; a future deployment must also
budget for rolling-update surge replicas.

Each arm used two minutes at 100 reads/s, three minutes at 4,500 reads/s and two
minutes at 100 reads/s. The burst included 36 updated listings/s and three
searches/s within the total, 4,000 cache keys, an 80% hot-key mix, HTTP/1.1,
1,024 workers and a 1,024-job queue. There were no publishes. Generator VM,
binary, catalog and connection rotation policy were unchanged. Hashes and the
gate recorded before the bursts are in [experiment.json](evidence/replica-capacity-2026-10-28/experiment.json).

| Measurement | Two apps, 60/10 each | Four apps, 30/5 each |
| --- | ---: | ---: |
| Scheduled reads | 810,000 | 810,000 |
| Reads dropped before an HTTP attempt | 50,653 (6.253%) | 48,899 (6.037%) |
| Attempt failures | 0 | 0 |
| Page p95, scheduled arrival to body completion | 688 ms | 621 ms |
| Search p95, scheduled arrival to body completion | 2,702 ms | 804 ms |
| Updated-listing p95, scheduled arrival to body completion | 3,360 ms | 829 ms |
| Largest sampled pool queue on one app | 905 | 3 |
| Ingress upstream-response p95 bucket | >2,500–5,000 ms | >100–250 ms |
| Ingress total-request p99 bucket | >1,000–2,500 ms | >250–500 ms |
| Short recovery: dropped reads / attempt failures | 0 / 0 | 0 / 0 |

The client percentiles include generator scheduler and worker-queue delay.
Ingress histograms independently support an origin-serving improvement; they
cover the bulk-read ingress and use a scrape-based 180-second counter increase
ending at burst completion. They are bucket bounds, not exact percentiles or
matched per-request traces. Upstream timing counts origin responses; total
request timing includes cached responses. Pool gauges were sampled every 15
seconds and can miss shorter queues. Each arm is a single observation.

## What improved, and what remains unresolved

Origin completions were balanced across replicas: 41,547/41,562 in the two-app
counter interval and 20,389–20,395 per app in the four-app interval. The four-app
arm observed 5.81 million prepared listing rows and no fallback rows. Keeping
120 total maximum connections while reducing sampled queues strongly argues
against increasing the DB pool as the next intervention.

CPU contention was reduced, not eliminated. The two apps spent about 28% of
their bounded cAdvisor intervals fully CPU-stalled. The four apps ranged from
3.3% to 22.8%; the highest remained on the database node. Its mean page-pool
acquisition time was still 80 ms, versus roughly 1–5 ms on its peers, even though
its periodic pending gauge samples were zero. Do not read sparse gauge samples
as proof of no waits.

The dominant statement in the bounded database deltas averaged 1.59 ms over
81,657 calls in the control and 1.82 ms over 80,475 calls with four apps. These
snapshot intervals include preparation and collection padding and only cover
statements present in both top-25 snapshots. No shared-block reads or temporary
block writes appeared in that statement's delta. DB quota-throttling counters
did not increase within either captured run, and app/DB restarts stayed at zero.
These observations do not demonstrate a database-capacity problem.

The residual dropped work cannot yet be assigned entirely to the server or
entirely to the generator. Both drivers received about 2 Gbit/s; aggregate CPU
busy averaged 61.5% and 65.0%, with no recorded interface drops. However, maximum
scheduler lag was 828 ms and 490 ms. The scheduler catches up to absolute
deadlines after a pause and drops jobs when its bounded queue is full. Slow
server responses can also keep those workers busy. Aggregate CPU headroom and
interface counters do not exclude single-thread, scheduling or network limits.

In the four-app run, seven complete ten-second windows scheduled 45,000 reads
with no drops, while other windows lost work. The existing measurements lack
client connection/DNS/TLS/first-byte timing and latency broken down by cache
outcome, so they cannot isolate why the remaining intermittent stalls occur.
There is no evidence here for a claimed fixed 2-Gbit/s network cap.

## Decision

The fixed-budget replica change is a measured mitigation for origin contention,
not a proven complete solution at the intended arrival rate. Stop increasing
replicas or pool sizes based on these short runs. Keep the PR in draft.

Before another capacity verdict, validate that the load generator can deliver
the intended arrival pattern independently of the registry, and separate
scheduler/queue, connection establishment, first-byte and body-transfer timing
by cache outcome. A split-generator control would help distinguish one client's
limit from aggregate server capacity, but must preserve the total request mix
and key distribution. These are next diagnostic requirements, not completed
tests or permission to generate production load.

Only after a credible short offered-load pass should the full burst/publish and
recovery gate run. Dynamic app scaling, scale-up delay and return to two nodes
remain separate unproven properties; no app HPA was implemented here.

## Validation and restoration

- Race tests (`go test -race ./internal/... ./cmd/... ./pkg/...`) and lint passed.
  [CI for the measured source](https://github.com/modelcontextprotocol/registry/actions/runs/40704622588)
  passed. Pool tests exercise defaults, explicit 30/5 limits, invalid settings
  and the deadline of a 31st acquisition with 30 connections held.
- All 70 two-app and 140 four-app direct capture operations succeeded; bounded
  runtime commands succeeded and database endpoint samples remained Ready.
- The original app image, 250m requests, two replicas, affinity, ingress HPA
  specification and rate-limit annotation were restored. Profiling was removed.
  The generator VM and disk were deleted and local test PostgreSQL stopped.
- All 48 API comparisons matched after restoration. The database retained
  187,108 versions / 52,366 names, with the same postmaster start time. No test
  publications were created. Additive prepared-JSON data from the previous
  experiment remains in staging; the original app ignores it.
- At 14:50 UTC, ingress had decreased from six to four replicas and the cluster
  autoscaler identified a removal candidate; four nodes still awaited normal
  cooldown. Node-pool bounds remained 2–4. Restoration is not proof of the
  candidate's natural two-node recovery.

Portable aggregates are in
[evidence/replica-capacity-2026-10-28](evidence/replica-capacity-2026-10-28).
Raw client results, direct metrics, CPU profiles, cAdvisor samples and historical
queries remain in the local `replica-capacity-validation-2026-10-28` directory.
Raw profiles and private deployment snapshots are not included in the PR.
