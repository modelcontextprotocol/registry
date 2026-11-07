# Pulumi staging canary qualification — 2026-11-06

The parallel OSS canary is serving the existing staging and Grafana hostnames at
its own IP. Public DNS and ingress-nginx remain unchanged. The production preview
contains no gateway changes, and no production apply was performed. The normal
release workflows and staging image selection are unchanged; local qualification
used the already released v1.8.8 digest through an environment override only.

## Feature and monitoring results

- Existing registry v0/v0.1 reads and Grafana login respond through the canary with
  valid public TLS. The fixture uses a separate, explicitly trusted test certificate.
- MISS/HIT behavior, 30-second expiry/background refresh, cache locking, original
  encoded/duplicate query keys, HEAD, conditional gzip and identity fallback pass.
  Private/Set-Cookie responses and HTTP 500/502/503/504 are not cached. A cached
  successful response remains available while its origin returns HTTP 503.
- The tests found and fixed two edge cases: add `Vary: Accept-Encoding` to identity
  responses too, and reject storage of error responses even when the upstream
  advertises cache freshness. HSTS is explicitly retained in locations that add
  cache headers. These fixes are scoped to the new canary policies.
- HTTP redirects preserve the path/query. The 1 MiB body limit is enforced.
  An invalid publish request reaches application validation (422); this is **not**
  an authenticated publishing test. Spoofed forwarding headers do not evade source-IP
  limiting: 1,920 bounded health requests returned 1,180 HTTP 200 and 740 HTTP 429.
- Vector request and all four client histogram counts match completed load requests
  exactly, with no parse or missing-size errors. Background cache refreshes are
  counted separately. VictoriaMetrics reconciles with the current exporters.
- Kubelet performed multiple 10 MiB log rotations during load without a counting
  discrepancy. A quiescent same-node Vector restart recovered in about 11 seconds
  without historical replay; the next 1,000 requests were counted correctly.
  This does not establish exactly-once delivery during crashes or node loss.
- The dedicated Grafana dashboard is provisioned and all nine panels return query
  results. Existing ingress and canary scrape targets are healthy. This verifies
  data availability, not browser OAuth or actual alert notification delivery.
- Graceful replacement of one gateway pod under 500 requests/second succeeded:
  the replacement became ready in about 37 seconds; all 45,000 requests returned
  HTTP 200 and matched the collected metrics.

## Matched load and resources

Both arms used two replicas, the same 74,660-byte public listing payload, 32 hot
keys, HTTPS/HTTP 1.1 and gzip. Their compressed first-response hashes match.
The generator ran on the original worker, outside the three-node canary pool.
The comparison had three Vector collectors, including one idle collector.

| Traffic | Requests per arm | ingress-nginx CPU | NGF + Vector CPU | CPU difference | p95 latency: old / new |
| --- | ---: | ---: | ---: | ---: | ---: |
| Cached, 500/s for 60s | 30,000 | 0.875 cores | 1.170 cores | +34% | 3.93 / 3.90 ms |
| Cached, 1,000/s for 120s | 120,000 | 1.877 cores | 2.172 cores | +16% | 15.28 / 15.97 ms |
| 20% distinct keys, 250/s for 60s | 15,000 | 0.533 cores | 0.657 cores | +23% | 5.18 / 4.46 ms |

Every matched run returned HTTP 200 for every offered request, with no transport
errors or generator drops. At 1,000/s, summed container working-set peaks were
about 122 MiB for ingress-nginx and 304 MiB for NGF/controller/three collectors.
This is a sum of per-container peaks, not a simultaneous node-memory peak.

CPU covers the named ingress components; it excludes the generator, fixture and
Kubernetes/GCP system agents. These are single-run observations, not statistical
confidence bounds. The first two baseline global origin counters included a few
concurrent fixture functional probes, so those origin counters are excluded from
the comparison. Generated request/cache counts remain exact. See
[validation.json](validation.json) for the recorded aggregates.

This establishes the controlled two-replica test target. It does not replay the
whole production fleet, payload/path mix or node-failure scenario, and does not
prove a final production node count. The separate canary incurs temporary node,
disk, IP and load-balancer costs while retained. Do not reduce reservations merely
to make it fit the original staging worker.

## Autoscaling and retained deployment

The temporary ingress-nginx baseline is removed after comparison. A fixture-only run exercised the normal 2–4 replica HPA: it reached four ready
replicas, then returned to two after the cooldown. The successful 240-second run
at 1,000 requests/second delivered all 240,000 requests, all HTTP 200, with exact
request/histogram reconciliation and no generator drops. The canary pool
consolidated from three nodes to two during this phase; four proxies became ready
on those two nodes.

The first scaling attempt used 128 load workers and dropped 172/240,000 scheduled
requests when its client queue filled; all 239,828 sent requests succeeded. It is
retained as a failed strict qualification attempt. The rerun used 256 workers.
During scaling its p95/p99 latency was 54.9/167.3 ms, p99.9 was 594 ms and maximum
latency was 1.69 seconds. Treat these transient tails as an input to production
SLO/headroom validation, not as the steady-state comparison above.

Cleanup completed through the saved full-stack Pulumi plan. The test fixture,
generator, baseline, fixture listener/routes and test certificate were removed;
the orphaned self-signed test Secret was then deleted. The retained state has two
canary nodes, two proxy pods, two Vector collectors and the 2–4 replica HPA. The
original staging worker and database node remain (four cluster nodes in total). The retained configuration enables
only `gatewayCanary`; neither test mode is enabled. The dedicated pool has a
minimum of two and maximum of three e2-standard-2 nodes.

## Remaining cutover gates

This is the original qualification snapshot. The later
[monitoring follow-up](monitoring-validation.md) closes the parser/timing and
internal notification-path checks in item 3, inventories actual dashboard/alert
dependencies, and caps the canary pool at two nodes. Real recipient delivery
remains pending.

1. Independently qualify Gateway ACME issuance/renewal and a safe IP/DNS handoff.
   Existing certificates currently renew through the retained ingress solver.
   Also adopt cert-manager for internal control/data-plane certificate rotation
   before production; the canary uses the upstream bootstrap certificate job.
   A new DNS record was not available; explicit-IP/SNI testing avoids needing one.
2. Complete browser Grafana OAuth and authenticated publishing through the canary.
3. Finish upstream retry-list timing and long/partial CRI record handling, and migrate actual dashboard/alert
   queries to the new metric contract; validate notification delivery.
4. Qualify production-shaped fleet capacity, node failure and rollout headroom.

The current ingress remains the active path and rollback option. Do not retire it
or describe the migration as production-ready based only on these canary results.


## Separate existing Grafana observation

Google OAuth initiation through the canary returns the correct Google redirect
and HTTPS callback URL. The temporary OAuth cookies omit `Secure` on both the
current ingress and the canary; this is an existing Grafana setting, preserved
by this change. Full browser login and cookie hardening are separate follow-ups.
