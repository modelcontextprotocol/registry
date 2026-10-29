# Coordinated CPU capacity candidate — 2026-10-28

**Failed the staging short delivery gate. Production is unchanged.**
After reauthentication, the candidate was applied in staging and tested with
four Ready apps, six Ready ingress replicas and four healthy node backends.
Of 810,000 scheduled reads, 7,418 (0.916%) were dropped before dispatch.
All 802,582 attempted requests succeeded, but their latency excludes dropped
work. The long burst, publish test and autoscaling recovery gate were not run.
The opt-in remains disabled; this result does not support promotion.

Follow-up: the [production workload qualification](WORKLOAD_PROFILE.md) found
material API/page-size, cache-locality and duration differences in this stress
scenario. Its failed gate remains a failed gate, but it is not a validated
acceptance test for the actual production cycle. Qualify the workload baseline
before another capacity comparison.

Latest follow-up: [the production read-trace comparison](WORKLOAD_COMPARISON.md)
measures the app improvement and the infrastructure transition separately,
with explicit cache-state and workload-coverage limitations.

## Revised target — 2026-10-29

The selected next candidate is **three idle nodes, up to four**, with three
Ready apps (maximum four) and three Ready ingress controllers (maximum six).
Keep staging at this intended production footprint until validation passes;
production promotion remains gated. App CPU stays 800m, pool maximum/minimum
30/5 per replica, ingress CPU 800m, and registry DB request/limit 1500m/3 CPUs.
This is a new candidate, not a passing result. Historical measurements and the
older two-node test procedure below remain unchanged for reproducibility.

Use the ordered production trace and fidelity checks from
[the latest comparison](WORKLOAD_COMPARISON.md) for acceptance. Record cache state
and the rise from three Ready replicas, all offered work (including generator
drops), errors, query mix, per-pod CPU/queues and publish behavior. If the short
delivery gate fails, stop and identify the limiting stage. If it passes, run the
full burst and recovery, requiring natural return to three nodes and three
replicas of each workload. Do not force eviction to pass the recovery gate.
Staging can be cheaper later, but must be rematched for capacity acceptance.

## Observed result

The [saved result](evidence/coordinated-capacity-2026-10-28/summary.json)
and [raw driver output](evidence/coordinated-capacity-2026-10-28/coordinated-burst.json)
preserve the offered workload and limitations.

| Measurement | Three-minute burst | Two-minute recovery |
| --- | ---: | ---: |
| Offered reads/s | 4,500 | 100 |
| Scheduled / dropped reads | 810,000 / 7,418 | 12,000 / 0 |
| Attempted request failures | 0 | 0 |
| Page p95 | 316 ms | 3.7 ms |
| Search p95 | 835 ms | 26.9 ms |
| Updated-listing p95 | 664 ms | 9.6 ms |

The generator first completed 405,000 calibration requests without drops or
failures. During staging load its sixteen-vCPU VM averaged 17.8% CPU busy
(maximum 22.6%), with no observed network-device drops. This excludes neither
single-thread bottlenecks nor external network limitations.

The app's connection hold time was short: listing checkout averaged 12–16 ms
across replicas, with less than 0.1 ms after query completion. However, listing
connection acquisition still averaged 29–135 ms. Every 15-second pending-queue
sample was zero: those snapshots missed waits captured by the histograms.
Acquisition includes pool and scheduler delay, so this does not establish that
a larger pool would help. App CPU full-stall rates were approximately 8.7–15.5%
over sampled intervals inside the burst; DB quota throttling remained zero.

Placement also matters. The scheduler put apps 1/1/2/0 across four nodes; it did
not rebalance them when the fourth node arrived. The sole ingress on the DB
node used about 1.71 CPUs, versus 0.66–1.06 for the other controllers. That app
had the highest acquisition time and CPU stall rate. The previous four-app
test used hard one-app-per-node placement, and this test restarted PostgreSQL
before preparation, changing cache state. The apparent improvements are not
a controlled estimate of the CPU-request change alone.

