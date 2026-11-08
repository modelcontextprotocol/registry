# Staging Gateway Fabric canary

This opt-in Pulumi deployment runs alongside ingress-nginx. `gatewayCanary` is
rejected outside GCP staging, and the cloud resources also require project
`mcp-registry-staging`. Neither release workflow nor production configuration is
changed. Normal staging pushes continue to deploy the current main image.

The canary uses OSS NGF 2.8.1 / Gateway API 1.6.3 / Vector 0.61.0. Controller,
data-plane, Vector and fixture images are pinned by digest. NGINX Plus and product
telemetry are disabled. Metrics go to the existing vmagent, VictoriaMetrics and
Grafana, under separate `gateway-canary-vector-*` jobs and
`registry_gateway_canary_*` series. Existing dashboards and alerts keep their
current data sources while mappings are qualified. The full event stream is
processed locally; GCP retains 10% of fast successful listing logs after metric
extraction.

## Isolation and cost

A separate tainted pool starts with two e2-standard-2 workers, autoscaling at most
two (node upgrades can temporarily surge). Two NGINX replicas can scale to
four. Each eligible node runs one single-threaded Vector collector requesting
500m CPU; proxy replicas request 500m. The controller requests 100m; the temporary fixture requests 100m when enabled. These are canary starting budgets, not approved production
sizing. The original staging worker/database pool, ingress, IP and storage stay
in place. The canary adds node/disks, one regional external IP and one L4 load
balancer while enabled. Account for this overlap when interpreting cost.

## No DNS changes

DNS access is unavailable for this experiment. Use the exported `gatewayCanaryIP`
with the existing hostnames. For example:

```sh
curl --resolve staging.registry.modelcontextprotocol.io:443:CANARY_IP \
  https://staging.registry.modelcontextprotocol.io/v0.1/health
python3.13 gateway-canary/check.py --ip CANARY_IP --fixture-ca /tmp/fixture.crt \
  --output /tmp/canary-contract.json
```

TLS references the existing staging and Grafana Secrets through narrowly named
ReferenceGrants. The original cert-manager Ingress solver continues renewing
these certificates. This canary **does not validate independent Gateway HTTP01
renewal or an end-to-end browser OAuth callback through the new IP**. Those remain
cutover gates; do not retire ingress-nginx or its solver based on these results.

`gatewayCanaryFixtures: "true"` temporarily enables a fixture and load generator.
`gatewayCanaryComparison: "true"` additionally enables a private ingress-nginx
baseline and caps the gateway at two replicas for a matched comparison. Neither
flag is enabled in the retained canary. Turning them off removes the unthrottled
fixture listener, workload, test certificate, generator and comparison resources.

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
render using `ngf-values.json`, including the certificate-generation hook.
Pulumi owns individual resources so saved plans contain concrete identities;
there is no custom NGINX template or controller fork. The certificate job stays
in state after completion rather than generating perpetual TTL replacements.
This is the upstream bootstrap path for inter-plane TLS. Before production,
manage these internal certificates through the existing cert-manager installation,
as recommended by the [upstream certificate guide](https://docs.nginx.com/nginx-gateway-fabric/install/secure-certificates/).
That is separate from public listener certificate renewal.

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

## Rollback and release flow

The normal staging hostname continues to reach ingress-nginx throughout. To stop
using the canary, stop the explicit-IP probes; no DNS rollback is needed. To remove
its infrastructure, set `gatewayCanary: "false"`, preview and apply through the
normal staging workflow. Review deletion of canary CRDs carefully if anything
else begins using Gateway API; they are cluster-scoped. Do not remove shared CRDs
while another controller relies on them. Original application, ingress, databases
and volumes must remain in the removal preview.

No image pin, release-wrapper or workflow dispatch mode is introduced. For local
qualification only, `REGISTRY_STAGING_IMAGE` can select the already built release
digest without persisting a staging configuration pin. A later normal main push
resumes normal image selection.

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

Use only the staging context. Enable `gatewayCanaryComparison` for matched tests,
preview/apply, and wait for both two-replica controllers to become ready. Build
`load.go` for Linux amd64 and copy it to `/work/loadgen` in `load-generator`.
Export **only** `fixture-tls`'s `tls.crt` field to a local file, and place that
public certificate at `/work/fixture.crt` in the generator. Do not export its key.
The generator validates this certificate and the fixture hostname.

From `deploy/`, run each arm sequentially with identical arguments:

```sh
python3 gateway-canary/measure.py --arm baseline --rate 1000 --seconds 120 \
  --epoch matched-run --output /tmp/baseline-result
python3 gateway-canary/measure.py --arm ngf --rate 1000 --seconds 120 \
  --epoch matched-run --output /tmp/gateway-result
```

Use `--miss-percent 20 --rate 250` for the mixed test. Do not run other fixture
probes during measurement. The driver discovers the internal Service addresses;
GKE's Service range is outside RFC1918, so it explicitly permits those discovered
addresses while retaining TLS verification. Results include offered/achieved
load, errors, cache outcomes, CPU/memory samples and metric reconciliation.
This fixed payload is a controlled comparison, not a production traffic replay.

`limits.py` runs 1,920 bounded real-canary health requests to verify source-IP
limiting; give it `--ip`, `--fixture-ca` and `--output`. `rollout.py --output DIR`
replaces one canary proxy during a fixture load. `collector_restart.py --output
FILE` checks a **quiescent** same-node collector restart, then rerun a short
`measure.py` check. Collector counters reset on restart; persisted file positions
prevent historical replay but do not guarantee exactly-once delivery across a
crash. Raw counter totals from deleted exporter instances must not be added to
current exporter totals; use rates/increases for time-series dashboards.

For autoscaling, disable `gatewayCanaryComparison` and enable only
`gatewayCanaryFixtures`; this removes the temporary baseline and restores the
2–4 replica HPA while retaining the fixture. Wait for the cooldown to return to
two replicas, then run `python3 gateway-canary/autoscale.py --output DIR`. This
uses 256 load workers, 1,000 requests/second and a four-minute window, recording
HPA, readiness, node-count and metric evidence. Afterwards remove both test flags,
preview/apply, and delete the orphaned self-signed `fixture-tls` Secret if
cert-manager's certificate owner-reference option is disabled. The live staging
and Grafana certificate Secrets must remain untouched.

Public staging handoff and rollback: [handoff.md](handoff.md).
