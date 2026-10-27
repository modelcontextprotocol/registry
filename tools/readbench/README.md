# Local read-path benchmark

This harness measures the real registry HTTP routes with PostgreSQL 17 and an isolated synthetic dataset. It does not contact production or staging, and does not provision cloud resources. Every database and HTTP target must use a literal loopback IP; the database must be named `registry_bench`. The development seed file supplies JSON templates only; no production data is downloaded.

See [measured results and limitations](RESULTS.md) for the initial optimization comparison.

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
