# Production Gateway migration completion — 2026-11-09

Production now uses OSS NGINX Gateway Fabric, Gateway API, Vector, cert-manager,
VictoriaMetrics and Grafana. The final configuration was merged in
[PR #1829](https://github.com/modelcontextprotocol/registry/pull/1829), after
endpoint adoption in [#1827](https://github.com/modelcontextprotocol/registry/pull/1827)
and certificate preparation in [#1828](https://github.com/modelcontextprotocol/registry/pull/1828).
The [production deployment](https://github.com/modelcontextprotocol/registry/actions/runs/42600083122)
succeeded. Authenticated operations and human notification receipt were checked
subsequently on the same day.

## Retained operating contract

- Both registry aliases and Grafana use the existing public IP `34.61.200.254`
  and `ingress-nginx/ingress-nginx-controller` Service. Its UID, node ports,
  health-check port and `externalTrafficPolicy: Local` were preserved.
- Public Certificates and trusted Secret contents were retained, protected in
  Pulumi, and detached from the removed Ingress owners. cert-manager now uses
  native Gateway HTTP-01 renewal. Internal Gateway TLS is cert-manager managed.
- Production retains 2–7 e2-standard-4 workers and one isolated database node;
  application HPA 2–4 and Gateway HPA 3–16. Ordinary autoscaling may change the
  instantaneous node count. The production application remains version 1.8.8
  at completion; later releases use the existing release process.
- Staging remains two e2-standard-2 VMs with one shared worker and one isolated
  registry database node. Its compact singleton profile is for functional
  validation, not a production capacity or availability recommendation.
- Active Service/namespace, Pulumi resource, certificate and metric names keep
  historical `ingress-nginx` / `gateway-canary` wording. These are not orphaned
  resources. Removing or renaming them would risk endpoint or monitoring continuity.
- Main pushes still build and deploy staging's exact image digest. Production
  still deploys an explicitly committed release image tag. Environment-scoped
  credentials, main-only applies and full-stack saved plans are unchanged.

## Observed interruption

Public probes first failed at 10:24:11 UTC and last failed at 10:29:19 UTC;
all probes were healthy again at 10:29:24 UTC. The observed window was about
5 minutes 13 seconds, followed by more than 12 minutes of continuous healthy checks.
This is an observation, not a downtime guarantee for future changes.

The old ingress reservations initially blocked Gateway/Vector scheduling. Scaling
that controller down released capacity. Its remaining admission webhook then
blocked Ingress deletion; the already-planned webhook removal and UID-guarded
foreground finalizer cleanup completed retirement after verifying certificate
owners were detached. These interventions are recorded in the cutover PR.

## Validation evidence

| Check | Observed result |
| --- | --- |
| Public connectivity | Both aliases, API v0/v0.1, HTTP redirects, public TLS, health/listing endpoints and Grafana passed |
| Caching | MISS/HIT and origin offload confirmed on all three production proxies; query/alias behavior, gzip/Vary and uncached health verified |
| Rate limiting | Isolated-source bulk/general tests produced expected 200/429 responses, independent buckets and shared alias/API buckets per proxy; no other statuses in the accepted in-cluster run |
| Certificates | Disposable Let's Encrypt staging issuance and renewal passed for all three public hostnames via native Gateway HTTP-01; original public certificates remained unchanged; fixtures removed |
| Monitoring | Gateway/Vector scrapes, dashboard queries, existing alert queries and parser/error/discard counters checked; four existing rules and notification configuration preserved |
| Publishing | Fresh GitHub device login with `io.github.rdimitrov/*` permission; CLI published a temporary remote MCP server; reads passed on both aliases and API versions; invalid token rejected with 401, wrong namespace with 403, missing required Authorization header with 422 |
| Publishing cleanup | Test version soft-deleted, absent from normal reads/listings on both aliases/API versions; historical deleted metadata remains; working MCP fixture and test CLI token removed |
| Grafana OAuth | Fresh Google login/callback and authenticated live dashboard queries succeeded |
| Actual Discord delivery | Synthetic fixture log traversed production Vector → VictoriaMetrics → Grafana → the existing Registry Dev Discord contact; operator confirmed receipt of both firing and resolved messages |
| Alert cleanup | Temporary rule recoverably deleted; four original rules remained normal; original rule/contact configuration hashes matched; temporary emitter removed |
| Infrastructure cleanup | Neither hosted cluster or Pulumi checkpoint retains legacy Ingress/IngressClass/chart resources or test fixtures; no pending stack operations; one public load balancer per environment; no unused reserved IPs |

Earlier failed port-forward rate-test attempts were not counted as passing tests.
Raw plans, state, credentials and private diagnostic logs are not committed.
The old production dump volume and backup retention were not changed.

## Scope and recovery

These checks complete functional migration validation. They do **not** establish
maximum production throughput, exactly-once log delivery under node loss, or a
zero-downtime recovery guarantee. The earlier load/rehearsal limitations remain
recorded in [staging validation](standalone-validation.md) and
[the production migration runbook](production-migration.md#capacity-and-risk).

There is no running ingress-nginx fallback. Follow the separately reviewed
[Git-based recovery procedure](production-migration.md#rollback), retaining public
endpoint/certificate identities and production capacity. Do not clear the
production phase or delete the historically named active Service as cleanup.
