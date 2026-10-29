# Ready capacity versus autoscaling — 2026-10-28

**The current candidate is not a complete burst-capacity fix.** Both arms failed
with the same application image, CPU reservation, connection pools and offered
traffic. Having four nodes and six ingress replicas ready before the burst did
not remove sustained queueing. Both arms returned naturally to two nodes, but
the autoscaling arm also had one connection refusal during recovery.

## Controlled comparison

Both arms used two connection-release application replicas with a 250m CPU
request, no CPU limit, and max60/min10 connections per replica. The application
pod UIDs and placement on the original two nodes were identical. PostgreSQL
retained its 2-CPU request, 3-CPU quota, 4-GiB memory budget, 512-MB shared buffers
and 200-connection ceiling. Neither database was moved or restarted.

The warm arm began with four Ready nodes, six Ready ingress replicas and four
healthy load-balancer backends. The autoscaling arm began with two of each and
allowed the existing ingress HPA (2–6) and node autoscaler (2–4) to respond. Both
used an 800m ingress CPU request and the same HPA policies. The warm arm's minimum
was returned to two immediately after its burst. No manual node deletion was
used to demonstrate idle consolidation.

Each arm had five minutes of 100 read requests/s preparation with no publishes,
then ten minutes of 4,500 reads/s including 36 uncached incremental-sync reads/s
and three varied searches/s, plus one publish/s. Recovery offered 100 reads/s
and one publish/s for 30 minutes. The burst used 1,024 read workers and a
1,024-entry queue; preparation/recovery used 128 each. HTTP/1.1, the 4,000-key
catalog, 80% selection from 128 hot keys, cache settings and transport behavior
were unchanged. Temporary rate-limit exemption covered only the generator IP.

The same four-vCPU generator VM and instrumented binary were used for all six
phases. Forty-eight direct-origin response hashes matched before both arms.
The first arm's 2,400 test publications were removed before the second, restoring
187,108 versions and 52,366 names. This comparison deliberately retained 250m:
the preceding 1-CPU candidate could not consolidate onto two nodes. It does not
isolate CPU priority from placement in that earlier experiment.

## Burst results

| Measurement | Ready capacity | Autoscaling from two nodes |
| --- | ---: | ---: |
| Burst start, UTC | 08:38:24 | 09:28:36 |
| Offered reads | 2,700,000 | 2,700,000 |
| Generator-dropped reads | 362,437 (13.42%) | 497,703 (18.43%) |
| Attempted read failures | 223 | 639 |
| Acknowledged publishes | 600/600 | 591/600 |
| Publish p95 / p99 | 6.30 / 6.83 s | 7.98 / 10.00 s |
| Page p95 / p99 | 650 / 5,565 ms | 660 / 6,102 ms |
| Read scheduler lag, mean | 0.39 ms | 0.16 ms |
| Read worker queue, mean | 224 ms | 243 ms |
| Read HTTP duration, mean | 261 ms | 278 ms |
| Generator CPU, mean / peak | 51% / 59% | 52% / 66% |
| Generator interface drops | 0 | 0 |
| Peak sampled pool waiters | 927 | 895 |

Dropped reads never reached HTTP dispatch and are separate from attempted
request failures. Client percentiles include intended-schedule-to-body-completion
time and failed attempts. Window timing counts reconcile with total scheduled,
dropped, dispatched and completed work. Aggregate generator CPU alone cannot
exclude every single-thread or network constraint, but sub-millisecond mean
scheduler lag and large downstream waits do not support a scheduling-capacity
explanation for the sustained losses.

Both arms fail the experimental gate: no failed or dropped work, page p95 at
most one second and publish p95 at most two seconds, followed by clean recovery,
natural two-node idle and a passing repeat. A passing repeat was not attempted
because neither candidate passed the burst gate.

## What this establishes

The warm arm dropped reads throughout the burst, including its final minute.
The autoscaling arm reached four healthy load-balancer backends and six Ready
ingress endpoints at 09:31:43 UTC, approximately 3.1 minutes after burst start.
It continued dropping reads after that point. This documents a capacity-readiness delay in the autoscaling arm, but it cannot
by itself explain the failure of the warm arm.

The application sharing the registry database node spent about 46% of the
captured warm-burst interval fully CPU-stalled, versus 13% for the other app.
In the autoscaling burst the corresponding values were about 45% and 19%.
These are cgroup CPU-pressure counter deltas over roughly 9.4–9.8 minutes,
not whole-run averages or percentages of the app's configured CPU request.
The database node was near full CPU while other nodes had spare capacity.

Origin arrivals were approximately 200 requests/s per app during the middle
and late warm burst. Equal request distribution therefore did not provide equal
processing capacity. The warm arm's peak sampled pool queue was 927, concentrated
on the app sharing the database node. Slow diagnostics showed multi-second pool
acquisition waits; a concurrent database snapshot had 118 idle connections and
three active sessions, including the diagnostic query.

The dominant page statement averaged 3.43 ms and 3.36 ms in the respective
bounded PostgreSQL snapshots bracketing preparation/burst and early recovery.
Those are SQL execution means, not end-to-end request latency or worst-case
query times. The sampled statement sets had no block reads or temporary-block
writes, and the statistics-reset and postmaster-start values were unchanged.
No database quota-throttling increment was observed during either burst.
Together, these measurements support application CPU contention with severe
pool congestion as a sustained bottleneck. They are consistent with slow
application-side connection turnover, but do not directly measure the interval
from SQL completion to connection return. They do not establish that every
individual failure has the same cause.

