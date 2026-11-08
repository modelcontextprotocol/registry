# Standalone staging qualification — 2026-11-08

Staging uses OSS NGINX Gateway Fabric, Vector, cert-manager, VictoriaMetrics and Grafana. The legacy ingress and separate canary have been retired. Production has not been migrated. **This is a staging validation record, not approval for a production cutover.**

[Sanitized JSON evidence](standalone-validation.json) includes earlier failed attempts.

## Configuration and cost

Four proxy replicas, each with one NGINX worker, run on the same two existing Gateway nodes. The cluster retains its original four nodes. Staging fixes the HPA minimum and maximum at four: a measured 4-to-2 cooldown left both surviving proxies on one node and interrupted public TLS. Keeping four avoids that scale-down transition without adding a scheduler component or more VMs. Compared with two proxies it reserves another 1 CPU and 512 MiB, within the existing nodes. Per-replica limits are unchanged; the aggregate rate-limit budget now starts at the prior four-replica maximum.

## Controlled load results

These bounded fixture tests use the public Gateway’s internal Service, with separate internet HTTPS health probes. They are not a production traffic replay or a full internet load-balancer capacity test. Gates remain zero transport/status errors, zero generator drops, p95 below 100 ms and p99 below 250 ms. Failed attempts remain in the accompanying JSON.

| Scenario | Offered | HTTP 200 | Client errors | Generator drops | p95 / p99 ms | Load gate |
| --- | ---: | ---: | ---: | ---: | ---: | --- |
| Fixed four replicas, 1,000/s | 240,000 | 240,000 | 0 | 0 | 41.3 / 155.9 | Pass |
| Steady 1,000/s | 120,000 | 120,000 | 0 | 0 | 107.8 / 199.7 | Fail |
| Rolling update 1,000/s | 600,000 | 599,998 | 2 | 0 | 149.0 / 246.8 | Fail |
| Burst 1,500/s | 90,000 | 76,866 | 0 | 13134 | 310.1 / 406.1 | Fail |
| 20% misses, 250/s | 30,000 | 30,000 | 0 | 0 | 10.6 / 38.9 | Pass |
| Steady 500/s | 60,000 | 60,000 | 0 | 0 | 8.0 / 18.6 | Pass |
| Proxy replacement 500/s | 90,000 | 90,000 | 0 | 0 | 4.4 / 13.3 | Pass |
| Controller replacement 500/s | 90,000 | 90,000 | 0 | 0 | 4.4 / 10.0 | Pass |
| One-node operation 500/s | 150,000 | 150,000 | 0 | 0 | 20.7 / 65.7 | Pass |
| After collector restart, 500/s | 30,000 | 30,000 | 0 | 0 | 5.4 / 13.8 | Pass |

Sampled steady-run CPU (cores, summed across current Pods/nodes):

| Offered rate | Proxy CPU | Vector CPU | Gateway node CPU, including system work |
| --- | ---: | ---: | ---: |
| 500/s | 0.90 | 0.59 | 2.13 |
| 1,000/s | 1.67 | 0.83 | 3.44 |
| 1,500/s burst | 2.22 | 0.84 | 3.93 |

At 1,000 requests/s the four-minute run passed, but a subsequent two-minute run missed the p95 gate. The ten-minute rolling update had two transport failures out of 600,000 offered requests and also missed p95. The driver records only aggregate transport failures, so those two failures have not been attributed to a specific TLS/connection error. The 1,500/s burst saturated the Gateway nodes and dropped offered work in the generator queue. These results do not establish reliable 1,000/s capacity during all lifecycle events or qualify the 1,500/s target.

The driver uses 256 concurrent workers and `GOMAXPROCS=1` under its existing one-CPU limit. Independent resource samples, not generator queue drops alone, show the node CPU constraint. Steady-run resource totals are in the JSON; do not sum different replacement Pod lifetimes as simultaneous CPU consumption.

## Recovery results

- rollout: overall passed=False; public failed checks=0; maximum public recovery=0.0s; cleanup errors=0; original node identities preserved=True.
- proxy: overall passed=True; public failed checks=0; maximum public recovery=0.0s; cleanup errors=0; original node identities preserved=True.
- controller: overall passed=True; public failed checks=0; maximum public recovery=0.0s; cleanup errors=0; original node identities preserved=True.
- node: overall passed=True; public failed checks=4; maximum public recovery=16.0s; cleanup errors=0; original node identities preserved=True.

The first rollout before graceful draining produced 45 transport errors out of 240,000 offered requests. The deployed hook waits 30 seconds for endpoint/LB propagation, gracefully quits NGINX and allows 90 seconds for termination. The final rollout above includes the update and restoration under active traffic; its healthy public probes do not override the two fixture transport failures.

Node recovery is a cordon/drain and uncordon test, including at least 60 seconds of actual traffic with one Gateway node evacuated. It does not simulate an abrupt VM power loss. Controller replacement and internal mTLS renewal are separate checks.

## Feature and monitoring evidence

Separate evidence covers cache MISS/HIT/offload, stale serving and refresh/locking, private/error bypass, gzip/identity/Vary, request/query/forwarded-header behavior, source-IP rate limits, HTTP redirects, body limits, native ACME issuance and forced renewal for both public hostnames, authenticated publishing/read/status-deletion, Grafana panels and internal mTLS leaf rotation without controller restart. Public ACME uses temporary Let’s Encrypt staging-CA Certificates; the active publicly trusted Certificates and TLS material stay unchanged.

Load logs exposed an empty-string upstream timing format on cache hits. Vector previously counted those as malformed timings. The correction was applied after the load suite and separately checked locally, through staging log emitters and with public cached API reads. The parser now treats both empty strings and dashes as missing measurements, without inventing zero-duration samples; malformed numeric values still increment the error metric. Local and post-deployment staging corpus checks cover both forms, retry groups, CRI fragmentation and the error counters. Counts and histograms reconcile with VictoriaMetrics. The alert proof covers firing and resolved deliveries to a temporary internal webhook; it does not prove delivery to a person.

## Cleanup and operational limits

The protected public Service UID, IP, node ports and public Certificates are preserved. Historical namespace, Service, node-pool and metric names retain resource identity and monitoring history; they do not imply a running legacy controller. Reversion requires a reviewed Git-based redeployment, not an instant selector rollback.

A temporary general-worker autoscaler expansion occurred earlier during app replacement while fixtures held CPU reservations. The extra worker hosted only system DaemonSets and was automatically removed. The final load/recovery suite enforced the original four-node inventory. Test workloads are removed before the final full-stack deployment to avoid repeating that reservation overlap.

The evidence retains failed and discarded attempts, including the earlier downscale interruption and a short TLS reset immediately after new proxies became ready; the latter’s exact cause was not established. No overlapping measurement is counted as valid qualification.

Two full-stack applies stopped because the typed Certificate lookup omitted an explicit namespace and read the Grafana Certificate from the default namespace; both live public Certificates remained present and ownerless. The lookup now supplies namespace and name, distinguishes unavailable metadata, and orders the managed update after the read. The retried full saved-plan apply and final cleanup are recorded in the accompanying JSON. Normal GitOps deployment and post-merge observation are verified after merging; this pre-merge report does not claim they have already completed. Longer staging observation, investigation of residual rollout errors, a capacity plan tested against representative production traffic, and a real production alert destination remain promotion gates. The current staging tests do not determine production node count or cost.
