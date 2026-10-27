# Local read-path benchmark

This harness measures the real registry HTTP routes with PostgreSQL 17 and an isolated synthetic dataset. It does not contact production or staging, and does not provision cloud resources. Every database and HTTP target must use a literal loopback IP; the database must be named `registry_bench`. The development seed file supplies JSON templates only; no production data is downloaded.

See [measured results and limitations](RESULTS.md) for the initial optimization comparison and [cyclic crawl results](CYCLIC_RESULTS.md) for the search minimum and repeated full-registry walks.

## Cyclic full-registry crawls and the search minimum

```sh
./tools/readbench/run-cycles.sh /tmp/my-registry-cycles
```

This separate runner builds the baseline from the initial harness commit and the candidate from the current checkout, then creates a fresh namespaced fixture and an isolated PostgreSQL cluster on port 55433. It runs two app processes (`GOMAXPROCS=2` each) behind two independent NGINX cache processes on ports 18082/18083. Both caches use the current 30-second TTL and five-second lock settings; no artificial origin delay is injected. Ports 18080–18083 must be free. Baseline/candidate order is A/B/B/A with fresh app processes and empty edge caches per trial. `READBENCH_BASELINE_REF` explicitly overrides the recorded baseline for future comparisons.

The workload launches three waves, 20 seconds apart, of 20 crawlers each. A crawler follows actual `metadata.nextCursor` values through the whole listing, preserving its filters and checking ordering, duplicates, page counts and cursor progression. With the default client count, 60% walk all versions, 30% walk latest versions, and 10% use distinct seven-day incremental watermarks. New waves start regardless of whether earlier walks finished. Crawler request starts share a ceiling of 1,200/sec; this is intentional client pacing, **not a claim that the server sustained that rate**. Results record actual completed requests per second and full-walk completion time, including pacing. Per-page HTTP latency excludes intentional pacing. The first wave starts cold, the second can reuse warm entries, and the third can cross cache expiration.

Ten independently scheduled searches/sec and one publish/sec continue through the waves and a final 30-second observation period. One percent of searches are short terms; this approximates the recent quiet sample, **not a measured burst distribution**. Other terms include `notion`, `net.todoist/mcp`, `io.github.github/`, `mcp` and `benchmark`, with a mix of latest/all versions. The fixture gives 70% of ordinary names a repeated `io.github.benchmark-` namespace, plus sparse matches and matches late in name order. It exercises the namespace/trigram issue missed by the original sequential-name fixture, but does not reproduce the production distribution exactly.

Search and publish latency includes generator scheduling delay. A further request-timeout allowance lets outstanding work drain. Walks that miss the hard deadline are marked incomplete; cancellation is never counted as successful recovery. Once-per-second direct-app samples record pool waiters, acquired connections and in-flight requests, with an observed-target count so missing metrics cannot masquerade as zero queues. PostgreSQL statement snapshots retain user/query/top-level identity to support correct counter differences.

The candidate's short-search `422` responses are counted separately. They are **never credited as successful non-short read throughput**. Compare completed full walks, accepted-search latency, publish success/latency, driver drops and queue recovery. Reject claims of improvement based only on the time required to reject short queries. The min-length policy cannot prevent unfiltered registry crawls.

Increase demand with `READBENCH_CRAWLERS=200 READBENCH_CRAWL_RPS=12000 READBENCH_SEARCH_RATE=100`, or test a larger short-search share with `READBENCH_SHORT_PERCENT=50`. `READBENCH_PUBLISH_RATE` changes publishing and `READBENCH_ORDER='baseline candidate'` selects a shorter comparison. These are local stress scenarios, not forecasts of production capacity. The underlying `cycles.py` also accepts explicit intervals and recovery duration. The harness does not emulate Linux cgroup CPU quotas, separate physical nodes or real client retries. Reuse the fixed baseline/candidate binaries and record all failures when comparing overload behavior.

The fixed search set repeats through the edge caches. Crawler URLs use one API version, canonical query ordering and limit 100. This favors shared cache entries compared with diverse production clients and searches. More clients can increase cache hit share without proportionately increasing origin work; always report both. On macOS the runner prevents idle sleep only for its own lifetime, and clock-continuity checks still reject invalid measurements.

## Run

Prerequisites: Go matching `go.mod`, Python 3, PostgreSQL 17 binaries (including `pg_stat_statements`), and NGINX on `PATH`. On macOS these can be installed with Homebrew (`postgresql@17`, `nginx`). Docker is not required.

```sh
./tools/readbench/run-local.sh /tmp/my-registry-benchmark
```

Use a fresh output directory. The script initializes its own cluster, binds PostgreSQL to `127.0.0.1:55432`, and stops it on exit. It retains the data and results for inspection. It uses HTTP ports 18080 and 18082; those must be free. Change the database port with `READBENCH_PG_PORT` and the database workload duration with `READBENCH_DURATION` (default `20s`). No existing database or system NGINX service is stopped or reconfigured. Local authentication is trust-based, so use this only on a trusted development machine.

