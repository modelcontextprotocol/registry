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

## Individual slow listing requests

The application emits `listing diagnostic` structured logs for GET `/v0/servers`
and `/v0.1/servers`. A request still running after two seconds produces
`event=listing_slow_in_progress`, followed by another snapshot every ten seconds
until completion. Slow or failed requests produce `event=listing_complete`.
Fast successful requests are silent. All these events share a per-process budget
of two events per second with a burst of ten; `diagnostics_suppressed` on the next
emitted event reports the number dropped since the preceding event. These are
samples, not an exhaustive request audit or a replacement for counters.

Use `request_id` to join application records to ingress `$req_id`. Ingress passes
`X-Request-ID` upstream. Direct requests without a valid ID receive a generated
ID in their logs; IDs are correlation hints, not trusted identities. The logger
accepts only 1–128 ASCII letters, digits, hyphens or underscores. A cached response
has no new application event. The app does not add an ID response header, which
would otherwise be cached and reused. Pod, route, start and observation times
identify the request's lifetime, including after client cancellation.

The exclusive wall-clock phase durations (milliseconds) add up to `total_ms`:

| Field | Measured interval |
| --- | --- |
| `handler_ms` | Handler parsing/filter construction before acquiring a connection |
| `pool_acquire_ms` | Pool acquisition, including connection creation/validation |
| `query_wait_ms` | pgx query start until Query returns; may include initial network/server work |
| `row_fetch_ms` | Advancing rows and finishing/closing the result, including network/server waits |
| `row_decode_ms` | Scanning, JSON decoding, and constructing response records |
| `response_ms` | Work after the database method returns, including response serialization/writing |

`first_row_ms` runs from pgx query start to the first successful `Next`, excluding
pool acquisition. It overlaps query/fetch timings; do not add it to the phase sum.
`first_row_received=false` distinguishes an empty or unfinished result from a
zero-duration first row. Neither query/fetch time nor first-row time is pure
PostgreSQL execution time. Row decoding also includes scheduler delays.

`phase` identifies the current operation in a progress snapshot. Completion
records include `failed_phase`, `outcome`, effective HTTP `status` (499 for client
cancellation), and PostgreSQL `sqlstate` where available. `rows_received` counts
rows presented for decoding; `json_bytes` counts scanned JSONB payload bytes,
not serialized HTTP bytes. Filter flags (`search`, `cursor`, `updated`, `latest`,
`exact_version`, `include_deleted`) and `limit` describe the list query shape
without exposing search strings, cursors, raw SQL, parameters or response data.
`backend_pid` links to a contemporaneous `pg_stat_activity` sample on the registry
PostgreSQL instance; it is zero before a query starts and is not globally unique
across database restarts. `pool_acquired_at_start` and `pool_max` describe the pool
when acquisition began, not necessarily its state at log time.

Application logs flow to the existing VictoriaLogs and GKE logging destinations.
These diagnostics distinguish application row processing from waiting for rows,
but still require live PostgreSQL wait-event samples to distinguish internal
locks, buffer contention, CPU scheduling and storage waits. A process crash can
lose the completion event; rate limiting can suppress either event independently.
