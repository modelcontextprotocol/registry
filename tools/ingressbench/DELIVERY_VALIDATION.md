# Generator calibration and DB isolation — 2026-10-28

**The generator needed more headroom, and real app CPU contention remains.
Neither staging layout passed the short gate. Production was unchanged.**
The conditional ten-minute burst with publishes and thirty-minute recovery was
not run. No app autoscaler or permanent dedicated DB node was introduced.

## Resolve the measurement uncertainty first

The instrumented driver now records scheduler, worker-queue, connection, TLS,
first-byte and response-transfer timings by request kind and bounded cache
outcome. Scheduled-to-completion latency remains available, so client delays
are not silently excluded. CPU profiles are optional and remain local.

An explicit calibration mode serves 64-KiB JSON responses over loopback HTTPS,
using the same request scheduler, workers and 30-second idle-connection rotation.
It cannot publish or collect a staging catalog. Calibration used 4,500 reads/s
for 90 seconds, 1,024 workers and the saved catalog.

| Generator host | Scheduled requests | Dropped before HTTP | Attempt failures | Page p95 |
| --- | ---: | ---: | ---: | ---: |
| Four vCPUs, fixture and client together | 405,000 | 25,918 (6.40%) | 0 | 63.9 ms |
| Sixteen vCPUs, fixture and client together | 405,000 | 0 | 0 | 1.55 ms |

The small host stalled at startup and around connection rotations even without
the registry. Its CPU profile includes both client and fixture work; TLS and
runtime contention are visible. The larger temporary generator passed with the
same connection policy. This establishes a local delivery limitation and a
passing local control, not an independent external-network capacity guarantee.
It does not prove that all losses in earlier staging tests came from the driver.
The instrumented binary was refactored into helpers for lint between calibrations;
request generation behavior was unchanged. Exact binary hashes are recorded.

Both subsequent staging arms used the same sixteen-vCPU generator and identical
instrumented binary. The generator stayed outside the tested node pool and was
deleted afterward. Staging node types and counts were not increased for this
generator change.

## Two bounded staging tests

Both used the existing prepared-JSON app image from source `43e17e0`, four apps
at 400m CPU request each, no app CPU limit, pools of 30 maximum / 5 minimum each,
six ingress controllers and four nodes. PostgreSQL retained its three-CPU quota.
The total app pool budget remained 120 maximum / 20 minimum connections.

Each test had two minutes at 100 reads/s, three minutes at 4,500 reads/s and two
minutes at 100 reads/s. The burst included 36 updated listings/s and three
searches/s within the total, HTTP/1.1, 4,000 keys and an 80% hot-key mix. Neither
short test published data.

The first layout spread one app per node, sharing nodes with ingress and the DB.
The second left PostgreSQL on its existing node and excluded apps and ingress
from that node. Six ingress replicas were spread two per workload node; four
apps occupied those three nodes. Temporary affinity and three-domain spread
constraints held the layout at four nodes. With local ingress traffic policy,
the three workload-node load-balancer backends were healthy and the DB node was
intentionally excluded. PostgreSQL was not restarted or moved.

| Measurement | Shared nodes | DB isolated, same four nodes |
| --- | ---: | ---: |
| Scheduled reads | 810,000 | 810,000 |
| Reads dropped before HTTP | 8,641 (1.067%) | 23,426 (2.892%) |
| Attempt failures | 9 timeouts | 0 |
| Page p95, scheduled to completed | 328 ms | 414 ms |
| Search p95, scheduled to completed | 3,369 ms | 1,635 ms |
| Updated-listing p95, scheduled to completed | 3,935 ms | 1,291 ms |
| Largest sampled pool queue on one app | 758 | 460 |
| Highest measured app CPU-stalled fraction | 39.1% | 30.7% |
| Dominant DB statement mean | 1.72 ms | 0.63 ms |
| Short recovery: drops / attempt failures | 0 / 0 | 0 / 0 |

