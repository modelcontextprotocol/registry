# Production behavior parity

Production's deployed ingress is the baseline. Staging is the validation
environment, not the source of production sizing or behavior. This review uses
production and staging `nginx -T`, production's installed Lua balancer, the
committed Pulumi inputs, and bounded staging fixtures. Production is not modified.

## Contract and corrections

| Behavior | Observed production contract | Gateway implementation / check |
| --- | --- | --- |
| Public identity | Existing IP, Service UID, HTTP/HTTPS node ports and health-check port; `externalTrafficPolicy: Local` | Protected in-place adoption; the earlier preview caught and corrected an immutable cluster-IP replacement |
| Registry routing | Both production aliases; exact `/v0/servers` and `/v0.1/servers`, then `/` | Both aliases on each registry HTTPRoute; original URI/query reaches the backend |
| TLS alias reuse | The two registry names share one SAN certificate | **Corrected:** one registry listener, with host-restricted routes, allows both names on one HTTP/2 connection; SNI validation remains enabled |
| Cache scope/key | GET listing paths; key includes scheme, method, hostname and original request URI/query | Same; aliases and query variants have separate entries |
| Cache lifetime/storage | Successful responses default to 30s; 20MiB key zone, 1GiB disk cap, 5m inactivity | Same per proxy; origin caching headers still participate |
| Cache recovery | Lock on, lock timeout 5s, background update, stale on updating/error/timeout/500/502/503/504 | Same; fixture suite checks origin offload, locking, refresh and last-good response retention |
| Cache bypass | Non-listing requests uncached; native NGINX handling of Authorization, private/no-store and Set-Cookie; no unconditional auth/cookie bypass of an already cached public listing | Retained; see explicit error-response hardening difference below |
| Bulk rate bucket | 600/minute/source IP, burst 1800, no delay, rejection 429 | Same values and local scope |
| General rate bucket | 180/minute/source IP, burst 540, no delay, rejection 429 | Same values; separate from bulk; Grafana remains unthrottled |
| Rate scope | One bulk zone shared by both aliases and both API versions; one general zone shared by aliases; zones shared by workers within a proxy, independent between proxies | Same policy targets/scope; fixture test alternates aliases and API versions while spoofing forwarded IPs |
| Compression | Public credential-free listing GET/HEAD; JSON, minimum 1024 bytes, level 1; gzip/identity negotiation | Same eligibility and settings; see explicit Vary difference below |
| Buffering/timeouts | Listings buffered, registry catch-all and Grafana unbuffered; connect/read/send 5/60/60s; request buffering on | **Corrected:** explicit Grafana policy, request buffering and proxy HTTP/1.1 |
| Request limits | Body 1MiB, body/header timeout 60s, body buffer 8KiB, header buffer 1KiB and large buffers 4×8KiB | Explicitly retained |
| Client keepalive | Maximum age 60s, idle timeout 75s, 1000 requests | Explicitly retained |
| Backend connection reuse | Idle cache 320/worker, age 1h, idle 60s, 10000 requests | **Corrected:** native UpstreamSettingsPolicy; see separate-pool scope below |
| Backend selection | Installed Lua default is round-robin; no passive failure quarantine in that implementation | **Corrected:** native round-robin and `maxFails: 0`; Kubernetes readiness still controls endpoints |
| Retry behavior | `error timeout`, at most 3 attempts, no total retry timeout; no `non_idempotent` replay override | **Corrected:** explicit matching directives; fixture fault test distinguishes GET failover from sent POST non-replay |
| Forwarded headers | Source-IP-derived X-Forwarded-For; generated/retained request ID; scheme headers; original forwarded values kept separately; Proxy header removed | **Corrected:** missing scheme/original headers and HTTPoxy mitigation restored; tested at fixture backend |
| Redirects | HTTP→HTTPS 308, one final slash removed from the raw request URI (including a query-ending slash); upstream Location rewriting off | **Corrected:** small SnippetsFilter before the native redirect; HTTPS path handling is unchanged |
| TLS transport | TLS 1.2/1.3, production cipher list, 10MiB session cache/10m timeout, tickets off, 4KiB TLS buffer | **Corrected:** explicit production settings instead of depending on different image defaults |
| WebSockets | Upgrade/Connection forwarding and bidirectional tunnel | Native Gateway headers retained; fixture handshake and payload echo |
| HSTS | `max-age=31536000; includeSubDomains` on HTTPS | Retained |
| Grafana/monitoring | Grafana OAuth backend, VictoriaMetrics, alerts and contact points | No auth/destination replacement; Gateway/Vector dashboards retained. Both fixture aliases are classified as test traffic |

