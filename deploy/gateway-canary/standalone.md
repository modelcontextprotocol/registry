# Standalone staging Gateway qualification

This is the staging completion gate before any production migration. Production
keeps its existing ingress and release flow. Staging uses OSS Gateway Fabric,
Vector, cert-manager, VictoriaMetrics and Grafana on the existing nodes.

## Final topology and recovery

`gatewayStagingStandalone: "true"` selects the final topology. Public TLS
Certificates are owned by Pulumi and renewed through native Gateway HTTP01
routes. cert-manager also manages the internal CA and 30-day server/client
certificates, renewing leaves ten days before expiry. The staging CA is a local,
self-signed trust root; its long-lived root lifecycle needs an explicit rotation
procedure, not an assumption that renewing the CA automatically reissues leaves.

The isolated canary Gateway/LB/address, ingress-nginx chart/controller, Ingress
objects, admission webhook, old solver and obsolete bootstrap resources are
removed before the main qualification run. The public Service and its namespace
retain their historical Kubernetes names to preserve Service UID, public IP and
GCP forwarding infrastructure. They are shared network resources adopted out of
the old chart, not an ingress-nginx runtime dependency. Stable metric names and
node-pool names are retained to preserve monitoring history and existing nodes.

There is no running legacy fallback. Recover from a reviewed Git revision using
the normal full-stack preview/saved-plan workflow. Git restores configuration,
not resource identity or deleted data: retain the protected public IP and active
TLS Secrets. Inspect Service adoption, certificate ownership and obsolete
resource recreation before applying a reverse migration. Do not promise an
instant selector rollback after removing the old controller.

Proxy rollouts use zero surge and replace one replica at a time. A 30-second
pre-stop delay covers endpoint propagation and the current GCP health check's
three 8-second failure intervals. The hook then gracefully quits NGINX and waits
for its workers before the image receives TERM; the total termination budget is
90 seconds. This follows Kubernetes' [NGINX lifecycle example](https://kubernetes.io/docs/tasks/configure-pod-container/attach-handler-lifecycle-event/)
and [NGINX signal semantics](https://nginx.org/en/docs/control.html). Recheck the
drain budget if the load-balancer health-check configuration changes. Terminating
pods can temporarily coexist with their replacement; the fixed node pool may
keep that replacement pending until the departing pod releases its reservation.

## Qualification matrix

Record actual results and any failed attempts; a healthy Pod is not a test result.
Run load scenarios sequentially with `REGISTRY_GATEWAY_TARGET=public`, so they
exercise the same data plane and policies as the public hostnames. The isolated
fixture has no database or publishing access. Its temporary hostname/listener,
Certificate, backend and generator must be removed after qualification.

| Scenario | Pass criteria |
| --- | --- |
| Certificates | Native ACME issuance and forced renewal for both public hostnames; existing trusted TLS remains valid; public Certificates have no Ingress owner. |
| Internal TLS | Renew both leaves, observe agent Secret propagation, and complete a fresh mTLS handshake with the renewed server/client credentials without restarting the controller. |
| Feature contract | Cache MISS/HIT/offload, stale serving, refresh/locking, private/error bypass, gzip/identity/Vary, request/query/forwarded-header behavior, redirects, body limits and source-IP limiting pass. |
| Normal release | Main push builds/deploys its exact image digest; authenticated publishing/read/delete and Grafana access work. |
| Steady load | 500 and 1,000 requests/s, zero generator drops or unexpected statuses/errors, p95 under 100ms and p99 under 250ms for the controlled fixture. |
| Mixed traffic | 250 requests/s with 20% unique misses; same error/latency gates, origin work measured. |
| Burst and scaling | Bounded 1,500 requests/s burst measured separately; HPA reaches four ready replicas at peak and returns to two after cooldown; no added nodes. Do not report generator-limited load as gateway capacity. |
| Proxy/controller replacement | Replace a proxy and restart the controller under load; measure client errors, recovery and public health. |
| One-node operation | Cordon/drain one Gateway node, test 500 requests/s on the remaining node, keep public probes running, and uncordon in a finally block. Public recovery within two minutes; record convergence errors. This is node evacuation/unavailability, not a powered-off VM experiment. |
| Collector recovery | Same-node restart restores checkpoints without historical replay, followed by successful load/metric reconciliation. No exactly-once crash guarantee is claimed. |
| Monitoring | All live scrape targets healthy; client/exporter/database request and histogram counts reconcile; CRI/retry timing contract passes; Grafana alert fires/resolves and reaches the temporary internal webhook. |
| Cleanup and soak | No legacy workload, duplicate Gateway/LB, fixtures, test issuers, temporary alerts or cordoned nodes; reviewed previews and a clean normal GitOps apply; observe the final configuration for unexplained errors, restarts, memory growth or monitoring gaps. |

No production apply is part of this work. Staging notification delivery is tested
through the previously approved temporary internal webhook; delivery to a real
person is a separate production gate. The earlier accepted staging interruption
budget does not establish a production availability target.
