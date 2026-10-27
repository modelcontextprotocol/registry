# Local read-path results — 26 October 2026

**The strongest application/database improvement tested was more detailed `updated_at` statistics for selective incremental reads. It is not a demonstrated fix for the full production incident or a 10× capacity claim.** Longer cache-fill coordination also prevented duplicate origin work in a controlled slow-origin experiment. No production or staging changes were made.

## Reproduce

```sh
READBENCH_DURATION=10s ./tools/readbench/run-local.sh /tmp/registry-readbench-results
```

See [the harness documentation](README.md) for prerequisites and safeguards. [Machine-readable evidence](results/2026-10-26.json) contains every final trial, build/fixture hashes, database settings, statement deltas and plan summaries. The final run completed from a fresh database through cleanup without invalid trials or retries. Earlier trials invalidated by host clock jumps were excluded; the harness detects this condition and preserves failed attempts separately.

Environment: macOS arm64, 12 logical CPUs, 32 GiB memory; Go 1.27.3; PostgreSQL 17.11; NGINX 1.31.8. The real application router/service/database ran with `GOMAXPROCS=2`; PostgreSQL had 512 MiB shared buffers. This does **not** reproduce production's Linux CPU quotas or node contention. Fixture: 185,001 versions, approximately 241 MiB heap, synthetic names and deterministic update times over 30 days. External ownership-provider calls are disabled in the benchmark.

## Database statistics: a selective-query improvement

Each case offered 600 incremental reads/sec or 1,800 mixed reads/sec, plus one publish/sec. Statistics targets ran in order **100 → 1000 → 1000 → 100**, with five-second warm-ups and ten-second measured trials. All 96,000 measured reads and 80 publishes succeeded, with zero driver drops. This establishes completion at the offered rates, not maximum capacity.

| Workload / measurement | Statistics 100, two repeats | Statistics 1000, two repeats |
|---|---:|---:|
| One-hour incremental HTTP p95 | 17.48 / 17.90 ms | 1.71 / 1.67 ms |
| One-hour listing mean DB execution | 11.35 / 11.49 ms | 0.553 / 0.553 ms |
| One-hour shared buffer accesses/call | 9,915 / 9,924 | 228 / 228 |
| Mixed HTTP p95 | 2.42 / 2.52 ms | 2.40 / 2.47 ms |
| Mixed mean DB execution | 0.684 / 0.645 ms | 0.686 / 0.645 ms |

The narrow query improved approximately **90% at HTTP p95**, used approximately **43× fewer shared buffer accesses**, and had approximately **20× lower mean database execution time**. Statement deltas exclude publish uniqueness lookups. Database execution time is elapsed statement time, not a direct CPU measurement. The first baseline had some shared-buffer reads; the final baseline had none and remained slow, so this is not explained solely by warming the cache.

The index already exists. For a representative one-hour filter, statistics 100 selected a name-index scan, filtered 72,333 rows and touched 12,308 buffers. Statistics 1000 selected `idx_servers_updated_at` plus a sort, touching 287 buffers. Increasing the statistics target improves the planner's distribution estimate; it also increases `ANALYZE` work. [PostgreSQL planner statistics documentation](https://www.postgresql.org/docs/17/planner-stats.html).

Important limits:

- The seven-day mixed workload showed **no meaningful improvement**. Much of the observed production incremental traffic had older watermarks, so the selective result cannot be applied to that whole workload.
- One-day and one-week plans were also captured; they did not exhibit the large one-hour improvement.
- Forced **generic prepared plans remained slow** with either statistics target (approximately 20–23 ms for the representative one-hour query). Custom plans benefited. The real HTTP benchmark used the application's normal pgx behavior and improved, but production's actual prepared-plan choice must be checked before treating this as its fix.
- Earlier valid 20-second trials also reproduced the selective improvement, while mixed-load results varied and one mixed candidate run dropped requests. Those do not support a broader throughput claim.
- Every final publish succeeded, but ten samples per trial do not establish publish-tail resilience; one candidate trial had publish p95 around 40 ms. No publish-latency improvement is claimed.

## Cache coordination: duplicate work prevented

Four fresh-cache trials compared lock timeout **and lock age** at 5 → 10 → 10 → 5 seconds. Each offered 800 reads across eight repeating URLs, plus eight publishes. A benchmark-only wrapper delayed each origin read by six seconds before running the real handler. Cache TTL stayed 30 seconds.

| Measurement | Five-second locks, two repeats | Ten-second locks, two repeats |
|---|---:|---:|
| Origin reads for 800 successful requests | 112 / 112 | 8 / 8 |
| Read p95 | 11.272 / 11.274 seconds | 6.003 / 6.002 seconds |
| Successful reads / publishes per trial | 800 / 8 | 800 / 8 |

That is **93% fewer origin reads** without rejecting work. NGINX permits another cache-fill request after lock age expires; requests can also escape to the origin after lock timeout. Both matter. [NGINX cache lock documentation](https://nginx.org/en/docs/http/ngx_http_proxy_module.html#proxy_cache_lock).

This is a mechanism test with an artificial delay and one cache process. It does not establish the right production timeout or a capacity multiplier. Independent ingress caches each need fills. Distinct incremental URLs still miss; longer locks cannot combine them. Longer waiting also trades off against client deadlines when an origin is hung.

## Correctness and rejected candidates

Complete response hashes matched at both statistics targets for 18 cases across `/v0` and `/v0.1`: ordinary/deep pagination, broad/narrow incremental filters, search, latest/exact versions, deleted records and empty results. Checks follow up to three pages and verify ordering, cursor progression, counts and deletion/version semantics.

An early-release-of-database-connection prototype showed no convincing benefit in an exploratory saturation test: approximately 2,198 versus 2,212 successful reads/sec and unchanged approximately 141 ms read p95. The screening was not a controlled final comparison and cannot establish equivalence; there was insufficient evidence to retain the code. A two-stage key/payload query was also not faster in initial SQL screening. Neither is included as an application change.

Validation: race-enabled harness tests, `go vet ./tools/readbench`, repository-wide golangci-lint 2.14.0, `go build ./...`, and the complete local runner passed. An independent review found no remaining blockers in measurement safety or the scoped interpretation.

## Recommendation

1. Treat statistics 1000 as the best-supported **selective-query candidate**, gated on production's actual watermarks, estimates and prepared-plan choice. The local comparison used `ALTER TABLE servers ALTER COLUMN updated_at SET STATISTICS 1000` followed by `ANALYZE servers(updated_at)`; no production migration is included here.
2. Treat longer cache-fill coordination as a separate candidate for repeated URLs. Test realistic miss-duration and client-deadline ranges before choosing production values.
3. Keep the broad-read scaling problem open. These results do not justify larger connection pools, a broad capacity claim, or another production rollout presented as a complete fix. Use this harness to require equal-work throughput, error/drop counts and publish behavior from the next candidate.
