# Public-ingress staging benchmark

This driver targets only `https://staging.registry.modelcontextprotocol.io` and
requires `-staging`. Run it on a separate load-generator host, outside the tested
node pool. It exercises HTTPS, ingress caches, and the application together. HTTP/1.1 is
explicitly selected by default; `-http2` allows HTTP/2. Keep the protocol fixed
within a comparison and verify the response protocol counts in the output.
The existing `tools/readbench` remains the query-focused benchmark.

Before using a generator for a capacity verdict, run `-staging -calibrate` with
the intended rate, workers and saved catalog. This uses only a loopback HTTPS
fixture with a 64-KiB JSON response and exercises the same scheduling,
connection rotation and body consumption. It cannot publish or collect a
staging catalog. The fixture shares generator CPU, so a failure demonstrates a
limit in this local client/server setup; a pass does not certify the external
network. `Calibration` in the result prevents confusing this with a registry
test. Optional `-cpu-profile=generator.pb.gz` writes a local Go CPU profile.

The [fixed-budget replica comparison](REPLICA_CAPACITY_VALIDATION.md) records
the earlier short test and its load-generator limitations. It is not a passing
production promotion or autoscaling gate.

The subsequent [generator calibration and DB-isolation control](DELIVERY_VALIDATION.md)
resolves part of that measurement uncertainty and confirms remaining app CPU
contention. Neither tested layout passed the short offered-load gate.

The [coordinated CPU capacity candidate](COORDINATED_CAPACITY.md) records the next
staging-only profile, CPU request budget and conditional validation gates. It is
not enabled or validated.

```sh
go build -o ingressbench ./tools/ingressbench
./ingressbench -staging -catalog -keys keys.json
./ingressbench -staging -keys keys.json -rate 4500 -updated-rate 36 \
  -search-rate 3 -hot-percent 80 -workers 1024 -duration 10m -token-file publish-token \
  -out result.json
```

Catalog collection walks four real cursor streams (two API versions, limits 50
and 100), up to 4,000 keys. It must be completed before timing. Preserve the same
catalog for comparisons; its SHA-256 is included in results. The hot fraction
uses 128 keys; remaining page requests traverse the full catalog. Search uses
five terms and varied valid limits. Incremental sync varies timestamps across
one hour, one day, and one week, producing uncached requests. Set its rate to
match observed origin demand. Rates are scheduled, not measured completions.

An optional short-lived staging JWT must permit publishing only
`io.benchmark/autoscale-*`. Each publish has a unique name and remote URL.
Publishing is independently scheduled at one per second with 16 workers and a
bounded queue; slow writes do not reduce the offered write rate silently.
Keep the token in a private file, never a command-line argument or report.
Only the benchmark's own staging publications should be removed afterward.

The default read concurrency and queue are each bounded at 512. Dropped work,
HTTP status zero (transport or body-read failure), HTTP failures, up to 100 timestamped failure samples (with an omitted count), cache response
headers, bytes and latency are recorded. Latencies run from intended start to
body completion, including scheduling/queue delay, and include failed requests.
`Scheduled` and `Dropped` count reads; `ScheduledPublishes` and
`DroppedPublishes` count publishes; `Completed` includes both. Elapsed time
includes draining outstanding work. Idle connections rotate every 30 seconds
so new ingress replicas can receive new connections.

`Windows` adds UTC-aligned ten-second read/publish counters. Scheduled work is
attributed to its intended time, drops to the actual enqueue attempt, dispatches
to the start of request processing, and completions/failures to body completion.
Partial first/last windows and delayed completions can cross interval boundaries;
compare counts over the whole run as well as individual windows. Scheduler lag
measures intended time to enqueue attempt (including dropped attempts); queue
delay measures enqueue to request processing, including worker preparation; HTTP
duration measures request processing through body completion. Queue/HTTP timings
are grouped by dispatch window. Their fixed histograms report `P95UpperMS`, a
bucket upper bound, while `MeanMS` and `MaximumMS` use observed durations. Overall
`LatencyMS` retains its existing exact sample percentiles. No URLs, bodies, or
credentials are included in these windows. Use the same instrumented binary in
both arms of a comparison. Window `FailureClasses` counts every failure by its
bounded transport-error class or HTTP status, even after the first 100 individual
failure samples have been retained. This preserves transition timing for later
error classes without retaining client data.

