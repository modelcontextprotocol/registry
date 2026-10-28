# Connection lifetime and app CPU validation — 2026-10-28

**Do not promote this candidate as a complete burst-capacity fix.** A larger
application CPU reservation eliminated attempted-request failures in this run,
but 4.67% of the offered reads still could not be dispatched. The two-node idle
target also remains unverified for this configuration.

## Changes and controls

`ListServers` now consumes a bounded page into owned JSONB byte slices and closes
the rows before decoding JSON. This releases a pooled connection before that
application work. A caller-owned transaction still retains its own connection.
SQL, filtering, ordering and cursor behavior are unchanged. Forty-eight direct
origin API response hashes matched the original implementation before each run.

The CPU comparison changed only the app CPU request from 250m to 1 CPU, using
the same connection-release image. Both runs retained two app replicas, pools
of max 60/min 10 per replica, a 1-GiB app memory limit, and no app CPU limit.
PostgreSQL retained its 2-CPU request, 3-CPU limit, 4-GiB memory limit, 512-MB
shared buffers and 200-connection ceiling. Ingress/node scaling settings were
unchanged from the preceding experiment.

Each burst offered 4,500 HTTP/1.1 reads/s for ten minutes, including 36 uncached
incremental-sync reads/s and three searches/s, plus one independent publish/s.
The same binary, 4,000-key catalog, 80% hot-key selection, 1,024 read workers and
1,024 queued reads were used. Recovery offered 100 reads/s and one publish/s for
30 minutes. Its driver added bounded failure timestamps without changing traffic
scheduling or transport settings. Each completed run's own benchmark records
were removed before the next comparison; the base corpus contained 187,108
versions and 52,366 server names.

## Client results

| Measurement | Connection release, 250m | Same image, 1 CPU |
| --- | ---: | ---: |
| Burst start, UTC | Oct 27 23:54:40 | Oct 28 00:42:40 |
| Offered reads | 2,700,000 | 2,700,000 |
| Reads dropped by the bounded generator | 471,814 (17.47%) | 125,976 (4.67%) |
| Attempted read failures | 269 | 0 |
| Acknowledged publishes | 597/600 | 600/600 |
| Publish timeouts | 3 | 0 |
| Publish p95 / p99 | 7.588 / 9.519 s | 1.350 / 1.924 s |
| Page p95 / p99 | 732 / 6,103 ms | 597 / 1,129 ms |
| Peak sampled pool waiters, total | 1,013 | 116 |
| Starting node count | 2 | 3 |
| Peak node / ingress count | 4 / 6 | 4 / 6 |
| Recovery reads succeeded | 180,000/180,000 | 180,000/180,000 |
| Recovery publishes succeeded | 1,800/1,800 | 1,800/1,800 |
| Recovery dropped work / failures | 0 / 0 | 0 / 0 |
| Recovery publish p95 | 13.9 ms | 14.2 ms |

Client latency includes scheduling delay through response-body completion. The
125,976 dropped reads were never dispatched because the bounded generator queue
was full; they are not HTTP failures, and must not be hidden behind the zero
attempted-request failure count. Two timed-out publishes in the 250m run committed
despite the client timing out: 599 records persisted, versus 597 acknowledgments.

The predeclared experimental gate required no failed or dropped work, page p95
at most one second and publish p95 at most two seconds, followed by clean recovery
and a repeat of a passing candidate. These are experiment criteria, not an
established production SLO. The 1-CPU run met the latency and attempted-request
criteria but failed the offered-load criterion. A passing repeat is therefore
not established.

## What the evidence establishes

The connection-only change was insufficient. Its dominant page statement
averaged 3.40 ms of PostgreSQL execution while pool queues grew and publishes
timed out. During a captured late-burst interval, the app sharing the database
node spent about 46% of wall time fully CPU-stalled according to its cgroup PSI
counter, versus about 13% for the other app. These samples are not a complete
burst average.

In the 1-CPU run neither app shared the registry database node. The dominant
page statement averaged 3.00 ms, pool queues were much smaller, and client
publish latency improved. One app still experienced substantially more CPU
contention than the other: approximately 27% versus 8% full CPU-stall time over
the captured burst intervals. The settings improved the observed outcome; this
does not isolate the effect of CPU priority from the effect of placement or
the extra node already available at burst start.

No increase in the database CPU-throttling counter was observed during either
burst, and no database endpoint loss or app/database restart was observed in
the successful burst snapshots. The 250m run recorded about 85 ms of additional
DB throttling late in recovery. These observations do not establish that larger
connection pools would help; the pool budget was deliberately unchanged.

## Idle capacity and observation gap

The 250m candidate naturally returned to two Ready nodes around 00:37:05 UTC,
approximately 32 minutes after its burst ended, without moving either app or
the registry database during the measured cycle.

The 1-CPU rollout required a third node before load started. Scheduler events
reported insufficient CPU on the database node and the app topology-spread
constraint on the other node. The database node could not fit a 1-CPU app while
retaining its existing ingress pod and other reservations. A node-pool minimum
of two therefore does not establish that this placement can return to two.

Google Cloud reauthentication expired during recovery. The last successful node
snapshot was at 00:55:34 UTC with four nodes; node snapshot failures began at
00:56:04 UTC. The remote benchmark nevertheless completed at 01:23:09 UTC and its
complete JSON result was captured in the SSH command's stdout. Copying that
result file back failed during subsequent authentication. Client recovery
results are available, but full recovery infrastructure telemetry and the final
idle node count require backfill and live verification after reauthentication.
Do not describe that missing telemetry as healthy or zero.

Production was not changed. Temporary staging resources and the experimental
CPU/image settings require cleanup after authenticated access is restored.

## Reproduction

Application source: `3ea1fa2e974d97bff88cd0f04dc95fac5dea5f68`.
Image digest: `sha256:94f184cd38345b059f440756d5eaa22448dbc7d827490220d144bd9356713f94`.
Burst driver: `28ec057a098b8d01a5c8d7fb7fed6cd2d3d89dc2f35c201dff1173c2e6a00150`.
Recovery driver: `c747ec90766f5044632b08019d4fb5d32dbb36f10b73dff02821ba83263b0006`.
Catalog: `120f34ec070fe08acf093e65f13a28631d944cf510915c65233b9eab62c143be`.

Full internal/cmd race tests, database/telemetry tests, infrastructure Go tests
and root lint passed. GitHub checks passed at `3ce54f9`. Those checks establish
code validation, not a successful capacity test. See the adjacent
`results/2026-10-28-connection-cpu.json` for portable client measurements and
`README.md` for benchmark commands and measurement boundaries.