Matching rate-limit numbers does not make the service-wide allowance fixed:
both implementations have independent buckets per proxy replica. HPA changes the
aggregate allowance and client connection distribution. No distributed/global
limiter is introduced. Cached and uncached requests still consume the appropriate
request bucket.

## Explicit differences retained for review

- **Error-cache hardening:** Gateway explicitly declines to store non-200 origin
  responses, even when they advertise cache freshness. Production has only the
  `proxy_cache_valid 200 30s` default and can honor such response headers. Keeping
  the stronger rule protects last-good cached data. This is a deliberate
  difference, not an assertion of byte-for-byte cache equivalence.
- **Vary headers:** Gateway emits `Vary: Accept-Encoding` for listing responses,
  including identity and bypass responses; production relies on `gzip_vary on`.
  The explicit header avoids ambiguous downstream cache variants. Compression
  eligibility and level remain the same.
- **Upstream pool scope:** ingress-nginx uses one Lua upstream and a shared idle
  connection cache per worker. Gateway has separate registry/Grafana upstream
  pools; the 320-connection setting is per pool/worker. It is an idle cache limit,
  not an active-connection cap. Matching settings does not prove equal memory or
  peak capacity, particularly with Gateway's one-worker-per-proxy configuration.
- **Endpoint-selection mechanics:** native round-robin/NGINX retries replace Lua
  selection. The retry conditions and cap match, but peer selection during endpoint
  churn is not identical; a singleton backend does not gain a second healthy peer.
- **Host validation:** production aliases use a shared listener so normal same-SAN
  connection reuse works. Invalid hostnames and mismatches between unrelated
  listeners can still be rejected differently. We do not disable SNI validation
  globally to mimic permissive cross-site requests.
- **Forwarded Host spelling:** Gateway derives X-Forwarded-Host from `$host`, while
  ingress-nginx uses the original HTTP Host value. Ordinary public 443 hostnames
  match; explicit default ports/case may be normalized. Host and query forwarding
  remain covered by the fixture checks.
- **Cold cache and observability implementation:** cache files are not transferred;
  the cutover starts cold. Gateway access logs and Vector supply edge metrics in
  place of the old controller's exporter. Existing application metrics, Grafana
  configuration and notification destinations stay in place.

These differences must remain visible in the migration review. The stronger
cache/Vary behavior is proposed for retention; it is not silently treated as
current production behavior. A full replacement should not be described as
already proven equivalent in capacity or failure timing.

## Staging evidence (2026-11-08 UTC)

[Sanitized results](production-parity-validation.json) record the final successful
runs. The corrected configuration passed:

- Listing cache MISS/HIT and origin offload, query isolation, locking, expiry and
  background refresh, last-good retention on 503, private/Set-Cookie/error storage
  exclusions, HEAD, gzip/identity/Vary and credential-dependent compression.
- Two certificate SANs on one verified HTTP/2 connection, separate hostname cache
  entries, rejected unknown hosts, forwarded headers, backend connection reuse,
  WebSocket handshake plus bidirectional payload, and the 1MiB request limit.
- Exact production HTTP redirect handling, including repeated slashes and queries.
- Bulk policy: 2200 fixture requests produced 1993 successes and 207 expected 429s;
  general policy: 850 produced 566 successes and 284 expected 429s. Requests
  alternated aliases/API versions and spoofed forwarded IPs. Buckets stayed
  independent, with the intended shared scope within each policy.
- Fault injection into one of two disposable origins: all 20 GETs succeeded,
  with two-attempt failover observed. Ten POSTs reached the healthy origin and
  succeeded; ten reached the failing origin and returned 502 after one attempt.
  Successfully retried listing responses became cache HITs on the next request.
- Live Vector processing of real CRI partial records, retry/missing/invalid timing
  fields, background requests, and the second alias's fixture classification.
  Six client requests and one internal request were classified separately;
  VictoriaMetrics reconciled to six client requests. The emitter was removed.
- All 11 existing production baseline connectivity probes passed. Production was
  read-only throughout; these are not tests of a production Gateway deployment.

The same two staging node UIDs were retained. The application image and public
Service identity/ports stayed unchanged. No node, application or proxy budget was
increased. The ordinary staging configuration removes the temporary test routes,
origins, policies and certificate; the generated test Secret is removed separately.

