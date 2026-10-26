# Diagnosing production overload

Use a shared time window to compare arrivals, completions, in-flight requests,
pool acquisition, query execution, cache outcomes, and node CPU pressure. Record
the deployed application version and ingress configuration alongside the window.
Do not infer incoming demand from the existing request-rate graph: its counter
increments only after a handler returns.

## Application metrics

All names below are the exported Prometheus names. HTTP labels use route
templates, not server names, cursors, search terms, or full URLs.

| Metric | Meaning |
| --- | --- |
| `mcp_registry_http_request_arrivals_total` | Handler entries, labeled by `method` and `path` |
| `mcp_registry_http_requests_in_flight` | Handlers currently running, with the same labels |
| `mcp_registry_http_requests_total` | Existing completed-request counter, including status codes; semantics unchanged |
| `mcp_registry_http_request_duration_bucket` | Existing end-to-end handler duration histogram in seconds |

Arrivals and in-flight counts cover instrumented application handlers, excluding
the existing skipped health, ping, docs, and metrics routes. They do not include
ingress cache hits or requests rejected before the application. Cancellation and
panic unwind the in-flight counter; a panic need not increment completions.

Compare per-pod arrivals and completions using the same window:

```promql
sum by (instance) (rate(mcp_registry_http_request_arrivals_total[2m]))
sum by (instance) (rate(mcp_registry_http_requests_total[2m]))
sum by (instance) (mcp_registry_http_requests_in_flight)
```

A growing in-flight count with arrivals exceeding completions identifies backlog
growth. Recovery can have more completions per second than the overloaded period.
The hosted scrape interval is 60 seconds: shorter bursts can be missed by gauges,
so also inspect histograms and ingress logs. Use a longer rate window when samples
are missing. Counters reset when pods restart.

## Database metrics

| Metric | Meaning |
| --- | --- |
| `mcp_registry_db_pool_acquire_duration_bucket` | Seconds spent acquiring a connection, including connection creation/validation and failed attempts |
| `mcp_registry_db_pool_acquire_pending` | Acquisition attempts currently in progress |
| `mcp_registry_db_pool_connections` | Scrape-time connections labeled by `state`: `acquired`, `idle`, or `constructing` |
| `mcp_registry_db_pool_max_connections` | Configured maximum for each application pool |
| `mcp_registry_db_query_duration_bucket` | Seconds after acquisition through query completion, row exhaustion, or row close |

Acquisition and query histograms have fixed `operation` and `outcome` labels.
Operations are `list_search`, `list_updated`, `list_page`, `list`, and `other`.
For overlapping list filters, search takes precedence over updated-since, which
takes precedence over cursor pagination. `list` includes other first-page list
filters; `other` includes lookups, publishing, transactions and startup queries.
Outcomes are `success`, `canceled`, `timeout`, and `error`. Pending acquisitions
have only the operation label. No SQL, arguments, database URLs, or error messages
are exported as labels.

```promql
histogram_quantile(0.95,
  sum by (le, operation) (rate(mcp_registry_db_pool_acquire_duration_bucket[5m])))
histogram_quantile(0.95,
  sum by (le, operation) (rate(mcp_registry_db_query_duration_bucket[5m])))
sum by (instance, operation) (mcp_registry_db_pool_acquire_pending)
sum by (instance, state) (mcp_registry_db_pool_connections)
```

Acquisition is not pure queue time: it may create or validate a connection.
Query time excludes pool acquisition but includes network transfer and application
row consumption; it is not PostgreSQL CPU time. Queries inside a transaction reuse
its connection, so acquisition and query counts need not match. These tracers
cover pgx Query, QueryRow and Exec; batch and COPY operations are not instrumented.
Histograms observe finished attempts, so combine them with pending/in-flight
gauges and read-only `pg_stat_activity` samples while operations remain stuck.
Use fresh `pg_stat_statements` deltas to attribute completed database work, rather
than historical averages. Do not subtract histogram percentiles to estimate wait.

## Ingress cache evidence

The access log retains the ingress-nginx combined-log prefix and appends:

- `cache_status`: HIT, MISS, EXPIRED, STALE, UPDATING, BYPASS, REVALIDATED, or `-`.
- `scheme`, `host`, `ingress_pod`: identify the cache key and the independent cache.
- `upstream_connect_time`, `upstream_header_time`: upstream phase timing in seconds.
- `completed_at`: completion time as Unix seconds with millisecond precision.

The prefix already includes method, original request URI, total request duration,
upstream address, upstream response time/status, and request ID. Upstream fields
may be `-` or contain multiple attempts; do not assume they are always scalars.
Existing parsers that anchor the end of the old format must accept the new suffix.
NGINX's default log escaping remains enabled. Do not treat the combined log as JSON.

Read ingress logs through the existing GKE logging destination or a bounded
`kubectl logs` capture. The VictoriaLogs collector currently ingests application
logs only; this change does not add the much larger ingress stream to that store.

Group by ingress pod and the complete key components:
`scheme + method + host + original request URI`. Preserve query parameter order
and encoding, because the configured cache key does. Compare simultaneous MISS or
EXPIRED upstream requests for identical keys with request duration and the
five-second cache-lock timeout. A request that outlasts this timeout can proceed
upstream without caching its response; cache status alone does not prove a lock
timeout. Background refreshes and lock-age behavior can also create upstream work.
See [NGINX cache-lock documentation](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_cache_lock_timeout).

Approximate request start time as `completed_at - request_time` when correlating
with database and CPU samples. Access logs appear on completion and cannot reveal
requests that have not yet finished. User-agent strings are client claims, not
verified identities. Avoid copying raw logs containing client identifiers or
query strings into public incident reports.

## Rollout and interpretation

The application metrics require a new application image; ingress log fields
require the infrastructure deployment. Neither changes concurrency, resource
budgets, caching behavior, SQL, or indexes. Use the normal staging and production
promotion process. On staging, hold a request open and verify that arrivals and
in-flight counts change before completions, then verify they drain. Confirm pool
metrics scrape and a cached list response has an ingress cache status in logs.
Confirm existing log consumers accept the appended fields before promotion.

Capture a busy period and its recovery. Check whether duplicate upstream requests,
pool acquisition waits, query duration, or node CPU pressure rises first. Fix the
existing listing-latency alert separately; an alert remaining green is not proof
that these measurements are healthy. Reproduce the observed traffic with
production-scale data before validating a cache, concurrency, query-plan, or
capacity change. A tiny staging seed only validates instrumentation wiring.
