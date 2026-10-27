# Staging autoscaling experiment — 2026-10-27

**Neither autoscaling candidate passed. Do not promote these settings as a
production performance fix.** Database CPU protection helped, but client-visible
publish timeouts and application pool queues remained.

All three runs used the same application image, benchmark binary and saved
cursor catalog, starting from two Ready `e2-standard-4` nodes and two ingress
replicas. Each offered ten minutes of 4,500 reads/s, including 36 uncached
incremental-sync reads/s and three varied searches/s, plus one independent
publish/s. The read concurrency and queue were each bounded at 1,024. Actual
traffic was lower when that queue filled; dropped work is reported separately
from attempted-request failures. All runs explicitly used HTTP/1.1.

| Measurement | Fixed capacity | Ingress/node autoscaling | Autoscaling + 60s client connections |
| --- | ---: | ---: | ---: |
| Burst start, UTC | 21:07:43 | 21:43:20 | 22:25:39 |
| Offered reads | 2,700,000 | 2,700,000 | 2,700,000 |
| Dropped reads at generator | 666,282 (24.68%) | 631,279 (23.38%) | 976,541 (36.17%) |
| Read transport/body failures | 13 | 625 | 435 |
| Acknowledged publishes | 600/600 | 593/600 | 596/600 |
| Publish client timeouts | 0 | 7 | 4 |
| Publish p95, client measurement | 2.06 s | 8.98 s | 8.44 s |
| Page p95 / p99, client measurement | 0.93 / 1.44 s | 0.82 / 6.65 s | 1.34 / 7.49 s |
| Peak sampled pool waiters, total | 294 | 1,259 | 915 |
| Peak sampled DB CPU pressure, some avg10 | 85.55% | 30.05% | 19.88% |
| Largest observed node / ingress count | 2 / 2 | 4 / 6 | 3 / 5 |

Client latency runs from intended dispatch to body completion, including queue
delay and failures. The server histogram p95 is a different measurement. HTTP
status 200 alone does not count a truncated response as successful. In the first
autoscaling run, 597 publish records persisted despite only 593 acknowledgements:
four timed-out requests committed. They remain ambiguous client outcomes.

The fixed-capacity run reproduced node CPU saturation, database CPU contention,
database readiness failures and application queues. Both candidates increased
the database CPU request from 200m to 2 CPUs, retaining the 3-CPU quota, 4-GiB
memory limit, 512-MB shared buffers and 200-connection ceiling. App replicas
remained at two, with each pool retaining max 60/min 10. Neither candidate's
sampled database endpoints became unready during its burst.

The second candidate exposed a scaling blind spot: with five ingress pods the
HPA measured 648m average CPU against its 704m target and held at five, while
the database node was at 101% of allocatable CPU. Another sample showed the two
apps receiving approximately 155 and 151 requests/s, but with zero and 875 pool
waiters respectively. The queued app shared the database node. Equal request
counts therefore did not imply equal available processing capacity. Query mix
and per-request cost still need consideration when comparing replicas.

Database statement deltas during the first candidate showed approximately
3.9 ms mean server execution for the dominant page query. Sampled slow-request
diagnostics attributed 86–90% of elapsed time to acquiring a pool connection.
These are different populations and timing boundaries: application query timing
also includes consuming rows. This supports investigating app CPU contention and
connection hold time; it does not prove that a larger pool would solve the issue.

The first candidate scaled naturally back to two nodes at 22:25:27 UTC, about
32 minutes after its burst ended. Database pods stayed on their existing nodes.
HPA stabilization, gradual pod removal and node draining all contributed to the
delay. Minimum node count is not an immediate post-burst cost guarantee.

## Reproduction and limits

Use the command and capture instructions in this directory's README. The fixed
baseline had earlier cache-warming calibration; the candidates started after
rollout/idle and added cold caches while scaling. Staging used the same main
catalog throughout, with additional small benchmark publications between runs;
it was not restored to a byte-identical database snapshot each time. Fifty
fresh-connection version probes were added during the first candidate to verify
new-backend reachability. The second candidate did not include those probes.

The generator remained outside the tested node pool. Its sampled utilization in
the fixed run was about 47% CPU on average, peaking near 53%; the bounded queue
still limited delivered traffic as responses slowed. These tests reproduce a
failure mode; they are not a replay of every production client or a guarantee at
4,500 delivered requests/s. Earlier HTTP/2 calibration had transport failures and
is not used as this comparison's baseline. A separate 100-request/s HTTP/2 smoke
test with the 60-second setting completed 9,000 requests without errors; that
does not validate high-load HTTP/2 behavior.

The comparison binary SHA-256 was
`28ec057a098b8d01a5c8d7fb7fed6cd2d3d89dc2f35c201dff1173c2e6a00150`.
The catalog SHA-256 was
`120f34ec070fe08acf093e65f13a28631d944cf510915c65233b9eab62c143be`.
Timestamped raw measurements are retained with the investigation artifacts.

The next candidate should address application CPU availability and bounded read
concurrency while retaining a database connection budget across the maximum app
replica count. It must repeat the same burst and protect publishing during
scale-up. Raising a pool limit or merely lowering the ingress CPU target is not
established as a solution by this evidence.
