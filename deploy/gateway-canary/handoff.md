# Staging public-IP handoff

The staging handoff is opt-in and restricted to `mcp-registry-staging`. Production
and the image/release workflows are unchanged. `gatewayStagingHandoff: legacy`
prepares the new data plane but leaves public traffic on ingress-nginx.

## Architecture

- Promote the existing public address, `35.222.36.75`, to a protected regional
  reservation. Keep the original `ingress-nginx/ingress-nginx-controller` Service,
  its UID, node ports, health-check port, and `externalTrafficPolicy: Local`.
- Run `ingress-nginx/registry-edge` under the existing OSS Gateway Fabric
  controller. A Service can only select pods in its own namespace. Its generated
  Service is ClusterIP, so preparation allocates no additional public load balancer.
- Derive routes, caching, rate limits, compression, request handling, and TLS
  references from the qualified canary assets. Keep both original certificate
  Secrets; neither the certificates nor their issuer is replaced.
- Reuse the existing two Vector collectors. Include the new data-plane logs and
  native metrics in the existing VictoriaMetrics/Grafana pipeline. Cloud Logging
  sampling still happens after local, unsampled metric extraction.
- Cap each Gateway at two replicas while both exist on the same two canary nodes.
  This temporary overlap consumes an additional 500m CPU/256Mi request per node;
  it does not change node-pool bounds. Restore the qualified scaling arrangement
  when retiring the original canary data plane, before production qualification.

The original ingress controller remains running for rollback and HTTP01 renewal.
The new Gateway routes `/.well-known/acme-challenge/` on both public hostnames to
`legacy-acme`, a separate ClusterIP Service whose selector always targets the
original ingress pods. It must never point at the public Service, which would
loop after handoff. This preserves the current cert-manager Ingress solver; it
is **not** a proof of Gateway-native ACME or permission to retire ingress-nginx.

## Staged operation

Use the normal full-stack saved-plan workflow, not targeted Pulumi updates.
Always build the `deploy/infra` binary before previews. Inspect previews and
abort on any public Service replacement, unexpected address change, node-pool
expansion, application image pin, or production Gateway change.

1. **legacy:** reserve the current IP, prepare the Gateway, labels and telemetry.
   Verify the existing Service UID/IP/ports, both ready Gateway replicas,
   accepted/programmed Gateway and routes, existing TLS chain, cache HIT,
   redirects, client-IP handling, metrics and unchanged node inventory.
2. **overlap:** the public Service selects the shared staging-edge label on both
   implementations. Wait for the GCP target pool to report the new serving nodes
   healthy, and verify both implementations appear in request logs.
3. **gateway:** the public Service selects only `registry-edge`. Keep continuous
   public registry/Grafana probes running. GCP health checks converge separately
   from EndpointSlices; overlap warms new nodes but does not prove zero dropped
   connections when old nodes lose local endpoints. Measure this explicitly and
   qualify draining before claiming an interruption-free production procedure.
4. While gateway-only, use a temporary Let's Encrypt **staging** Issuer and
   Certificate for the existing public hostnames. Confirm issuance and reissuance
   through the challenge route. Never attach the untrusted test certificate to
   a public listener. Remove the test issuer/certificate/account and solver
   artifacts afterwards.
5. Complete Google OAuth including the Grafana callback and dashboard access.
   Exchange a real GitHub token, publish a uniquely named staging fixture, read
   it, then mark that exact version deleted using the authenticated status API.
   Keep fixture files outside this repository and never log tokens.
6. Rehearse rollback through **overlap** (wait for legacy nodes to be healthy),
   then **legacy**. Confirm the same IP, UID, TLS and functioning public endpoints.
   A persistent switch is a separate deliberate phase after these checks pass.

The passphrase file can be supplied without exposing its value:

```sh
cd deploy
export PULUMI_CONFIG_PASSPHRASE_FILE=/path/to/staging-passphrase
make staging-preview
make staging-up
```

Change only the `mcp-registry:gatewayStagingHandoff` value for the next phase;
retain `gatewayStagingPublicIP` and the protected address during rollback.
Do not merge a phase change until reading all PR reviews and inline comments,
including Copilot. Let normal staging GitOps apply the reviewed desired state.

## Evidence

Local deployment tests cover environment/project guards, fixed Service identity
and compatible ports, both TLS references, policy target rewriting, fixture
exclusion, bounded overlap replicas and the non-looping ACME backend. Live
handoff, rollback, renewal, OAuth and publish results must be recorded before
marking these gates complete. Prior canary monitoring and load evidence is in
[monitoring-validation.md](monitoring-validation.md) and [validation.md](validation.md).
