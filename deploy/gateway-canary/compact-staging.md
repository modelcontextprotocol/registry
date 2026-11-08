# Two-node staging and production qualification

`gatewayStagingCompact=true` keeps everyday staging on two `e2-standard-2` VMs: one isolated registry database node and one shared worker for the registry application, monitoring, Gateway Fabric and Vector. The dedicated Gateway node pool is removed. This replaces the four-node validation topology recorded in `standalone-validation.md`; that report remains historical evidence, not the current capacity configuration.

The worker pool has minimum=maximum=1; the database pool contains one node. Both use zero-surge node upgrades, so planned upgrades can interrupt staging rather than provision an extra VM. Application and Gateway HPAs are fixed at one replica. The Gateway PDB allows singleton eviction. A worker failure interrupts the application, public Gateway and monitoring; this profile is intentionally not highly available.

CPU requests are 100m for the proxy, 100m for Vector, 50m for its controller and 100m for VictoriaMetrics. The proxy and Vector have no CPU limit and can consume spare CPU. Their memory limits, cache configuration, access-log fields, metric extraction, dashboards, certificate management and retention remain unchanged. Local rate limits remain 600 requests/minute per client for bulk reads and 180/minute for other API routes, with their existing bursts; one proxy removes the multiplied aggregate allowance of the former four proxies. These are functional limits, not a load-test bypass.

The normal staging main-push build/deploy and explicit production release flow remain unchanged. Application image pinning is still resolved by the deployment workflow, not stored as an experimental staging override.

## Validation on everyday staging

Before applying, inspect both environment previews, the worker's CPU/memory/ephemeral-storage reservations and retained public Service/Certificate identities. After applying, verify two Ready nodes, no dedicated Gateway pool, one ready application/proxy/controller/collector, four current healthy Gateway/Vector scrape targets, the same public IP/Service and trusted Certificates, and no pending workload. Repeat authenticated publishing/read/status-deletion, public HTTP-to-HTTPS and TLS checks, cache/query/gzip/header checks, metrics reconciliation and Grafana health. Run bounded public HTTPS traffic below the source-IP rate limit from an external client; verify expected rate limiting separately. Observe resource growth and restarts, and verify another normal deployment without extra VMs.

A short observation is not an overnight soak. An unattended load run must have an explicit end time, cleanup, and measured results; do not leave a background generator running indefinitely.

## Separate production qualification

1. Read production traffic/resource telemetry without changing production. Record observation windows, sample resolution and gaps. Establish representative route/method/status mix, response sizes and cache behavior. Current ingress metrics do not directly expose cache hit/miss counts, so do not infer cache ratios from route names or CPU alone.
2. Reproduce the earlier unexplained rollout transport errors with per-request error type, timestamp, connection reuse and TLS details. Include connection establishment and long-lived clients.
3. Prepare a proposed production topology and a time-boxed staging expansion with an explicit incremental cost estimate and teardown plan. Obtain approval before provisioning additional capacity. Do not treat two-node staging or the old fixture-only tests as production sizing evidence.
4. Drive representative requests through the public HTTPS/load-balancer path from clients that respect realistic source-IP distribution. Exercise warm/cold/stale cache, compressed and uncompressed larger responses, misses, authenticated operations, deployments, collector interruption and abrupt node loss. Validate the final parser under the full load and account for generator bottlenecks separately.
5. Define pass criteria before the run: traffic target based on measured demand plus agreed headroom; no unexplained transport/status errors; latency target; resource headroom; complete metric reconciliation; and an agreed failure-recovery budget. The earlier strict fixture gates remain recorded as failures rather than being relabeled.
6. Verify alert firing and resolution at a real destination and perform longer observation before a production migration decision. Restore the two-node staging profile and verify billing resources are removed after temporary qualification.

Production remains unchanged until a separate migration is authorized and qualified.
