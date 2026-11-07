# Canary monitoring contract

The existing VictoriaMetrics/vmagent/Grafana stack remains the monitoring backend.
These changes use the existing single-threaded Vector DaemonSet, with unchanged
CPU/memory requests and limits. Canary node autoscaling is capped at the existing
two nodes. Managed GKE upgrades retain their existing temporary surge policy.

## Log reconstruction

Vector reconstructs CRI `P`/`F` fragments **before** looking for the access-record
marker. Groups are separated by file and stream; concatenation adds no bytes.
Complete records flush immediately. Incomplete groups expire after five seconds
or 16 fragments and increment `registry_gateway_canary_cri_errors_total`.
The file source retains its 64 KiB per-fragment limit: buffered messages are
bounded to at most 1 MiB per active file/stream group. This is not an unbounded
log buffer. A missing tail cannot be recovered; it is reported instead of counted
as an HTTP request. Vector's internal error/discard counters must also be monitored.

This closes ordinary runtime splitting, including a split within the JSON kind
marker. It does not introduce durable partial-message buffers or exactly-once
delivery across a collector crash/node loss. Existing file checkpoints survive
same-node replacement; counters reset with the process.

## Timing semantics

The existing `upstream_duration_seconds`, `upstream_connect_seconds` and
`upstream_header_seconds` histograms keep their original single-numeric semantics.
This matches ingress-nginx's `tonumber(...)` collection: missing values and timing
lists do not produce observations. See the pinned
[ingress-nginx monitor source](https://github.com/kubernetes/ingress-nginx/blob/controller-v1.15.1/rootfs/etc/nginx/lua/monitor.lua).

New `upstream_all_attempts_seconds`, `connect_all_attempts_seconds` and
`header_all_attempts_seconds` histograms record one observation per client request
with at least one measured timing. Each observation sums numeric timings across
comma-separated attempts and colon-separated upstream groups. Missing `-` values
are omitted, never replaced with zero. All-missing timings produce no observation;
an invalid or negative token suppresses that field's aggregate and increments one
`upstream_timing_errors_total` per affected record. Background refresh requests
remain separate from client latency histograms.

`multi_upstream_requests_total` counts requests whose upstream response timing
contains multiple entries. This includes retries **and upstream-group redirects**,
not just retries. Summed upstream times describe measured upstream work, not
end-to-end client latency; use `request_duration_seconds` for the latter.
See [NGINX upstream variables](https://nginx.org/en/docs/http/ngx_http_upstream_module.html#variables).
All metric names above have the `registry_gateway_canary_` prefix.

## Query mapping

Canary queries select `traffic="registry"` or `traffic="grafana"`. Synthetic
records select `traffic="fixture"` and must not be mixed into application alerts.
Use `rate`/`increase` for counters, rather than summing raw values from historical
exporters. Labels such as ingress name are not interchangeable with the canary's
bounded route categories.

| Existing ingress metric | Canary equivalent | Meaning |
| --- | --- | --- |
| `nginx_ingress_controller_requests` | `registry_gateway_canary_requests_total` | Client requests; group by status/method/route as needed |
| `nginx_ingress_controller_request_duration_seconds` | `registry_gateway_canary_request_duration_seconds` | Client request duration, including cache hits |
| `nginx_ingress_controller_request_size` | `registry_gateway_canary_request_bytes` | Request bytes |
| `nginx_ingress_controller_response_size` | `registry_gateway_canary_response_bytes` | Total bytes sent, including headers |
| `nginx_ingress_controller_response_duration_seconds` | `registry_gateway_canary_upstream_duration_seconds` | Single-numeric upstream duration |
| `nginx_ingress_controller_connect_duration_seconds` | `registry_gateway_canary_upstream_connect_seconds` | Single-numeric upstream connect time |
| `nginx_ingress_controller_header_duration_seconds` | `registry_gateway_canary_upstream_header_seconds` | Single-numeric upstream header time |

The canary dashboard additionally exposes cache outcomes, background refreshes,
multi-upstream timings, parser/CRI/timing errors, missing byte fields and Vector
internal errors/discards. Its scrape-health panel must be checked alongside
error rates: a disappeared exporter must not look like zero errors.

## Validation

Run the actual pinned Vector binary locally:

```sh
python3 gateway-canary/monitoring_contract.py --vector /path/to/vector
```

After applying the reviewed staging preview, run:

```sh
python3 gateway-canary/monitoring_staging.py --output /tmp/monitoring-staging.json
```

The staging checker creates one small node-pinned emitter at a time, observes
real CRI partial records, reconciles client/internal/timing counters on both
collectors, checks ingestion into VictoriaMetrics, removes its emitter Pods and
asserts that node identities did not change. It emits deliberately invalid
synthetic timings to exercise error telemetry; coordinate this with any active
error notifications. It does not simulate an actual failing upstream or claim
notification delivery just because a PromQL expression returns a result.

Before merging, inventory the live dashboards and alert rules, validate their
mapped queries, and exercise firing/resolution through Grafana's evaluator and
notification path. Real recipient delivery requires an explicitly approved test;
an internal webhook receipt alone does not prove delivery to a person's inbox.