Issues found during validation were corrected, not counted as successful tests:
old macOS Python/LibreSSL could not verify the explicitly trusted fixture leaf;
Python/OpenSSL with partial-chain verification fixed that without disabling TLS
verification. Curl chose two sockets, so a direct HTTP/2 client replaced that
inconclusive alias test. A fixture surge could not fit the worker's remaining CPU
requests; compact fixtures now use no-surge updates and 25m requests. An early
probe before configuration convergence failed as expected and was rerun only after
programming completed. Expanded redirect cases caught a raw-URI difference; live
production's Lua implementation supplied the exact correction.

These are bounded functional and monitoring checks. They do not qualify production
peak capacity, human alert delivery, authenticated Grafana OAuth or a real publishing
transaction. Those limits and the previously tested renewal/restart scenarios remain
in the [migration runbook](production-migration.md).

Copilot also identified duplicate shared upstream-policy registrations in the older
non-standalone handoff modes. A full `Deploy` mock reproduced failures for `legacy`,
`overlap` and `gateway`; the fix registers those shared policies once. The same test
now passes in all three modes and `standalone`. CodeQL findings in the probe helper
were fixed by enforcing TLS 1.2 in every context, with certificate/hostname checks
retained and a verified public TLS 1.3 handshake afterward. A follow-up review
strengthened non-replay validation to include successful POSTs too. Rechecking
the actual request logs from both completed fault runs confirmed all 40 POSTs
used exactly one upstream attempt (20 successful, 20 intentionally failed).
A regression test rejects a successful POST with two attempts.

## Reproducing the bounded staging checks

Use exactly the two-node compact staging profile. Temporarily enable
`gatewayCanaryFixtures` in a private staging config copy, inspect the full saved
plan, and apply it. Compact mode excludes the dedicated load-generator Pod and
uses a no-surge fixture rollout with a 25m CPU request per temporary origin.
These fixture-only settings allow the two-origin fault test without changing
application/proxy budgets or adding nodes.
The fixture listener has two `.invalid` SANs on a disposable certificate; neither
DNS changes nor production TLS private keys are needed. Fetch only its public
`tls.crt` for the client trust file. Wait for routes, policies and the data-plane
configuration to converge before probing.

Use Python 3.13 with OpenSSL and an isolated virtual environment:

```sh
python3.13 -m venv /tmp/registry-parity-client
/tmp/registry-parity-client/bin/pip install -r deploy/gateway-canary/parity-requirements.txt
/tmp/registry-parity-client/bin/python deploy/gateway-canary/check.py \
  --ip STAGING_IP --fixture-ca /tmp/fixture-ca.pem --output /tmp/cache.json
/tmp/registry-parity-client/bin/python deploy/gateway-canary/production_parity.py \
  --ip STAGING_IP --fixture-ca /tmp/fixture-ca.pem --output /tmp/parity.json
```

Certificate and hostname verification stay enabled. The supplied fixture
certificate is an explicit trust anchor; the old macOS Python/LibreSSL combination
does not support the needed named partial-chain verification flag.

The parity script adds two temporary policies targeting only fixture routes.
It sends 2200 bulk and 850 general requests to those routes, checks independent
buckets and alias/version sharing, then deletes the policies in `finally`. It
briefly scales only the disposable origin to two small Pods to verify read
failover and non-replay of sent POSTs, restoring one replica in `finally`.
Node count/identities are checked. This is functional fault/rate-limit testing,
not a production traffic replay or peak capacity test.

Afterward apply the ordinary staging config to remove the managed fixture resources.
Once the fixture Certificate is gone, delete only its generated `ingress-nginx/fixture-tls`
Secret (cert-manager does not set an owner reference here). Then verify the same nodes/public endpoint, inspect Gateway and collector health, and
repeat ordinary public API/Grafana/redirect checks. Keep raw plans, full configs
and diagnostic logs private; commit only sanitized validation results.

## Sources

The comparison above primarily uses the deployed configs, rather than assuming
chart defaults. Native settings were checked against the pinned 2.8.1 CRD schema
and [NGINX's upstream policy documentation](https://docs.nginx.com/nginx-gateway-fabric/traffic-management/upstream-settings/).
The redirect filter is placed before the generated return in the
[2.8.1 server template](https://github.com/nginx/nginx-gateway-fabric/blob/v2.8.1/internal/controller/nginx/config/servers_template.go).
The remaining raw directives use the already enabled
[Snippets API](https://docs.nginx.com/nginx-gateway-fabric/traffic-management/snippets/).
All configured policies run on OSS NGINX; no Plus active-health-check feature or
paid metrics component is introduced.
