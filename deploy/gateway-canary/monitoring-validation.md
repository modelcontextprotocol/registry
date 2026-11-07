# Monitoring follow-up: 2026-11-08

The existing staging canary now reconstructs GKE CRI fragments and records
retry/group timing totals while preserving the single-attempt histograms. The
existing VictoriaMetrics and Grafana stack remains in use. No production apply
was run and no nodes were added. Machine-readable evidence is in
[monitoring-validation.json](monitoring-validation.json); semantics and repeat
commands are in [monitoring.md](monitoring.md).

## Results

| Check | Observed result |
| --- | --- |
| Actual Vector 0.61.0 local pipeline | Split JSON marker, 300 KB record, interleaved files/streams, retry groups, missing/invalid timings, background refresh, malformed JSON, timeout and fragment bound passed |
| Real GKE CRI splitting, both collectors | 300 KB records produced actual `P` fragments; each run reconciled 10 client requests, two background refreshes, six all-attempt timing observations and two deliberately invalid timing records |
| Exporter → VictoriaMetrics | Exact totals matched; final synthetic client total was 60,020 after the contract and rate checks |
| Grafana dashboard | All 16 live panels match the repository; all 60 queries across registry/fixture/Grafana selections executed successfully, with no NaNs. Thirteen queries had no matching series, such as absent errors |
| Alert notification path | Synthetic access records drove a real Grafana rule to firing and then resolved; the internal webhook received both notifications |
| Notification cleanup | Test rule, contact point, mount, ConfigMap, Service and receiver Pod removed; original Grafana template and notification policy restored |
| Monitoring health | Seven canary scrape targets healthy; zero parser/CRI errors in the retained collectors |
| Capacity | Same four cluster nodes throughout, including the same two canary nodes; canary autoscaling now capped at two |

The initial 48 KB staging input did not produce partial records. Runtime probing
found GKE uses 256 KiB payload fragments (observed CRI line length 262,184 bytes),
which also exposed the old 64 KiB source limit as insufficient. The corrected
262,400-byte limit admits the envelope. Four fragments cap each buffered group
at 1,049,600 bytes; incomplete groups expire after five seconds. The revised
300 KB test passed on both nodes.

## Collector resource check

Each collector ingested 500 synthetic access records/second for 60 seconds,
sequentially, in one-second batches. Each counted all 30,000 records. Kubelet
cumulative CPU and working-set samples gave:

| Collector run | Average CPU cores | Peak working set |
| --- | ---: | ---: |
| First node | 0.139 | 45.3 MiB |
| Second node | 0.092 | 46.0 MiB |

Vector remains single-threaded with 500m CPU / 128 MiB requests and a 512 MiB
memory limit. This check fits the existing reservations; it is a log-ingestion
check, not a new simultaneous HTTP gateway comparison or production sizing proof.
Managed GKE upgrade surge settings were not changed.

## Existing dashboards and alerts

Read-only production inventory found two dashboards and four application alerts.
They use application metrics, Kubernetes events and resource metrics, with no
`nginx_ingress_controller_*` dependency. No existing query needs rewriting for
the gateway. The four alert queries evaluated successfully: availability 100%,
service up 1, no slow-publish threshold hits, and no server-list latency threshold
hits. Their production configuration and existing Discord destination were not
changed.

Staging had no persistent Grafana alerts and only the default email contact, with
SMTP unconfigured. The temporary proof exercised the actual log-to-notification
path without sending to an external recipient. Cleanup restores the original
staging state; this does not establish ongoing staging on-call coverage.

The production Pulumi preview contained only the existing two expired ingress
admission Jobs and two Grafana empty-RBAC normalizations (188 unchanged resources),
with no gateway/application changes. Production was not applied.

## Remaining limits

- Actual Discord/email delivery remains a cutover check; an internal webhook
  receipt is not proof of delivery to a person.
- Retry-list parsing is validated using controlled access records. This is not
  an upstream-failure routing test.
- Browser-rendered Grafana OAuth, authenticated publishing, independent public
  and internal certificate lifecycles, safe IP/DNS handoff, production traffic
  mix, node failure and rollout headroom remain separate cutover gates.
- File checkpoints and partial-message buffers do not provide exactly-once
  delivery across crashes or node loss.

The original ingress remains the active public path and rollback option. The
normal release workflow and staging image selection are unchanged.
