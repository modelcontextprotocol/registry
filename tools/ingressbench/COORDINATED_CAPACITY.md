# Coordinated CPU capacity candidate — 2026-10-28

**Prepared locally; not applied or load-tested. Production is unchanged.**
Google Cloud interactive reauthentication blocked the initial live staging
inventory. No generator VM was created and no staging resources were changed.
The preceding short tests failed; their results are not a pass for this candidate.

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

The [saved CPU packing calculation](evidence/coordinated-capacity-2026-10-28/cpu-budget.json)
uses the 15:51 UTC staging inventory, including system pod requests. Each node
has 3,920m allocatable CPU. Keeping the two singleton DB nodes, placing one app
and one ingress on each, and relocating the other movable workloads gives:

| Idle node | Requested CPU | Unallocated requests |
| --- | ---: | ---: |
| Registry DB node | 3,726m | 194m |
| Monitoring DB node | 2,683m | 1,237m |

With a 2-CPU DB request, the first placement would require 4,226m and would not
fit. The 1.5-CPU request is therefore a deliberate balance with the larger app
request, not an increase from the previous staging DB request. The DB can still
use up to its existing 3-CPU quota when CPU is available. It must demonstrate
acceptable latency and pressure under the benchmark.

The four-node peak packing fits four apps and six ingress controllers in CPU
requests. These are constructive request budgets, **not** a Kubernetes scheduler
simulation or proof of runtime headroom. Refresh inventory and check memory,
volume locality, affinity, disruption budgets and scale-down eligibility before
applying. Natural return to two nodes must be observed, not inferred from the
configured minimum. The DB node has a tight request budget.

## Implementation guard

`mcp-registry:coordinatedCapacity` defaults to false and is not enabled in any
stack file. The program rejects it outside staging or without all of:

- `provider=gcp`, `nodeMachineType=e2-standard-4`, `burstAutoscaling=true`;
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
the preceding commit. No successful live preview or load test is claimed.

HPA behavior follows the [Kubernetes v2 API](https://kubernetes.io/docs/reference/kubernetes-api/autoscaling/horizontal-pod-autoscaler-v2/)
and [GKE CPU utilization semantics](https://docs.cloud.google.com/kubernetes-engine/docs/concepts/horizontalpodautoscaler).
