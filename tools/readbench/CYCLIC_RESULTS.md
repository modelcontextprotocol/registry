# Cyclic crawl benchmark — 2026-10-27

The three-character search minimum rejects short searches before service/database work. It **did not demonstrate a bulk-crawl capacity improvement**. Both versions completed the same crawl workload, including the local 10× scenario. These runs did not reproduce the sustained production queueing incident.

## Measured workload

Two app processes (`GOMAXPROCS=2` each), two independent NGINX cache processes, and PostgreSQL 17.11 ran on one macOS arm64 machine with 12 logical CPUs and 32 GiB RAM. PostgreSQL used 512 MiB shared buffers, statistics target 100 and automatic plan selection. Caches used a 30-second TTL and five-second lock settings. No artificial origin delay was injected. No staging or production resources were changed or load-tested.

The namespaced fixture contained 185,001 versions, three per name, approximately 1% deleted records, repeated namespace trigrams and rare late-sorting matches. Each trial began with fresh app processes and empty edge caches. A three-second search warm-up reached the database through the first app only; neither app received crawler warm-up.

Three synchronized waves began at 0, 20 and 40 seconds, irrespective of earlier completion. Within each wave, 60% of clients walked all versions, 30% latest versions, and 10% unique seven-day incremental listings. Clients followed actual cursors to exhaustion, checking ordering, duplicates, counts and latest-version filtering. Background searches and publishing continued for 90 seconds; observation/draining ended at 100 seconds. Publishing exercised the local authorization, validation and database write path, excluding external ownership-verification services.

The reference scenario used 20 crawlers per wave, a shared ceiling of 1,200 crawl request starts/sec, 10 searches/sec and one publish/sec. The stress scenario used 200 crawlers per wave, a 12,000/sec crawl-start ceiling and 100 searches/sec; publishing stayed at one/sec. The search mix had 1% short terms and a small repeating set of longer terms. The measured short probes were `a`; two-character, whitespace and Unicode behavior is covered separately by handler tests.

## Results

Reference runs used A/B/B/A order. Ranges below are the two separate trial results, not pooled quantiles. Stress results are one baseline/candidate pair and are descriptive, not a statistically established regression or improvement.

| Scenario and version | Complete walks | Crawl pages, HTTP 200 | Page p95 | Accepted search p95 | Publish p95 | Last walk finished |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Reference baseline, each trial | 60/60 | 79,546 | 1.81–1.83 ms | 2.39–2.56 ms | 7.89–8.26 ms | 66.30–66.31 s |
| Reference candidate, each trial | 60/60 | 79,546 | 1.73–1.76 ms | 2.63–3.24 ms | 7.94–8.28 ms | 66.29 s |
| 10× baseline | 600/600 | 795,460 | 51.73 ms | 42.52 ms | 44.79 ms | 70.74 s |
| 10× candidate | 600/600 | 795,460 | 52.96 ms | 43.81 ms | 53.50 ms | 70.66 s |

Every valid trial completed 90/90 publishes, with zero unexpected HTTP failures and zero driver drops. Reference trials each completed 891 accepted longer searches, and stress trials each completed 8,910. Candidate trials separately returned nine and 90 expected short-search `422`s respectively. Policy rejections are excluded from useful read throughput. Page latency excludes deliberate crawler pacing; full-walk time includes pacing, and search/publish latency includes scheduling delay.

The reference waves overlapped: the slowest walks took roughly 26, 31 and 26 seconds by wave. One-second completed-read buckets peaked at 1,212–1,223/sec for reference and 12,233–12,276/sec for stress; these are peaks, not sustained throughput claims. Completed requests can bunch into a bucket above the request-start ceiling.

Both app metric endpoints were observed in 99 samples per trial. Sampled pending pool acquisitions peaked at zero in all reference trials, one in the stress baseline and zero in the stress candidate. Every fully observed sample after 80 seconds had zero pending acquisitions. Sampling cannot exclude shorter queue spikes. Missing final deadline samples were excluded, not interpreted as zero queues.

## What this proves, and what it does not

The guard prevents one- and two-character searches from reaching the service/database in both API versions, with unchanged handling of empty searches and accepted search strings. This is an API compatibility change: previously accepted short searches now receive `422`. The registry UI also suppresses these requests and prompts for three characters. It is a bounded protection, not a complete search optimization; three characters can still yield unselective or poorly indexed predicates, including punctuation-heavy terms.

The benchmark validates complete cyclic walks and concurrent publishing under this local model. It provides **no evidence that the minimum fixes bulk crawling**, and no reason to call the small reference latency differences a capacity gain. Stress candidate publish p95 was higher in the single pair; that result is retained rather than hidden by a favorable aggregate.

Cache reuse materially limits the interpretation of the 10× result. Reference cache HIT share was about 80.2–80.3%, with 15,850–15,855 total origin reads per trial. Stress HIT share was about 95.2%, with 38,435–38,456 origin reads. Ten times the client crawl workload therefore produced only about **2.4× origin reads**. The two app processes received nearly equal origin counts, as configured by the local round-robin proxies; that does not establish production load-balancer behavior.

Most search terms repeat through the same caches, all crawler URLs use `/v0.1`, canonical parameter ordering and limit 100, and the fixture fits comfortably in the host's memory. The model does not reproduce production search diversity, mixed API versions/default limits, cgroup quotas, physical nodes, retries, exact data skew or client arrival distribution. Consequently, it does **not** establish that production can sustain 10× origin/database work or that the incident's root cause is resolved.

The next optimization comparison should retain these complete crawls while varying cache-key diversity and adding uncached, selective and unselective search cases. Measure origin query work and publisher latency under constrained resources before selecting a database/plan/cache change. Do not infer a production fix from a quiet window or from this cache-friendly stress result.

## Evidence and reproducibility

[Machine-readable summary](results/2026-10-27-cycles.json) records each valid trial, binaries/source hashes, versions, configuration, latency, status, cache and origin counts. The baseline source was commit `e19fda006538296bbdb63be1fb22195d66813928`; the measured candidate adds the API minimum-search guard. UI request suppression and CLI help text were added after timing; they do not change the HTTP/API workloads measured here. The raw-evidence archive hash is included in the summary. Raw local evidence retains per-second samples, each crawl, database counter snapshots, cache configuration and the rejected trial.

One initial reference baseline trial had a +30.19-second host clock discontinuity; the runner retained and excluded it, then retried. The four valid reference trials and their comparison file completed successfully. A subsequent shell-wrapper trailer failed because the script had been edited during execution; its cleanup still stopped the owned database. The corrected, unmodified wrapper completed the later stress run with exit status zero and stopped its database. Never edit the runner while it is executing.

```sh
./tools/readbench/run-cycles.sh /tmp/registry-cycles-reference
READBENCH_CRAWLERS=200 READBENCH_CRAWL_RPS=12000 \
  READBENCH_SEARCH_RATE=100 READBENCH_ORDER='baseline candidate' \
  ./tools/readbench/run-cycles.sh /tmp/registry-cycles-stress
```

Local validation passed: repository-wide Go lint and build; full API handler tests against isolated PostgreSQL; race-enabled handler-policy and benchmark tests; schema/example validation; Python compilation and shell syntax checks. This evidence does not include a deployment or a production load test.
