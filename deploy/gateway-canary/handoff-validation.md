# Staging Gateway handoff validation — 2026-11-08

Staging public traffic now uses the OSS Gateway Fabric data plane. The original
Service UID, public IP `35.222.36.75`, node ports and health-check port were retained.
The previously ephemeral IP is now a protected Pulumi-managed reservation. Both
Gateway replicas and the existing Vector collectors run on the same two canary
nodes; the cluster still has four nodes total.

## Results

- Gateway and all five HTTPRoutes accepted/programmed; both proxy pods ready.
- Existing public registry and Grafana certificates validate over TLS. HTTP
  redirects to HTTPS; HSTS remains enabled. Listing requests produce MISS then
  HIT with matching bodies and gzip compression.
- Direct public handoff stabilized after about 41 seconds. An emergency selector
  rollback to ingress-nginx stabilized after about 41 seconds; returning to
  Gateway stabilized after about 40 seconds. Each result required three
  consecutive successful probe rounds for registry health, listings and Grafana.
  Connection errors/timeouts during convergence are recorded, not counted as
  successful requests. Preparation also caused an approximately 10-second
  connection interruption. Brief staging downtime was explicitly accepted;
  these results are not a zero-downtime production qualification.
- A fresh temporary Let's Encrypt staging issuer issued a certificate for both
  existing public hostnames. `cmctl renew` produced revision 2. Gateway access
  logs confirm HTTP01 challenge responses for both hosts. Public listener
  certificate Secrets were unchanged; the test namespace and all test ACME
  resources were removed. Renewal still depends on the retained legacy ingress
  solver through the dedicated `legacy-acme` Service.
- A real GitHub token exchange, authenticated publish, read-back and authenticated
  status update all returned 200 through Gateway. The unique staging test version
  was marked deleted afterward; no publisher credentials or fixture files were
  added to the repository.
- Google OAuth completed in Chrome using the existing authorized account. Its
  callback returned to staging Grafana, and the provisioned Gateway dashboard
  loaded live traffic, latency, cache and error panels.
- All nine Gateway/Vector scrape targets are healthy, including both new proxies.
  Metrics from the new proxy pods are present in VictoriaMetrics. No nodes were
  added and no image pins or release-workflow changes were introduced.
- Full deployment Go tests, 72 deployment-script tests and PR CI/CodeQL passed.
  Both environment previews passed. Production was previewed only and showed
  the existing admission-job and empty Grafana RBAC housekeeping changes.

The emergency rollback drill used JSON Patch tests for the original Service UID
and expected selector, then restored the desired Gateway selector. Planned
updates continue through the normal saved full-stack Pulumi plan and GitOps.
See [handoff.md](handoff.md) for the procedure and
[handoff-validation.json](handoff-validation.json) for sanitized evidence.

## Remaining scope

Retain ingress-nginx for rollback and the ACME solver during the staging soak.
Before removing it or migrating production, qualify Gateway-native certificate
renewal, production interruption/draining requirements, internal certificate
rotation and node-failure capacity. Retire the original isolated canary data
plane before restoring the qualified four-replica scaling ceiling on the public
Gateway. Existing staging notification proof uses an internal webhook; delivery
to a person is still a separate production check.