## Connection resets remain a separate uncertainty

The warm arm had timeouts but no connection resets. The autoscaling arm recorded
304 read connection resets and one publish connection reset, in addition to
335 read timeouts and eight publish timeouts. Its largest failure windows were
near new load-balancer backends becoming healthy. The original ingress pods had
no observed readiness loss or restart during the burst. This is correlation,
not proof that backend admission caused the resets; no packet capture establishes
their origin in this comparison.

The first 100 retained individual failures were early timeouts, so the bounded
sample omitted individual reset timestamps. Complete per-window failure totals
remain available. A subsequent benchmark-only change adds complete per-window
error-class counts for future tests. It was not substituted into either arm.

The autoscaling burst left 595 published records despite only 591 successful
acknowledgments. Thus at least four unsuccessful client attempts committed;
a failed client response must not be counted automatically as a failed database
write.

## Recovery and cleanup

| Measurement | Ready capacity | Autoscaling from two nodes |
| --- | ---: | ---: |
| Recovery window, UTC | 08:48:31–09:18:31 | 09:38:42–10:08:42 |
| Offered reads / publishes | 180,000 / 1,800 | 180,000 / 1,800 |
| Generator-dropped work | 0 | 0 |
| Read failures / publish failures | 0 / 0 | 1 / 0 |
| Recovery page / publish p95 | 8.06 / 12.98 ms | 8.14 / 13.95 ms |
| Two-node idle confirmed, UTC | 09:22:18 | 10:10:54 |

Queues drained in both arms. The autoscaling recovery had one connection refusal
at 09:52:59 UTC, near an ingress endpoint becoming terminating while its node
still appeared healthy to the load balancer. This is another scaling-transition
failure to investigate; the sampled observations do not identify the rejecting
backend. Thus the autoscaling recovery does not pass the zero-error gate.

Both arms consolidated naturally to two Ready nodes with the same application
pods and placement, before restoring the pre-experiment deployment. The last
spare node disappeared after each 30-minute client recovery finished. Continued
idle observations establish consolidation, but are not additional measured
recovery traffic. This result applies to the 250m candidate, not the preceding
1-CPU configuration that remained on three nodes.

No application or registry-database restart, registry-database endpoint loss,
or snapshot command failure was observed in either arm. The load-balancer
health collector had one failed query after the final autoscale node was deleted;
that sample is missing telemetry, not a successful health check. Database CPU
throttling increased by 0.215 seconds over the warm observation and 0.340 seconds
over the autoscaling observation, outside their bursts. Empty alert/5xx metric
series are not evidence that every request succeeded; the client recorded
transport failures.

Cleanup is complete. The original staging app image and 250m CPU request, HPA
specification and rate-limit annotation were restored. The generator VM and its
boot disk were deleted. Scoped cleanup removed 2,400 warm-arm publications and
2,395 autoscaling-arm publications; the corpus returned to 187,108 versions and
52,366 names with no benchmark records. All 48 direct-origin response hashes
still matched. At 10:15:42 UTC the restored deployment had two healthy app replicas
on two Ready nodes. The registry database postmaster start remained unchanged.
Production was not modified.

## Interpretation and next test

Do not promote ingress/node autoscaling alone as the solution, and do not treat
larger connection pools as a demonstrated remedy. The next application-capacity
experiment should address CPU contention and work placement while retaining a
bounded aggregate connection budget and proving the two-node idle state. A
smaller CPU reservation increase that fits the baseline is a useful isolated
control; application scaling/placement should be tested separately if it cannot
meet the gate. The reset mechanism also needs complete error-class timing and
packet-level evidence during scale transitions before claiming a clean rollout.

This is one controlled pair, not proof of an exact production traffic mix or a
10× capacity margin. Ready capacity changes cache readiness and connection
distribution as well as node startup timing. `updated_since` values advance with
wall clock in both arms. Pool gauges are scraped every 60 seconds and direct
snapshots every 30 seconds; brief events can be missed. Ingress counters can
include traffic beyond the generator, so client counters define offered and
completed benchmark work.

## Reproduction and validation

Application source: `3ea1fa2e974d97bff88cd0f04dc95fac5dea5f68`.
Image: `sha256:94f184cd38345b059f440756d5eaa22448dbc7d827490220d144bd9356713f94`.
Measured driver source: `4eb0c10810904e47696ea853b09b1348e68f38b5`.
Measured binary: `96693b313ae702e76943ef2242e3de5c471ff3d0f0011b6999a51b88dfeee1a2`.
Catalog: `120f34ec070fe08acf093e65f13a28631d944cf510915c65233b9eab62c143be`.

Benchmark race tests, lint and GitHub CI passed at `4eb0c10`. The subsequent
per-window error-class addition passed those checks at `b14b0ec`. No production
configuration or deployment was changed. See `README.md` for driver invocation
and measurement semantics, and `CONNECTION_CPU_VALIDATION.md` for the preceding
CPU/connection-lifetime comparison.

The portable measurements are in
[`results/2026-10-28-warm-capacity.json`](results/2026-10-28-warm-capacity.json).
They retain phase totals, timing conservation, generator/pressure summaries,
database deltas, integrity checks and cleanup verification.