The fixture contains 185,001 server versions, three versions per name, approximately 1% deleted records, JSON shapes taken from the existing local seed, and deterministic update timestamps spread across 30 days. On the measured machine its heap was approximately 241 MiB and total relation size approximately 309 MiB. The fixed timestamp epoch is `2026-10-26T12:00:00Z`; generated filters use that epoch, not the current date. Publish probes add records with current timestamps. This intentionally includes a small new-write tail, but does not reproduce production's exact data distribution.

The `serve` mode uses the production router, telemetry, pool configuration, service and database implementation. It uses a public benchmark-only JWT key, disables external package-ownership validation, and binds only to loopback. Publish probes exercise JWT authorization, schema validation, the database transaction, uniqueness checks and insertion. External registry and identity-provider latency is excluded.

## Workloads and measurements

`load` offers arrivals independently of response completion. Latency starts at the scheduled arrival time, including generator dispatch lag. The read pool has `-workers` workers and an equally sized waiting queue; excess arrivals count as **driver drops**, not successful requests. HTTP failures and deadlines are recorded separately. Success latency is labelled as such; always inspect error and drop counts alongside it. Publishing has its own open-loop scheduler and bounded pool, so one slow publish does not serialize later arrivals. The report includes successful read goodput, p50/p95/p99, publish results, dispatch lag, response bytes, origin-start counts and application process CPU deltas.

Offering duration is limited to two minutes per trial, below the benchmark token's lifetime. `valid_for_comparison` checks clock continuity and authentication only. A clock discontinuity above 500ms invalidates the trial; the scripts retain it separately and retry at most twice after warm-up (or with a fresh cache). Three invalid trials stop the experiment. `served_all_offered_requests` additionally requires every scheduled read and publish to succeed without generator drops. A valid saturation measurement can fail this second check: do not present its success latency as an equal-work improvement over a run that completed all offered work.

`verify.py` compares complete response hashes across both API versions, filters and three pages per nonempty case, checks ordering/deletions, and saves SQL plans at one-hour, one-day and one-week selectivity. Custom and generic prepared plans are captured separately. The experiment metadata records binary/source/fixture hashes, PostgreSQL settings and the statement-statistics reset time.

Available scenarios:

- `page`: varied name/version cursors, limit 100.
- `updated`: varied cursors and unique seven-day update watermarks.
- `sparse`: one-hour update watermarks; exercises selective incremental queries.
- `search`: varied substring searches.
- `mixed`: 55% page / 43% updated / 2% search, approximating the observed earlier origin mix.
- `page-heavy`: 85% page / 14% updated / 1% search, approximating the later origin mix.
- `hot`: eight repeating cursor URLs, for cold-cache fill coordination.

The two predefined experiments use counterbalanced order (A, B, B, A), fresh application processes, fixed `GOMAXPROCS=2`, fixed offered rates, and warm-up for each database workload. `compare.py` restores the original column statistics target on exit and removes only synthetic publish records between runs. It captures database statement counters before and after each measured run. The statistics experiment compares targets 100 and 1000 for `servers.updated_at`; no indexes are added.

The cache experiment compares the production lock timeout/age of five seconds with ten seconds. Both use a 30-second TTL, eight initially cold keys, 100 offered reads/sec for eight seconds, and a deliberately injected **six-second origin delay**. This isolates duplicate cache-fill behavior. **It is not a realistic throughput or production latency forecast.** The application routes still run after the artificial delay. Each round uses an empty, separate NGINX cache. The load timeout is 20 seconds so slow successes remain visible rather than being hidden by a short client timeout.

## Individual runs and profiling

After starting a disposable database and seeding it:

```sh
go build -o /tmp/readbench ./tools/readbench
GOMAXPROCS=2 /tmp/readbench -mode serve -listen 127.0.0.1:18080 \
  -db postgres://mcpregistry@127.0.0.1:55432/registry_bench \
  -cpuprofile /tmp/readbench.cpu
/tmp/readbench -scenario mixed -rate 1800 -duration 30s -out /tmp/mixed.json
# Stop the server with SIGTERM before reading the profile.
go tool pprof -top /tmp/readbench /tmp/readbench.cpu
```

`-target` accepts comma-separated local app origins for a two-replica experiment. CPU metrics for a load-balancing proxy are not attributable to individual backends; use direct targets when measuring per-process CPU. `/_readbench/stats` and artificial delays exist only in this tool, never in the registry executable.

## Interpretation and limitations

This measures relative changes on one developer machine. It does not recreate GKE scheduling weights, Linux cgroup quotas, two physical nodes, ingress traffic distribution, exact client retries, or production data skew. `GOMAXPROCS` constrains runnable Go threads, not PostgreSQL or host CPU. An improvement here is a candidate for a separately reviewed production change, not proof of a production capacity multiplier.

Use the same binary, fixture, resource settings and load for A/B comparisons. Do not run compilers, unrelated benchmarks, or CPU-heavy work during timed trials. Keep the result JSON, versions, SQL plans and experimental configuration. Reject an apparent latency improvement if it comes from more errors/dropped requests or reduced useful work. Verify response equivalence, pagination, deleted records and publishing before proposing a rollout. Do not increase staging capacity to run this harness.
