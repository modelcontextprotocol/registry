# Constrained search/crawl benchmark — 2026-10-27

The local test reproduced CPU throttling, multi-second search latency, pool
contention and slower publishing. A namespace suffix index plus an equivalent
search predicate removed that overload in the tested workload. Forcing custom
plans alone did not. Production and staging were not modified or load-tested.

## Why this query change

Earlier production evidence for `search=io.github.github/&version=latest`
showed the trigram index returning about 132,842 candidates for one final row.
It took 361 ms with a custom plan and 346 ms with a generic plan. Thus a generic
plan was not required for that particular expensive search. This is previously
captured read-only evidence, not a new production experiment.

The local fixture reproduced this poor selectivity: the original query took
61.7 ms for `io.github.github/` and 9.5 ms for a varied complete server name in
isolated diagnostic executions. With the additional namespace predicate and
index, these examples took about 0.06 ms. These single-query timings explain the
mechanism; the concurrent comparison below establishes the workload effect.

`check_server_name_format` already guarantees exactly one `/` in stored names.
Consequently, a substring match spanning `/` must match a suffix of the
namespace. The candidate adds a case-insensitive prefix lookup over the reversed
namespace, using a B-tree expression index, and retains the original complete
`ILIKE` predicate. Partial namespaces, case-insensitive matching, literal
`%`/`_`/backslash escaping, ordering and cursors are preserved. Searches with no
namespace or a namespace shorter than three bytes keep their old predicates.
PostgreSQL's normal automatic plan selection remains enabled.

