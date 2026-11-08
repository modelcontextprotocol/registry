# Staging Gateway Fabric

Staging now selects `gatewayStagingStandalone: "true"`. Its public hostnames use
OSS Gateway Fabric, with no running ingress-nginx or duplicate canary. See
[standalone.md](standalone.md) for the final topology, qualification gates and
Git-based recovery procedure. Production retains its existing ingress. Normal
staging pushes continue to deploy the current main image. The completed
[standalone validation](standalone-validation.md) records both passing checks
and capacity limits; it does not approve production migration.

The `gateway-canary` paths, metric prefixes and node-pool names are retained to
preserve monitoring history and infrastructure identity. Earlier comparison and
handoff reports below describe historical configurations, not the current one.

The Gateway uses OSS NGF 2.8.1 / Gateway API 1.6.3 / Vector 0.61.0. Controller,
data-plane, Vector and fixture images are pinned by digest. NGINX Plus and product
telemetry are disabled. Metrics go to the existing vmagent, VictoriaMetrics and
Grafana, under separate `gateway-canary-vector-*` jobs and
`registry_gateway_canary_*` series. Existing dashboards and alerts keep their
current data sources while mappings are qualified. The full event stream is
processed locally; GCP retains 10% of fast successful listing logs after metric
extraction.

## Capacity and temporary fixtures

The existing two e2-standard-2 Gateway nodes remain capped at two; no additional
nodes are required by this migration. Standalone staging keeps four proxy
replicas (two per node) to avoid the observed scale-down placement failure.
Each Gateway node runs one single-threaded Vector collector requesting 500m CPU;
each proxy requests 500m and the controller requests 100m. The worker and database
pools remain unchanged. Removing the duplicate canary releases its separate IP
and L4 load balancer. Node upgrades may temporarily surge under the pool's existing
upgrade policy; that is separate from workload autoscaling.

The original public IP and Service are preserved. Public certificates are managed
by Pulumi/cert-manager with native Gateway HTTP01 renewal. Internal server/client
TLS certificates are also managed by cert-manager; the old bootstrap Job is gone.

`gatewayCanaryFixtures: "true"` temporarily enables an isolated backend and load
generator on the existing worker, with a test listener on the public Gateway.
Remove this flag after qualification. `gatewayCanaryComparison` is rejected in
standalone mode; historical ingress comparisons are recorded in the reports.

`fixture.gateway-canary.invalid` is a reserved test hostname, without DNS. The
fixture has its own cert-manager self-signed test certificate. Supply its public
certificate to the checker with `--fixture-ca`; hostname and TLS chain checks
remain enabled. Use Python 3.13/OpenSSL for the checker; the older macOS
Python/LibreSSL client rejected the test trust anchor. No private key is read or exported. The fixture has no registry/database connection, and its
load routes deliberately have no rate limiter. Real registry routes preserve
600/180 requests/minute and 1800/540 bursts per source IP per replica. Never use
the unthrottled fixture route as the application route.

## Resource ownership

`pkg/k8s/gatewaycanary/assets/controller.json` is the reviewed upstream Helm
render using `ngf-values.json`. Standalone deployment replaces its bootstrap
certificate hook with cert-manager resources. Pulumi owns concrete resources;
there is no custom NGINX template or controller fork. Public Certificates are
protected, have no legacy Ingress owner, and retain their existing trusted Secrets.

`crds.json` contains Gateway API's standard installation plus the pinned NGF
chart CRDs. `routes.json` is the canary's native Gateway/NGF policy configuration.
`fixture.json` embeds the isolated `fixture.py` server. `vector.json` preserves
the tested histogram buckets and metric labels, with a JSON log parser. Checkpoint
state survives collector pod replacement on the same node; node loss still loses
its local log/checkpoint files, and no exactly-once crash guarantee is claimed.

Upstream references:
- https://github.com/nginx/nginx-gateway-fabric/tree/v2.8.1/charts/nginx-gateway-fabric
- https://github.com/kubernetes-sigs/gateway-api/releases/tag/v1.6.3
- https://vector.dev/docs/reference/configuration/sources/file/