Each layout was tested once. Isolation also changes traffic distribution and
concentrates apps/ingress onto three nodes. These results describe that complete
placement change, not a pure measurement of PostgreSQL isolation alone.
Comparison against older four-vCPU-generator runs additionally involves changed
node/pod placements and must not assign all differences to generator CPU.

## What the additional measurements establish

On the larger generator, mean CPU busy was 18.4% / 21.7% across sixteen vCPUs;
mean received traffic was about 2.12 / 2.07 Gbit/s. No interface drops were
recorded. Scheduler timing was much tighter than in the earlier runs. This
still does not rule out every network or client limitation, but the server-side
queue and CPU-pressure evidence establishes a real remaining server problem.

In the shared layout, the most affected app was on a node **without PostgreSQL**.
Its mean page pool-acquisition time was 1,632 ms and checkout time 128 ms, while
the healthiest peer averaged about 1 ms and 5 ms. The affected app spent 39% of
its bounded cAdvisor interval fully CPU-stalled. Node CPU samples reached 90%
on that node and 97% on the DB node; interval averages can hide shorter peaks.

Client phases distinguish the slow path: shared-layout page cache hits averaged
about 59 ms of HTTP processing, while page misses averaged 788 ms, including
759 ms from request-written to first response byte. Updated-listing misses
averaged 575 ms to first byte. Connection acquisition and body transfer were
much smaller on average. First-byte time includes ingress, networking and
upstream queueing; it is not a pure application execution timer. Nine attempts
timed out: four pages and five updated listings.

DB isolation made the dominant observed SQL statement faster, but two app
replicas still reached sampled queues of 357 and 460 and approximately 31% CPU
stalling. Faster SQL therefore did not remove the application-side bottleneck.
The statement comparison covers 82,727 / 86,114 calls in bounded top-25 snapshot
deltas that include preparation and collection padding. That statement recorded
no shared-block reads or temporary-block writes. Neither captured run increased
the DB quota-throttling counter, and neither restarted an app or the database.

Both short recoveries completed all 12,000 reads without drops or failures, and
the final sampled app pool queues were zero. All 140 direct capture operations
per arm succeeded. These are short recovery observations, not full publishing,
autoscaling or idle-footprint validations.

## Decision

Keep the PR in draft. Four app replicas alone and dedicated DB placement alone
have not met the offered-load requirement. Do not increase the database pool
or promote either layout on these results. Do not run the conditional long test
after a failed short gate.

The next design change should address CPU allocation and placement for apps
sharing busy nodes with ingress. Raising an app CPU request requires checking
actual scheduling budgets and the intended idle node count; it is not equivalent
to having a dedicated core. Additional replica counts or a DB-only node do not
by themselves establish that CPU will be available where requests land. The
specific resource/placement change still needs validation; this report does not
claim a proven production fix.

## Validation and cleanup

`go test -race ./tools/ingressbench` and `make lint` passed. Tests verify timing
separation, connection reuse, missing-event handling, fixture size, bounded
cache labels and rejection of publishing/catalog collection in calibration
mode. [CI at driver source `08077e4`](https://github.com/modelcontextprotocol/registry/actions/runs/40715012264)
passed, as did CodeQL. The app image itself was unchanged.

The temporary generator VM and disk were removed. The original staging app
image/resources, two replicas, app and ingress affinity/spread constraints,
rollout strategies, ingress HPA specification and rate-limit annotation were
restored. All 48 sampled response hashes matched the pre-test responses. No test
publications were created. At the final 15:51 UTC snapshot, four nodes and six
ingress replicas remained; ingress HPA bounds were restored to 2–6 and node-pool
bounds remained 2–4. Natural return to two nodes has not been demonstrated.
Restoration verification is recorded alongside the portable results.

Portable measurements are in
[evidence/delivery-validation-2026-10-28](evidence/delivery-validation-2026-10-28).
Raw client results, profiles and cluster samples remain in the local
`delivery-validation-2026-10-28` evidence directory. Private deployment snapshots
and raw profiles are not uploaded to the repository.