Trigram indexes can return many candidates for repeated/common text, which still
need the full predicate checked. PostgreSQL documents how trigrams are extracted
and used for pattern searches in its [pg_trgm documentation](https://www.postgresql.org/docs/17/pgtrgm.html).
The new index is a targeted complement to the existing trigram index, not a
replacement. It occupied 4,734,976 bytes (about 4.5 MiB) on this fixture.

## Experiment

The final comparison uses A/B/B/A order on a fresh database. Both versions have
the three-character guard; all offered search terms meet it. A uses application
commit `d317a62` without the namespace index. B adds the namespace predicate and
index. The runner drops/creates that index explicitly by variant, analyzes the
fixture, starts fresh app/cache processes, and keeps every other setting fixed.

PostgreSQL 17.11 runs in a Linux ARM64 container with verified cgroup v2 limits:
1.5 CPUs, 4 GiB memory, no swap, 512 MiB shared buffers, 200 maximum connections,
default statistics target 100 and `plan_cache_mode=auto`. The local VM has four
CPUs and 6 GiB RAM. Two native Go apps (`GOMAXPROCS=2`, 60 pool connections each)
and two native NGINX caches run outside the database quota. This enforces the
database's CPU/memory ceiling; it does not emulate the complete production
cluster or constrain total host/application CPU.

The namespaced fixture contains 185,001 versions, three per name, about 1%
deleted, with deterministic update timestamps. Three waves of ten crawlers start
at 0, 20 and 40 seconds. They vary API version, page size, parameter order and
incremental watermarks while following real cursors. There is a shared ceiling
of 1,200 crawler starts/sec, not a fixed achieved throughput.

For 60 seconds, 200 open-loop searches/sec bypass both caches and reach the apps
directly. Half are varied complete names; the remainder mix sparse, common,
namespace, numeric and absent matches. Each category is balanced across both
replicas. There are 128 search workers and 128
waiting slots; excess arrivals are counted as driver drops. Publishing runs at
one/sec for 90 seconds; observation/draining ends at 100 seconds. This is an
intentional saturation workload, not a measurement of the production burst mix.

Page latency excludes deliberate crawler pacing. Search and publish latency
includes scheduling delay. Successful-response quantiles must be read alongside
drops, errors and unfinished walks; they are not equal-work comparisons when
the baseline fails to complete its offered work.

The first exploratory comparisons, including an earlier A/B/B/A sequence,
assigned each even-period category to one replica. They are retained separately.
The generator was corrected and the complete A/B/B/A comparison repeated; only
that balanced comparison is used for the final result. No trial was silently
discarded because it was slower or failed to complete work.

## Final balanced comparison

Ranges are the two separate trials for each variant, not pooled quantiles.
Every trial offered the same 30 walks, 12,000 searches and 90 publishes.

| Measurement | Baseline | Namespace index + predicate |
| --- | ---: | ---: |
| Complete crawls | 18/30 | 30/30 |
| Successful crawl pages | 54,271–55,901 | 84,143 |
| Successful searches | 7,514–9,075 / 12,000 | 12,000 / 12,000 |
| Driver-dropped searches | 2,914–4,483 | 0 |
| Request failures, excluding driver drops | 4–13 | 0 |
| Successful publishes | 90/90 | 90/90 |
| Full-name search p95 | 2,111–2,932 ms | 1.60–1.64 ms |
| Namespace search p95 | 7,695–8,173 ms | 1.44–1.49 ms |
| Common-term search p95 | 3,443–3,672 ms | 29.8–31.4 ms |
| Crawl page p95 | 192–199 ms | 2.88–2.98 ms |
| Publish p95 | 452–591 ms | 12.09–12.86 ms |
| Peak sampled pending acquisitions, both apps | 37–42 | 1 |
| Database CPU consumed over observation | 99.2–99.4 CPU-seconds | 37.2–39.5 CPU-seconds |
| CPU quota periods throttled | about 62% | about 0.13% |

The candidate did more useful work while using less database CPU. Origin reads
increased from 30,158–33,057 to 69,536 because all walks and searches progressed;
the two replicas' origin counts differed by less than 0.5%. Thus the gain was
not created by hiding more requests in cache, routing around a replica, rejecting
valid searches or dropping work. There were no OOM events in any final trial.

Slow baseline request diagnostics show database query waits dominating, with
pool acquisition delay added as concurrency grew. Publishes then waited for the
same database/pool resources. After search arrivals stopped, late fully observed
samples had only zero or one pending acquisition in both variants. Baseline
walks still unfinished at the deadline remain incomplete in the results.

All four final trials passed clock/authentication validity checks. The runner
completed successfully, repeated the response-equivalence check, and removed
its container and volume. The dedicated local VM was then stopped.

## Controls and correctness

An initial 50-search/sec run with the same diverse crawlers completed all 30
walks and all 3,000 searches: there was no sustained overload at that lower
offered rate. At 200 searches/sec, the baseline completed only 18 walks and
dropped 2,746 searches in the first exploratory run. A search-specific custom
planning experiment also completed only 18 walks and retained multi-second
latency; it was rejected. The suffix-index candidate completed all offered work
both with and without forced custom planning, so the final implementation omits
the planning override.

The pre-comparison equivalence check matched full response hashes for 356 cases
and 526 responses per variant, across both API versions, up to three cursor
pages, latest/exact versions, deletion/update filters, case variants, partial
namespaces, and literal metacharacters. The final runner repeats that check
after timing. Database regression tests independently compare results against
literal substring matching with pagination, latest and deletion filters.

## Scope of the conclusion

This proves a repeatable local fix for the reproduced search-driven overload,
with more completed useful work rather than fewer accepted requests. It links
to an expensive query shape already observed in production. It does not prove
that every historical alert had this cause, that production can sustain a
particular RPS multiplier, or that a system remaining stuck after load drops has
the same explanation. In these runs the pool queue cleared after the burst;
unfinished/cancelled crawls are still counted as failures to complete the work.

The fixture's namespace/version distribution is synthetic; the local database
uses ARM64, PostgreSQL 17.11 and C collation. Production evidence came from
PostgreSQL 17.5 on x86-64. The VM, storage, network, application CPU, retries and
client mix differ. Broad searches without a namespace remain a separate cost.
External publisher ownership checks are excluded. These limits remain even when
the local candidate completes all work.

Migration 016 and the [deployment procedure](../../deploy/README.md#namespace-suffix-index)
describe concurrent index creation and verification before an existing busy
database receives the application change. No such operation was performed in
production or staging during this work.

## Reproduce

See the [runner instructions](README.md#constrained-database-and-diverse-clients).
The runner retains versions, source/binary hashes, configuration, per-second
pool samples, per-crawl results, SQL counters and cgroup CPU/memory events, then
removes its own container and volume. Clock-invalid trials are retained and
excluded with bounded retries. Never modify the runner during execution.


## Retained evidence and validation

[Machine-readable results](results/2026-10-27-constrained.json) contain the final
balanced trials, exploratory controls, source/binary hashes, resource settings,
latency/status/drop counts and cgroup deltas. A hash identifies the retained raw
archive containing per-second samples, every crawl, SQL plans and counters,
application diagnostics, cache configurations and equivalence hashes.

Validation passed: full database and API handler test packages against isolated
PostgreSQL, race-enabled database search and benchmark tests, category/replica
balance tests, repository-wide golangci-lint (zero issues), `go build ./...`,
`make validate`, Python compilation and shell syntax checks. The fresh-database
runner exercised migration 016 and cleanup end to end. No production/staging
release, migration or configuration change was performed.