NGF and Gateway API are Apache-2.0; NGINX OSS is BSD-2-Clause and Vector is MPL-2.0.
These core licenses do not constitute a complete transitive image/SBOM audit.
`assets-provenance.json` records the chart digest and vendored asset hashes.

## Recovery and release flow

There is no live legacy fallback. Use the reviewed Git recovery procedure in
[standalone.md](standalone.md), preserving the protected public IP and certificates.
No image pin or workflow dispatch mode is introduced. A normal main push builds
and deploys its exact image digest. Temporary local qualification may use
`REGISTRY_STAGING_IMAGE` without persisting a staging pin.

## Monitoring follow-up

See [monitoring.md](monitoring.md) for CRI reconstruction, retry timing semantics,
query mappings and the local/staging validation commands. The completed follow-up
results are in [monitoring-validation.md](monitoring-validation.md).

## Qualification status

See [validation.md](validation.md) and [validation.json](validation.json) for the
observed results and remaining cutover gates. Source rendering, mocks, a successful apply or one healthy HTTP request
are not proof of feature or capacity parity. Production-shaped load, real IP
limits, log rotation/restarts, collector freshness, HPA/rollout/failure behavior,
TLS renewal, Grafana OAuth, dashboard/alert mappings and notification delivery
must each have separate evidence or be marked pending.


## Repeating qualification

Use only the staging context. Temporarily enable `gatewayCanaryFixtures`, build
`deploy/infra`, preview the full stack and apply its saved plan. Build `load.go`
for Linux amd64 and copy it to `/work/loadgen` in the `registry-gateway-canary`
namespace's `load-generator` Pod. Export **only** `ingress-nginx/fixture-tls`'s
`tls.crt` to a local file and `/work/fixture.crt` in the generator.

From `deploy/`, run scenarios sequentially:

```sh
export REGISTRY_GATEWAY_TARGET=public
kubectl --context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging \
  -n ingress-nginx rollout status deployment/registry-edge-registry-gateway-canary --timeout=360s
python3.13 gateway-canary/check.py --ip 35.222.36.75 --fixture-ca /tmp/fixture.crt \
  --output /tmp/gateway-contract.json
python3 gateway-canary/measure.py --arm ngf --rate 1000 --seconds 120 \
  --epoch qualification --output /tmp/gateway-load
python3 gateway-canary/autoscale.py --output /tmp/gateway-autoscale
python3 gateway-canary/resilience.py --scenario proxy --output /tmp/gateway-proxy
```

Use `--miss-percent 20 --rate 250` for mixed traffic. Do not run other fixture
probes during measurement. The driver discovers the public Gateway's internal
Service IP and retains hostname/TLS verification. This measures the active data
plane with a controlled fixture, not a production traffic replay or internet
capacity benchmark. Public HTTPS probes run separately during every scenario.

`limits.py` verifies real source-IP limiting; use `--replicas 4` for standalone staging.
`autoscale.py` measures fixed four-replica capacity in standalone mode; the
historical canary mode still exercises the two-to-four HPA transition.
`resilience.py` also supports controller, rollout and node scenarios. Rollout
tests default to ten minutes to cover both the update and restoration with
graceful draining. The node scenario defaults to five minutes; it drains one
Gateway node, verifies at least a minute of overlap with the generator’s actual
traffic interval, and always attempts to uncordon it. The rollout
scenario patches the managed NginxProxy temporarily, then restores it.

`collector_restart.py` checks a quiescent same-node restart, followed by a new
load measurement. Checkpoints prevent historical replay, but there is no
exactly-once crash guarantee. Deleted exporter counters must not be added to
live exporter totals; dashboards use rates/increases.

Remove the fixture flag, preview/apply, then remove the orphaned test TLS Secret
only after confirming its Certificate and test listener are gone. Preserve both
public trusted certificates. Validate the final full-stack preview and normal
GitOps deployment before beginning the final observation period.

Historical reports: [comparison](validation.md), [monitoring](monitoring-validation.md),
[public handoff](handoff.md).

The load driver uses `GOMAXPROCS=1` within its existing one-CPU container limit,
while retaining 256 concurrent request workers. This avoids the Go scheduler
oversubscribing that quota. Generator drops and scheduling delays remain visible
in every result; zero-drop and latency gates are unchanged.