Drops recur roughly every 30 seconds. Both the generator's idle-connection
rotation and ingress cache TTL are 30 seconds; current aggregate timing cannot
separate their contribution. The next investigation should distinguish these
effects and the ingress hot node using the captured profiles and counters,
before proposing another capacity change. Do not interpret either quiet
recovery or lower attempted-request latency as a passing delivery gate.

### Restoration

The [verified cleanup](evidence/coordinated-capacity-2026-10-28/cleanup.json)
restored the exact original app specification (two replicas, 250m CPU request,
original image and pools), ingress HPA specification, DB resources (2-CPU
request / 3-CPU limit), and bulk-ingress whitelist. The app HPA was removed.
The generator VM and disk were confirmed absent. All 48 sampled response hashes
matched; the corpus remained 52,366 names / 187,108 versions with no benchmark
publications. The DB restarted during resource restoration and was Ready.

At the final 20:21 UTC snapshot, staging still had four nodes and five
non-terminating ingress pods. Restored bounds allow 2–6 ingress replicas and
2–4 nodes; restoration does not prove this candidate returns naturally to two
nodes. No forced node deletion or long recovery test was performed.

## Why this candidate

The production review found CPU pressure in ingress, both apps and PostgreSQL
while both nodes were close to full CPU. Requests were approximately evenly
split across the apps. The 3-CPU DB quota was not exhausted: a limit is a ceiling,
not exclusive node capacity. Production requests 200m for PostgreSQL; the prior
staging tests requested 2 CPUs. This difference, and staging's candidate app
image, prevent treating staging as an exact control for today's production.

The previous four-app and DB-isolation tests still failed their offered-load
gates. This candidate changes CPU allocation across the entire request path,
without increasing node size or the maximum of four nodes. It does not treat
app replica count or dedicated DB placement alone as a proven fix.

| Setting | Production now | Candidate |
| --- | --- | --- |
| Nodes | 2 × e2-standard-4 | 2–4 × e2-standard-4 |
| App replicas | 2 | HPA 2–4 |
| App CPU request | 250m | 800m, no CPU limit |
| App HPA CPU target | None | 70% of request = 560m |
| Ingress replicas | 2 | HPA 2–6 |
| Ingress CPU request | 300m | 800m, no CPU limit |
| Ingress HPA CPU target | None | 70% of request = 560m |
| DB CPU request / limit | 200m / 3 CPUs | 1500m / 3 CPUs |
| App pool maximum / minimum | 60 / 10 each | 30 / 5 each |
| Total steady-state app pool maximum | 120 | 60–120 |

Both HPAs allow two added pods per minute, use a ten-minute scale-down
stabilization window, then remove at most one pod per two minutes. CPU targets
are candidates for testing, not measured safe throughput thresholds. CPU-only
HPA can miss runnable work that cannot obtain CPU; pressure, queues and actual
delivery remain pass/fail evidence. Node provisioning and load-balancer health
checks add further delay. The existing 60-second ingress connection lifetime
and per-pod ingress metric scraping remain part of the burst profile.

Placement uses the existing hard per-revision hostname spread with maxSkew 1.
Neither apps nor ingress are excluded from the DB node. A rollout may require
additional capacity. App rollouts use maxSurge 0 / maxUnavailable 1; terminating
pods can still overlap with replacements, so 120 is a steady-state pool budget,
not a hard global connection semaphore. Observe DB connections during rollouts.
Memory limits, DB buffers and max_connections=200 are unchanged.

## Two-node budget

The [original CPU packing calculation](evidence/coordinated-capacity-2026-10-28/cpu-budget.json)
uses the 15:51 UTC staging inventory. A fresh preflight rechecked system pod
requests, memory, volume locality, disruption budgets and autoscaler bounds.
Each node
has 3,920m allocatable CPU. Keeping the two singleton DB nodes, placing one app
and one ingress on each, and relocating the other movable workloads gives:

| Idle node | Requested CPU | Unallocated requests |
| --- | ---: | ---: |
| Registry DB node | 3,726m | 194m |
| Monitoring DB node (fresh preflight) | 2,653m | 1,267m |

With a 2-CPU DB request, the first placement would require 4,226m and would not
fit. The 1.5-CPU request is therefore a deliberate balance with the larger app
request, not an increase from the previous staging DB request. The DB can still
use up to its existing 3-CPU quota when CPU is available. It must demonstrate
acceptable latency and pressure under the benchmark.