`RequestPhases` separates attempted requests by request kind and bounded cache
outcome. It records scheduler delay, worker queue delay, connection acquisition,
TLS handshake, request-written to first response byte, first byte to body
completion, and total HTTP processing. Connection acquisition includes dial,
TLS and transport waiting; these phases overlap and must not be added together.
First-byte time includes network and upstream queuing, not just server CPU.
Counts distinguish absent events from measured zero durations. TLS hooks can
include speculative connections that are not ultimately used by a request, so
their count can exceed the number of new connections actually obtained. No
addresses, URLs, headers or credentials are retained in these aggregates.

Temporarily exempt only the generator IP from staging ingress rate limits when
measuring aggregate multi-client capacity. Save and restore the prior annotation;
never weaken production rate limits. The generator otherwise represents one
source address and is subject to the per-controller client limit.

Capture generator CPU/network utilization alongside cluster metrics. A saturated
generator or load-balancer connection imbalance is not evidence of database
capacity. Preserve timestamps, query mix, cache outcomes, per-pod CPU, DB CPU
pressure/throttling, pool queues, readiness, ingress HPA decisions and node
counts. Observe cold-cache scaling and recovery as well as the warm steady state.
A run that cannot reproduce the incident is calibration, not proof of a fix.

### Targeted application diagnosis

For a short staging diagnosis, set `MCP_REGISTRY_ENABLE_PPROF=true` on the
experimental application deployment. This opt-in listener binds to
`127.0.0.1:6060`; access it with a pod-specific `kubectl port-forward`, without
adding a Service or Ingress port. CPU captures use
`/debug/pprof/profile?seconds=30` (allowed range: 1–60 seconds). Heap and goroutine
profiles are available at `/debug/pprof/heap` and `/debug/pprof/goroutine`.
Remove the setting when restoring the deployment. Profile files remain local
investigation artifacts; do not publish them without reviewing their contents.

The pool checkout histogram measures successful acquisition through entry into
`Release`, excluding acquisition wait and release bookkeeping. The post-query
histogram measures the interval from the final pgx query-end callback to that
release callback. Query end includes row consumption/close, so this is **not**
server-side SQL completion time. Checkouts with no traced query contribute only
to checkout duration. Both histograms use bounded operation labels and contain
no SQL, arguments, connection identifiers or credentials.

The listing HTTP path uses versioned, normalized JSON prepared when a server is
created or updated. PostgreSQL still selects every result and supplies fresh
registry metadata. Migration 018 adds nullable derived columns; an older binary's
payload update invalidates them through a trigger. Missing or unknown codec
versions fall back to the ordinary typed normalization. Bump `listingCodecVersion`
whenever ServerJSON wire normalization changes. The HTTP router enables this
serialization-only path with schema-link response transforms disabled; other
service/database consumers retain typed records. The listing JSON row counter
labels the source as `prepared` or `fallback`.

Existing rows can be prepared using `go run ./tools/prepare-listing-json` with
`DATABASE_URL` supplied securely through the environment, after migration 018.
The tool defaults to 500-row transactions with a 100 ms pause, a five-second
statement timeout and a one-second lock timeout. It uses keyset batches, copies
rows before normalizing, and conditionally updates only unchanged JSONB. It never
changes source JSON or official timestamps. A concurrent update can leave a row
for another pass; rerunning is safe. Identity mismatches remain on the fallback
path and are counted. Check prepared/fallback coverage before interpreting a
benchmark. No automatic full-table backfill runs during application startup.

For a controlled app-replica comparison, `MCP_REGISTRY_DATABASE_MAX_CONNECTIONS`
and `MCP_REGISTRY_DATABASE_MIN_CONNECTIONS` set each app's pool budget (defaults
60/10). Invalid limits fail startup before connection retries. Two apps at 60/10
and four apps at 30/5 both permit 120 maximum and 20 minimum connections in total.
Lower per-app limits and finish that rollout before increasing replicas. Include
surge pods, monitoring and administration in the PostgreSQL connection budget;
the settings do not enforce a deployment-wide limit automatically.
