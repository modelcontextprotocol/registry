# Isolated database placement — 2026-10-29

The placement candidate is running in staging. This verifies rollout and
placement only; it has **not passed the representative burst/publish/recovery
acceptance test**. Production and public networking were not changed.

## Layout

| Workload | Placement and capacity |
| --- | --- |
| Registry PostgreSQL | One fixed e2-standard-4 node, CPU request/limit 3/3 |
| App | Worker nodes only, HPA 3–4, CPU request 800m, pool max/min 30/5 |
| Ingress | Worker nodes only, HPA 3–6, CPU request 800m, memory request 512Mi |
| Workers | e2-standard-4 pool scaling from two to three nodes |

The fixed node is labelled and tainted `registry-role=database:NoSchedule`;
PostgreSQL selects it and tolerates the taint. App and ingress select
`registry-role=worker`. Their hostname spread permits three idle replicas
across two workers, without requiring a separate node for every replica.
System agents still run on the database node. PostgreSQL remains a singleton;
this change does not add failover. Node upgrades can temporarily add surge nodes
beyond the steady-state maximum of four.

The live request inventory fits the constructed CPU budget: 3,341m on the DB
node; 3,245m/3,249m across two idle workers; and at most 3,616m per worker with
four apps, six ingress replicas and one additional ingress rollout pod.
Each node has 3,920m allocatable CPU. These are scheduling request sums, not
runtime headroom measurements. Several supporting workloads have no CPU
request. Actual consolidation and loaded CPU contention need observation.

## Rollout evidence

[Aggregate evidence](evidence/db-isolated-capacity-2026-10-29/summary.json)
records the current verification state. PostgreSQL became Ready on its reserved
node. All three app and three ingress replicas became Ready on worker nodes,
and no other business workload was placed on the database node. Both HPAs
reported healthy CPU metrics. The worker autoscaler accepted bounds 2–3.

All 48 saved response fixtures matched their reference hashes. The corpus
remained 52,366 names / 187,108 versions with no invalid indexes. Of 84 external
health probes during the rollout, one observed connection refused at
12:56:47 UTC. This was not an interruption-free rollout; the precise source of
that failed probe was not isolated. Node/DB/app and ingress rollouts were not
performance experiments.

At 13:12:29 UTC, the autoscaler had naturally reduced the cluster to three Ready
nodes: one database node and two workers. All three apps and three ingress
replicas were Ready, placed 2 apps + 1 ingress on one worker and 1 app + 2 ingress
on the other. No forced eviction or resize was used. All 20 health samples during
the idle observation succeeded. This demonstrates idle consolidation after
rollout; it is not a successful post-burst recovery test.

## State adoption and remaining gate

The new node pool and app HPA were created directly for the staging candidate.
Local Pulumi import is blocked by the unavailable stack passphrase. The staging
GitHub Actions job now prepares their adoption using the existing staging
secret, before its normal refreshed apply. The script checks exact stack,
provider and resource identities, skips completed imports and rejects conflicting
ownership. The database pool remains protected from deletion. This adoption
has been tested against the exported state but has not yet run in CI.

Before promotion, use the preceding observed traffic to establish cache state,
then replay the selected peak with corpus/rate-limit mismatches resolved, as
specified in [the production trace comparison](WORKLOAD_COMPARISON.md). Include
publish traffic and recovery, count all offered requests and generator drops,
and verify CPU pressure, connection waits, per-replica sharing and natural
return to one database plus two worker nodes. Keep cold-cache startup as a
separate resilience test. Do not increase the database pool on this placement
evidence alone.