The four-node peak packing fits four apps and six ingress controllers in CPU
requests. These are constructive request budgets, **not** a Kubernetes scheduler
simulation or proof of runtime headroom. Natural return to two nodes must be
observed, not inferred from the configured minimum. Fresh inventory confirmed
staging had returned to two nodes after the preceding experiment; it does not
validate this candidate's scale-down behavior. The DB node has a tight request
budget.

## Implementation guard

`mcp-registry:coordinatedCapacity` defaults to false and is not enabled in any
stack file. The program rejects it outside staging or without all of:

- `provider=gcp`, `nodeMachineType=e2-standard-4`, `burstAutoscaling=true`;
- `nodeMinCount=3`, `nodeMaxCount=4`;
- `registryCPURequest=800m`, `postgresCPURequest=1500m`, `postgresCPULimit=3`.

When enabled it registers the app HPA, gives it ownership of deployment replica
count, sets app pools to 30/5 and changes ingress's CPU target from 88% to 70%.
Use the prepared-listing candidate app image; current production 1.8.6 does not
contain these app changes. Do not merge/promote simply to test the opt-in.

## Bounded validation sequence

1. Reauthenticate and refresh all live staging state; back up the exact original
   specs privately. Verify the footprint and render/preview the proposed changes.
   Applying the DB request may restart the singleton DB; stage only while test
   traffic is stopped and spare schedulable capacity is available.
2. Use the previously calibrated sixteen-vCPU external generator, identical
   binary/catalog and request policy. Capture scheduler and request-phase timing;
   verify no calibration/delivery regression before interpreting staging drops.
3. Short gate with four Ready apps, six Ready ingress replicas and healthy LB
   backends: two minutes at 100 reads/s, three minutes at 4,500 reads/s, then two
   minutes at 100 reads/s. Keep the earlier 4,000 keys, 80% hot mix, 36 updated
   listings/s and three searches/s. Require zero drops, zero attempt failures,
   page p95 ≤1 second, healthy DB, and drained recovery queues.
4. If the short gate fails, capture the limiting stage, stop, restore original
   specs and delete the generator. Do not run the long test or tune multiple
   candidates during the same comparison.
5. Only if it passes: enable the intended 2–4 / 2–6 HPA bounds, let staging settle
   to two nodes and two replicas of each workload without forcing eviction, and
   then run the same ten-minute burst with one publish/s followed by thirty
   minutes of recovery. Record the rise from idle, pending pods, CPU/pressure,
   LB readiness, all offered requests and publishes, and natural scale-down.
   Require zero dropped work/failures, page p95 ≤1 second, publish p95 ≤2 seconds,
   drained queues and two-node recovery. Inability to settle to two nodes is
   itself a failed idle-footprint gate.

Persist origin and ingress counters separately, per-replica metrics, cache
outcomes from the benchmark response headers, and direct cAdvisor pressure
samples throughout. Ingress metric series must be nonempty; missing telemetry
is a collection failure, not zero load. Cache-outcome headers and direct pressure
captures are test evidence; this candidate does not yet install permanent
cache-outcome or CPU-pressure time series in production.

After the test, verify response equivalence, remove test publications and the
owned generator/disk, restore original app/ingress/DB settings and whitelist,
and record cleanup and actual node count. Do not claim a successful rollback
or two-node return from a successful patch command alone.

## Local checks

The Pulumi program builds, deployment-module tests pass, and repository
`make lint` passes. Mocked-resource tests check the HPA's target, scaling bounds,
CPU-only metric and recovery window; configuration tests reject production and
incomplete CPU budgets. A separate full deployment-module lint invocation also
reports existing module lint debt; new changes are checked separately against
the preceding commit. Kubernetes server dry-runs passed before the staging
changes. The live short test failed as reported above; a Pulumi apply was not
used for this temporary comparison.

HPA behavior follows the [Kubernetes v2 API](https://kubernetes.io/docs/reference/kubernetes-api/autoscaling/horizontal-pod-autoscaler-v2/)
and [GKE CPU utilization semantics](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/horizontalpodautoscaler).
