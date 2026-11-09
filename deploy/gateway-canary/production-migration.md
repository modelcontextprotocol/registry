# Production Gateway migration

**Completed on 2026-11-09.** Production now has `gatewayProductionPhase: standalone`.
See [the completion record](production-completion.md) for actual interruption,
feature checks, authenticated publishing, OAuth and Discord notification delivery.
The preparation/cutover sections below document the historical sequence, not
pending work. Keep the current phase and endpoint identity configuration; clearing
the phase does not roll back safely. The [rollback procedure](#rollback) remains
available for a separately reviewed recovery.

The [production parity review](production-parity.md) is the behavioral baseline,
including corrections and explicit differences. Staging validates those settings;
its reduced capacity is not a production sizing recommendation.

## Connectivity and features

| Contract | Production configuration |
| --- | --- |
| Public endpoint | Existing `ingress-nginx/ingress-nginx-controller` Service, UID `e9471fb4-c8a5-4951-9eb0-8e440b30c5d8`, IP `34.61.200.254` |
| Network ports | Read from that Service: HTTP `31935`, HTTPS `30874`, health check `32421`; preserve `externalTrafficPolicy: Local` |
| Registry aliases | `registry.modelcontextprotocol.io` and `prod.registry.modelcontextprotocol.io`; both use `default/mcp-registry:80` |
| API routing | Exact `/v0/servers` and `/v0.1/servers` bulk routes plus the registry catch-all route |
| Grafana | `grafana.prod.registry.modelcontextprotocol.io` → `monitoring/grafana:80` |
| TLS | Adopt existing public Certificates/Secrets; both registry names remain on `mcp-registry-prod-tls`; Grafana keeps `grafana-tls` |
| Renewal | Native Gateway HTTP-01 solver after retirement; HTTP listener accepts challenge routes from `default` and `monitoring` |
| Policies | Preserve production's contract using the shared policies validated in staging; review the explicit cache/Vary and implementation differences |
| Monitoring | Vector, VictoriaMetrics and Grafana; production Grafana hostname classified separately; existing alert configuration retained |

No DNS change or second public load balancer is required. Historical namespace,
Service and metric names remain to preserve identities and dashboards. The final
topology contains no ingress-nginx controller, chart, Ingress or legacy ACME proxy.

Tests check both aliases, listener/Secret references, shared policy content,
production placement and port preservation. These establish the intended
configuration; public connectivity and feature behavior must also be checked on
the deployed Gateway.

## Preparation checks

Initial preparation was checked without applying production changes. The later
staging parity work and behavioral corrections are recorded separately above:

- Go tests and vet for the Kubernetes/GCP packages passed; deployment build passed.
- All 78 deployment-script tests passed, including fail-closed identity checks
  and validation of both Certificates before either owner is patched.
- Normal staging preview passed: four metadata/RBAC normalization updates,
  no image, workload, node-pool or Gateway changes.
- Normal production preview passed: two TTL-cleaned admission Jobs recreated
  and two Grafana RBAC normalization updates; no migration activated.
- The production `adopt` preview passed: three imports (existing address and
  Certificates), no deletions or replacements. The Service selector stays legacy.
- The exploratory `standalone` preview now plans an in-place public Service
  update. It is intentionally unsuccessful because the live Certificates still
  have Ingress owners. It is **not** a complete approved cutover plan; repeat it
  after the managed preparation and ownership detachment.
- All 11 public baseline probes passed against the existing production ingress.
  They cover all three names, redirects, both API versions and health endpoints.

Two issues caught during preview were corrected: retaining the GCP address's
immutable description, and omitting API-assigned cluster IP fields from new
Service inputs so Pulumi does not propose replacing the existing endpoint.
Production Grafana log classification was also corrected from the staging name.
Raw preview events and plans remain private; they are not committed.

## Capacity and risk

Keep the existing production pool configuration: 2–7 `e2-standard-4` workers and
the isolated database node. Do not copy staging's compact profile or two-node cap.
Gateway proxies request 800m CPU/512Mi memory each, with an HPA of 3–16 replicas
at 70% CPU. Vector runs on workers. These settings preserve the production node
limits, not its instantaneous node count or spending: the autoscaler may add
workers within those existing limits.

Rollouts use no surge, at most one unavailable proxy, graceful draining and a
90-second termination grace period. This is not evidence of zero downtime.
Full retirement can interrupt connections while workloads and GCP health checks
converge; cache warm-up also increases backend work. The earlier staging handoff
had about 41 seconds of interruption. That is an observation, not a production
downtime bound. Keep a maintenance window and a rollback operator available.

Existing [staging qualification](standalone-validation.md) includes failures and
scope limits. The later production-scale rehearsal did not reach the Gateway
phase because its baseline saturated the test environment. Consequently this
implementation is not a passed production-peak capacity qualification. Real
Discord firing and resolved delivery was subsequently confirmed in the
[completion record](production-completion.md); production-peak capacity remains unqualified.

## Preparation and cutover

Use the normal `make -C deploy prod-preview` / `prod-up` saved-plan workflow.
Review all PR comments, including Copilot, and merge each configuration phase
before its deployment. Do not deploy a later phase against an earlier checkpoint.
The code-only PR deliberately does not enable any production phase.

Set these nonsecret values in `deploy/Pulumi.gcpProd.yaml` only after rechecking
the live endpoint. Keep encrypted credentials unchanged:

```yaml
mcp-registry:gatewayProductionPublicIP: 34.61.200.254
mcp-registry:gatewayProductionServiceUID: e9471fb4-c8a5-4951-9eb0-8e440b30c5d8
mcp-registry:gatewayProductionAddressName: ae9471fb4c8a549519eb08e440b30c5d
mcp-registry:gatewayProductionPhase: adopt
```

1. **Adopt.** Preview and apply imports of the existing address reservation and
   both public Certificates with deletion protection. The Service receives its
   existing reserved IP explicitly; its selector remains legacy. Preserve the
   reservation's immutable description and network tier. Reject any replacement
   of the Service, address, certificates, cluster, nodes, application or database.
2. **Managed.** Change the phase to `managed`, preview and apply. This disables
   certificate creation by the old Ingress annotations while retaining the old
   controller and HTTP-01 solver. Verify both Certificates are protected in
   Pulumi and remain Ready with the same Secret contents.
3. **Detach certificate ownership.** From `deploy/`, with the production backend
   and passphrase selected, run
   `python3 scripts/detach_production_gateway_certificates.py --output /tmp/prod-cert-review.json`.
   Review the result, then rerun with `--apply` and a separate output path.
   The helper requires the managed phase and protected state, verifies the old
   Ingress UID and disabled shim, and uses JSON Patch preconditions. It checks
   public certificate bytes and never reads private keys. Validate both owners
   are absent before proceeding. Empty SSA owner lists alone are insufficient.
4. **Final preview.** Change the phase to `standalone`. Read the entire refreshed
   preview. It must update the existing Service in place, import nothing new,
   retain public Certificates/Secrets/IP and production capacity, create the
   Gateway/Vector/native certificate resources, and retire legacy ingress
   resources. Stop on unexpected app/image/database/node changes or replacement
   of a protected endpoint. Attached Certificate owners intentionally fail this
   preview. A preview made before the adoption steps is not a cutover plan.
5. **Cutover.** Apply that exact reviewed saved plan during the migration window.
   Watch the public checks continuously. Confirm Gateway/HTTPRoute `Accepted`,
   `Programmed`/`ResolvedRefs` as applicable, healthy endpoints on multiple worker
   nodes, Ready Certificates and proxy/Vector health. Inspect load-balancer health
   and logs if connectivity fails; do not delete the public Service to recover.

After each phase, run `python3 deploy/scripts/probe_production_gateway.py`.
It checks normal DNS, verified public TLS, both API versions, health, redirects
and Grafana login/health across all three names at low volume. It does not prove
authenticated operations or cache/rate-limit semantics.

Before declaring migration complete, also validate cache MISS/HIT and backend
offload, gzip/Vary, publishing/read/delete with a controlled test namespace,
Grafana OAuth, source-IP throttling using an isolated source, native ACME renewal,
Vector parsing/error counters, metric reconciliation, dashboards and the real
notification destination. Reuse the bounded staging scenarios; do not run a
rate-limit flood or destructive fixture against ordinary production clients.

## Rollback

Full retirement removes the old runtime. Git history is configuration recovery,
not an instant selector rollback. Prepare and preview a return to the `managed`
phase on the migration-capable code, which recreates ingress-nginx and reverses
the Service alias while retaining the imported IP/Certificates. Keep all four
production configuration values. Never simply clear the phase: that would remove
protected resources from the desired graph. Keep Certificates detached and
managed; restore the legacy HTTP-01 solver. Expect an interruption while the old
controller becomes ready and caches warm. A rollback must itself pass the saved
plan review and the public/feature checks.
