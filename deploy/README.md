# MCP Registry Kubernetes Deployment

This directory contains Pulumi infrastructure code to deploy the MCP Registry service to a Kubernetes cluster. It supports deploying the infrastructure locally (using an existing kubeconfig, e.g. with minikube) or to Google Cloud Platform (GCP).

## Quick Start

### Local Development

Pre-requisites:
- [Pulumi CLI installed](https://www.pulumi.com/docs/iac/download-install/)
- Access to a Kubernetes cluster via kubeconfig. You can run a cluster locally with [minikube](https://minikube.sigs.k8s.io/docs/start/).

1. Ensure your kubeconfig is configured at the cluster you want to use. For minikube, run `minikube start && minikube tunnel`.
2. Run `make local-up` to deploy the stack.
3. Access the repository via the ingress load balancer. You can find its external IP with `kubectl get svc ingress-nginx-controller -n ingress-nginx`. Then run `curl -H "Host: local.registry.modelcontextprotocol.io" -k https://<EXTERNAL-IP>/v0/ping` to check that the service is up.

#### To change config

The stack is configured out of the box for local development. But if you want to make changes, run commands like:

```bash
PULUMI_CONFIG_PASSPHRASE="" pulumi config set mcp-registry:environment local
PULUMI_CONFIG_PASSPHRASE="" pulumi config set mcp-registry:githubClientSecret --secret <some-secret-value>
```

#### To delete the stack

`make local-destroy` and deleting the cluster (with minikube: `minikube delete`) will reset you back to a clean state.

### Production Deployment (GCP)

Staging and production use OSS NGINX Gateway Fabric with Gateway API, Vector,
cert-manager, VictoriaMetrics and Grafana. The [completed production migration](gateway-canary/production-completion.md)
records the cutover, final checks and recovery constraints. The active public
Service, IP, certificate and metric names retain their historical names for stability.
Local development still uses ingress-nginx.

**Note:** Deployments are automatically handled by GitHub Actions with separate workflows for staging and production:

- **Staging:** All merges to the `main` branch automatically build and deploy the latest code to the `staging` environment via [deploy-staging.yml](../.github/workflows/deploy-staging.yml). The deployment uses the digest returned by that build; it does not remain pinned to an old experiment image. Manual runs resolve the current published `:main` image to a digest once.

- **Production:** Deployment requires explicit configuration of the Docker image tag in `Pulumi.gcpProd.yaml` and is triggered automatically via [deploy-production.yml](../.github/workflows/deploy-production.yml) when this config file is pushed to `main`. This GitOps-style approach provides manual control and an audit trail for production versions.

**To deploy a specific version to production:**

1. Cut a release (if deploying a new version):
   ```bash
   # Via GitHub UI: https://github.com/modelcontextprotocol/registry/releases
   # Or via gh CLI:
   gh release create v1.2.3 --generate-notes
   ```
   This builds Docker images tagged as `1.2.3` and `latest` (note: image tags do not include the 'v' prefix)

2. Update the production image tag in `Pulumi.gcpProd.yaml`:
   ```bash
   # Edit deploy/Pulumi.gcpProd.yaml
   # Change line: mcp-registry:imageTag: 1.2.3  (note: no 'v' prefix for Docker image tags)
   git add deploy/Pulumi.gcpProd.yaml
   git commit -m "Deploy version 1.2.3 to production"
   git push
   ```

3. Wait for the Release workflow to finish publishing the image before merging the production tag change. The production deployment workflow then deploys the specified version. Publishing a GitHub release alone does not deploy production.

Both workflows now use the normal full-stack deployment path. Each operation
refreshes and previews the stack; applies refresh again and use that exact saved
plan. Preview refresh is read-only, so the apply refresh is necessary to recognize
resources removed by Kubernetes TTL cleanup rather than replacing stale checkpoint
entries. The plan still rejects changes outside what was previewed. GitHub
applies remain restricted to `main` and the existing environment-scoped
credentials/protections. Empty Grafana RBAC rules and detached public Certificate
owner lists are omitted to match Kubernetes' stored representation. The deployment
runner retains the fixed-name Helm admission Job reference handling for a reviewed
legacy recovery; these Jobs are absent from the current hosted stacks.
The one-off capacity adoption, resize, compression and security-upgrade wrappers
have been retired. Use the normal full-stack commands below for future changes.
`securityPatch202611: true` still selects the patched component versions; keep it enabled.

`REGISTRY_STAGING_IMAGE` supplies the current build digest to staging without
editing committed stack configuration. An optional `stagingImage` configuration
value overrides it for an intentional diagnostic freeze and must be digest-pinned.
Normal staging has neither that override nor `registryRolloutCommit`, so a new
main commit deploys automatically. Production keeps its explicit `imageTag`;
its existing rollout annotation pin does not prevent image changes from rolling.

Run `make staging-preview` and `make prod-preview` from this directory for read-only
previews. `PULUMI_CONFIG_PASSPHRASE_FILE` can point to an existing local passphrase
file; otherwise the scripts use `passphrase.staging.txt` / `passphrase.prod.txt`.
The staging preview resolves the current published main image unless
`REGISTRY_STAGING_IMAGE` is supplied; an explicit diagnostic config override
takes precedence over the resolved image. Preview
logs show operations and resource identities; raw plans/state stay private and
are deleted after the operation. No plan containing private state is uploaded.

See [release-flow-validation.md](release-flow-validation.md) for the staging and
production previews performed when restoring this flow.

**Manual Override:** The steps below are preserved if a manual deployment override is needed.

Pre-requisites:
- [Pulumi CLI installed](https://www.pulumi.com/docs/iac/download-install/)
- A Google Cloud Platform (GCP) account
- A GCP Service Account with appropriate permissions

1. Create a project: `gcloud projects create mcp-registry-prod`
2. Set the project: `gcloud config set project mcp-registry-prod`
3. Enable the bootstrap APIs needed before Pulumi can run. The remaining APIs
   (`cloudresourcemanager`, `compute`, `container`, `logging`, `monitoring`) are
   adopted as Pulumi-managed `projects.Service` resources by `ensureRequiredAPIs`
   in `pkg/providers/gcp/provider.go`, so they self-heal against drift but still
   need to be reachable on the very first deploy:
   ```bash
   gcloud services enable storage.googleapis.com         # for the Pulumi state bucket below
   gcloud services enable cloudresourcemanager.googleapis.com  # for the IAM bindings Pulumi creates
   gcloud services enable container.googleapis.com       # for the GKE cluster
   ```
4. Create the Pulumi service account and grant it the roles required to manage
   the deploy. `projectIamAdmin` is needed because the deploy creates IAM bindings
   on the project (for the default compute SA). The other four are general
   resource management:
   ```bash
   gcloud iam service-accounts create pulumi-svc
   sleep 10
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/container.admin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/compute.admin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/storage.admin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/storage.hmacKeyAdmin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/resourcemanager.projectIamAdmin"
   gcloud iam service-accounts add-iam-policy-binding $(gcloud projects describe mcp-registry-prod --format="value(projectNumber)")-compute@developer.gserviceaccount.com --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/iam.serviceAccountUser"
   gcloud iam service-accounts keys create sa-key.json --iam-account=pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com
   ```
5. Create a GCS bucket for Pulumi state: `gsutil mb gs://mcp-registry-prod-pulumi-state`
6. Set Pulumi's backend to GCS: `pulumi login gs://mcp-registry-prod-pulumi-state`
7. Get the passphrase file `passphrase.prod.txt` from the registry maintainers
8. Init the GCP stack: `PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt pulumi stack init gcpProd`
9. Set the GCP credentials in Pulumi config:
    ```bash
    # Base64 encode the service account key and set it
    pulumi config set --secret gcp:credentials "$(base64 < sa-key.json)"
    ```
10. Deploy: `make prod-up`
11. Access the repository via the ingress load balancer. You can find its external IP with: `kubectl get svc ingress-nginx-controller -n ingress-nginx`.
   Then run `curl -H "Host: prod.registry.modelcontextprotocol.io" -k https://<EXTERNAL-IP>/v0/ping` to check that the service is up.


## Ingress stability rollout

### Metrics storage capacity

`victoriaMetricsCapacity: true` reserves 250m CPU and 1 GiB memory for the
VictoriaMetrics server, with a 2 GiB memory limit. The previous 128/256 MiB
request/limit repeatedly OOM-restarted during the October 29 traffic cycle.
The profile retains the chart/image, storage volume and 14-day retention.
It does not change the registry application or its database.

For an infrastructure-only apply, `registryRolloutCommit` can pin the app's
existing `registry.modelcontextprotocol.io/deployCommit` pod annotation. Without
this setting, each new Git commit changes the annotation and restarts app pods
even when their image is unchanged. Update or remove the pin during the next
intentional app rollout; image and other pod-template changes still trigger a
rollout independently. A mutable image tag requires an updated rollout commit.

Internal caches use a 40% memory allowance; search concurrency is capped at two
queries with a 128 MiB per-query budget. These flags do not bound all process
memory: ingestion, merges and runtime overhead need headroom too. Queries over
the budget fail explicitly rather than qualifying as successful monitoring.
See [VictoriaMetrics resource limits](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#resource-usage-limits).

Preview the refreshed stack before applying; only the metrics server's pod
resources and arguments should change among running workloads. Full-stack
previews can also recreate completed ingress admission hooks and reconcile
chart RBAC metadata; inspect those separately. Preserve the existing PVC. Afterward,
verify stable restarts and memory through ingestion catch-up, successful bounded
queries, and fresh Grafana evaluations without datasource errors. Backfill
historical coverage in bounded windows only after recovery; a healthy current
query does not establish that old samples survived the outage.

For arrival/completion metrics, database-pool timing, and ingress cache logs, see
[Diagnosing production overload](../docs/administration/performance-observability.md).

Staging and production reserve 300m CPU per ingress controller and 250m per
registry replica, with no CPU limits. Each Deployment spreads its own revision
across eligible nodes using `DoNotSchedule`, `maxSkew: 1`, and
`matchLabelKeys: [pod-template-hash]`. Counting revisions separately prevents
old pods from masking an uneven placement of the replacement replicas. Tainted
nodes are excluded unless tolerated. Local development keeps soft spreading and
100m CPU requests. Hosted clusters require Kubernetes 1.27 or newer with
`MatchLabelKeysInPodTopologySpread` enabled.

Ingress readiness allows three seconds per check; liveness allows five. The
chart's ten-second period and three/five failure thresholds are retained. These
timeouts tolerate brief stalls; they do not establish that request latency is
healthy. Both Deployments allow one surge pod and zero unavailable pods during
updates. Before rollout, check that eligible nodes have enough unreserved CPU
and memory for the surge pods as well as the steady-state requests. A pending
replacement should be investigated, not worked around by deleting healthy pods.

Roll out these pod changes through staging and then the normal production
promotion. Check that both Deployments become available, each pair of production
replicas ends on different nodes, and no replacement remains Pending. Compare
ingress probe failures/restarts, node CPU pressure, and request latency at
comparable traffic over the following day, including a busy period. Application
metrics omit ingress cache hits, so include ingress logs in the latency check.

### Node boot disks

Hosted node pools use 50 GB `pd-balanced` boot disks for the ingress cache and
container logs. This replaces the 20 GB `pd-standard` disks that showed sustained
I/O queues. GKE surge upgrades explicitly allow one extra node and zero
unavailable nodes. Changing disk type or size rolls the nodes; the PostgreSQL
and monitoring persistent volumes are separate and keep their existing sizes.

Complete the pod rollout above before promoting the disk change to production.
Merging the disk change rolls staging first; production requires a separate
promotion. Preview the actual Pulumi plan before each rollout and confirm a
node-pool update rather than resource replacement. Check quota and capacity for
the extra node, verify a recent successful database backup, and schedule for a
database interruption: the single registry and Grafana PostgreSQL instances
must stop and reattach their persistent disks on another node. Surge capacity
does not make these databases highly available. Monitoring workloads can also
be interrupted while moving.

After the rollout, verify both nodes use 50 GB `pd-balanced` disks, all workloads
are ready, and the two registry and production ingress replicas are spread
across nodes. Check the registry health, listing, and search endpoints. Compare
boot-disk latency/queues, ingress restarts, and request latency during a busy
period before concluding that the disk upgrade resolved the observed stalls.

### Node CPU capacity

`mcp-registry:nodeMachineType` selects the GCP node machine type independently
for each stack; it defaults to `e2-standard-2`. Both `Pulumi.gcpProd.yaml` and
`Pulumi.gcpStaging.yaml` explicitly use `e2-standard-4`. Staging also runs two
ingress replicas so performance tests match production's node capacity and
independent caches. The node count and boot-disk configuration are unchanged
by this setting. Staging retains this capacity between test runs.

Changing machine type recreates nodes using the surge settings above. Preview
both stacks and require an update of the intended node pool, not replacement
of the pool or cluster. Check the full production preview because promotion
applies all infrastructure code at that commit. Confirm surge quota and zonal
capacity, a recent usable backup, and no competing GKE operation before rollout.

Expect database interruption and cold ingress caches as pods move. The single
PostgreSQL instance can move more than once if it lands on another old node
that is subsequently replaced. Watch database placement, PVC attachment,
service readiness, application reconnection, and ingress availability.

After recovery and cache warming, compare at least 15 minutes of similar
incoming traffic and query mix: node and PostgreSQL CPU pressure, CPU quota
throttling, pool queues, connection failures, listing latency, and errors.
Keep database resource settings, buffers, connection limits, and cache settings
unchanged during this comparison to attribute the capacity change's effect.
Reverting machine type causes another disruptive rolling update; investigate a
stalled GKE operation before reversing configuration.

### Independent production database capacity

`databaseNodeMachineType: e2-standard-8` opts the isolated production database
pool into eight vCPUs while keeping application workers at `e2-standard-4`.
The validated steady-state profile uses PostgreSQL CPU request/limit 6/6; 3/3
is also allowed for staged upgrade/rollback. It retains the
4 GiB memory limit, 512 MiB shared buffers, 200 server connections, app pools
20/5, app HPA 2–6 at 60%, worker range 2–7, and the existing database PVC.
Omitting the setting preserves the worker-sized database pool and previous CPU
budget. Staging does not use this production override.

Preview the full stack and require an in-place update of the protected database
node pool, never pool/cluster/PVC replacement. The Kubernetes provider depends
on the node pools, so the larger node is available before the six-CPU database
request is applied. GKE will roll the database node with surge capacity; the
singleton database must stop and reattach its PVC, causing an interruption.
Check a recent usable backup, free disk space and quota before applying. Verify
PVC identity, database readiness, connections, CPU, errors and latency after the
move and resource reconciliation. Memory, buffers and app concurrency are not
part of this change. This larger database VM remains billable during idle time.

Keep application promotion separate: prebuild and validate the query indexes,
then promote a tested stable image. Retain the previous image and indexes for
rollback. Downsizing the DB also rolls its node; first return PostgreSQL to its
three-CPU budget and verify it fits before reverting the node size.

### PostgreSQL shared buffers

`mcp-registry:postgresSharedBuffers` optionally sets `shared_buffers` on the
registry database for one stack. When omitted, PostgreSQL retains its default
(128 MB on the current image). Use PostgreSQL units, for example `512MB`.
This setting does not change node capacity, CPU resources, connection limits,
or the Grafana database. Removing an override restores the default and also
requires a restart.

Validate a buffer change in staging before a separate production YAML
promotion. Preview the full target commit and require only the intended
database parameter change plus understood deployment housekeeping. Check a
recent usable backup, database and operator health, current container memory,
OOM events, and available node memory. Budget buffers together with backend
and query memory and file cache; the 512 MiB memory request does not reserve
the full 4 GiB limit. A move from 128 MB to 512 MB adds 384 MB of configured
shared buffers.

Changing this startup parameter restarts the singleton database and interrupts
database-backed requests. Watch the operator reconcile, readiness and app
reconnection; verify `SHOW shared_buffers` and `pg_settings.pending_restart`
afterward. Rollback also requires a database restart.

After recovery and cache warming, collect at least 15 minutes of observations.
Compare request and query mix, pool queues, listing latency, errors, database
CPU pressure and quota throttling, memory, and BufferMapping wait samples.
Use fresh `pg_stat_io` and `pg_stat_statements` deltas within each window;
check reset timestamps and normalize buffer evictions and reads by completed
query volume. Lower buffer churn alone does not establish recovery if queues
and latency remain high. Keep other tuning settings fixed during the test.

## Server-name search index

Migration 015 adds a GIN trigram index for substring searches on `server_name`.
The index was built concurrently and verified valid in production on
2026-10-23; production does not need to repeat that build.
On a busy existing database, build it **before deploying** the migration, using
an administrative `psql` connection to the application database:

```sql
SET application_name = 'registry-trigram-index-build';
SET lock_timeout = '10s';
SET statement_timeout = '10min';
CREATE INDEX CONCURRENTLY idx_servers_name_trgm
    ON public.servers USING gin (server_name gin_trgm_ops);
```

Run these statements with autocommit enabled, outside a transaction (do not use
`psql --single-transaction` or put all statements in one `psql -c` argument).
The existing `pg_trgm` extension supplies `gin_trgm_ops`. Concurrent construction
keeps reads and writes available, but consumes CPU and I/O. Watch API latency,
database waits, and `pg_stat_progress_create_index` while it runs. If the build
materially worsens service, cancel only the backend identified by this
`application_name` and the index-build progress view.

Verify the index before deploying:

```sql
SELECT i.indisvalid, i.indisready, pg_get_indexdef(i.indexrelid)
FROM pg_index i
WHERE i.indexrelid = to_regclass('public.idx_servers_name_trgm');
```

Both flags must be true, with a GIN index on `server_name gin_trgm_ops`. If the
index already exists and matches, skip the build. A cancelled or failed build
can leave an invalid index: after confirming the build has stopped and the
index is invalid, remove only that index with
`DROP INDEX CONCURRENTLY public.idx_servers_name_trgm` and retry the build.
Do not rely on `IF NOT EXISTS` to repair an invalid index.

After completion, check `EXPLAIN` for representative substring searches and
compare fresh `pg_stat_statements` deltas and API responses, including pagination.
Ingress caches listing responses, so HTTP latency alone is insufficient evidence.
Very short or broad search terms may still scan many rows.

The application migration runs in a transaction. It creates the index normally
on fresh databases and skips an existing valid index; it rejects an invalid
index instead of silently recording success. Other populated installations
should also prebuild the index concurrently to avoid blocking writes during
startup. No application restart is needed for the operational index build.

### Namespace suffix index

Migration 016 adds `idx_servers_namespace_suffix` for substring searches that
span the namespace/path separator. The existing `check_server_name_format`
constraint guarantees exactly one `/`. A matching search must therefore match
a suffix of the namespace; reversing it converts that suffix to a B-tree prefix
lookup. The application still applies the complete original substring predicate.
Searches without a namespace component keep their existing behavior.

For a busy existing database, use the same concurrent-build procedure and
monitoring described above, with these statements in an autocommit session:

```sql
SET application_name = 'registry-namespace-index-build';
SET lock_timeout = '10s';
SET statement_timeout = '10min';
CREATE INDEX CONCURRENTLY idx_servers_namespace_suffix
    ON public.servers (lower(reverse(split_part(server_name, '/', 1))) text_pattern_ops);
```

Verify both `indisvalid` and `indisready`, and the exact expression/operator class
using `pg_get_indexdef`, before deploying the application migration. A failed
build must be repaired explicitly; `IF NOT EXISTS` cannot repair it. Keep the
trigram index: searches without a namespace still use it. After creation, run
`ANALYZE public.servers` to collect expression statistics, then compare plans for
rare namespaces, partial namespace suffixes, common terms, and latest/all-version
searches. Measure publisher latency as well as read performance.

The local constrained benchmark and its limits are documented in
[`tools/readbench/CONSTRAINED_RESULTS.md`](../tools/readbench/CONSTRAINED_RESULTS.md).
That experiment does not deploy this index or change production settings.

### Search page covering index

Migration 017 adds `idx_servers_search_cover` and
`idx_servers_search_namespace_cover`. Search queries first select the
ordered page keys and then fetch their full records in the same SQL statement.
The partial index covers non-deleted names/versions and the latest/update filters,
so rejected names need not fetch JSON-bearing heap rows. Included-deleted searches
retain the same results but cannot use this partial index. Other listing queries
are unchanged.

Prebuild on a busy database in an autocommit session:

```sql
SET application_name = 'registry-search-cover-index-build';
SET lock_timeout = '10s';
SET statement_timeout = '10min';
CREATE INDEX CONCURRENTLY idx_servers_search_cover
    ON public.servers (server_name, version) INCLUDE (is_latest, updated_at)
    WHERE status != 'deleted';
CREATE INDEX CONCURRENTLY idx_servers_search_namespace_cover
    ON public.servers (lower(reverse(split_part(server_name, '/', 1))) text_pattern_ops)
    INCLUDE (server_name, version, is_latest, updated_at)
    WHERE status != 'deleted';
```

Verify `indisvalid`, `indisready`, and `pg_get_indexdef` before deployment, then
`ANALYZE public.servers`. Keep the existing trigram and namespace indexes for
selective searches. Index-only scans still depend on the visibility map; measure
heap fetches on recently published data as well as quiet data. Validate broad and
rare terms, both latest/all versions, full walks, and publisher latency under load
before production promotion. A larger connection pool is not a substitute for
reducing database work under a CPU limit.

### Database CPU budget for burst traffic

`mcp-registry:postgresCPULimit` controls the PostgreSQL container CPU quota and
otherwise defaults to `1500m`. Staging opts into `"3"`; this does not change node
count, machine type, memory, storage, or database connection limits. The broad
search comparison uses the same 3-CPU budget for both application versions.
The query changes alone did not sustain the complete offered workload at 1.5 CPUs.

Promoting the tested combination requires setting `postgresCPULimit: "3"` in
`Pulumi.gcpProd.yaml` along with the application promotion, after prebuilding and
verifying both indexes above. That file triggers the production deployment
workflow when merged. The current CloudNativePG operator restarts the singleton
for CPU-resource changes, so schedule that operation deliberately; it is not a
zero-downtime application-only rollout. Staging experiments drained app connections
before the restart to avoid waiting for idle pooled connections during shutdown.

Keep the application pool at 60 maximum / 10 minimum connections per replica
and PostgreSQL at 200 connections for this rollout. A larger pool does not add
CPU capacity; a smaller-pool experiment also failed the offered workload. See
[staging results](../tools/readbench/STAGING_SEARCH_RESULTS.md) for the experiment conditions and limits.

## Structure

```
├── main.go                 # Pulumi program entry point
├── Pulumi.yaml             # Project configuration
├── Pulumi.local.yaml       # Local stack configuration
├── Pulumi.gcpProd.yaml     # GCP production stack configuration
├── Pulumi.gcpStaging.yaml  # GCP staging stack configuration
├── Makefile                # Build and deployment targets
├── go.mod                  # Go module dependencies
├── go.sum                  # Go module checksums
└── pkg/                    # Infrastructure packages
    ├── k8s/                # Kubernetes deployment components
    │   ├── backup.go          # Database backup configuration
    │   ├── cert_manager.go    # SSL certificate management
    │   ├── deploy.go          # Deployment orchestration
    │   ├── ingress.go         # Ingress controller setup
    │   ├── monitoring.go      # Metrics and monitoring setup
    │   ├── postgres.go        # PostgreSQL database deployment
    │   └── registry.go        # MCP Registry deployment
    └── providers/          # Kubernetes cluster providers
        ├── types.go           # Provider interface definitions
        ├── gcp/               # Google Kubernetes Engine provider
        └── local/             # Local kubeconfig provider
```

### Architecture Overview

#### Deployment Flow
1. Pulumi program starts in `main.go`
2. Configuration is loaded from Pulumi config files
3. Provider factory creates appropriate cluster provider (GCP or local)
4. Cluster provider sets up Kubernetes access
5. `k8s.DeployAll()` orchestrates complete deployment:
   - Certificate manager for SSL/TLS
   - Ingress controller for external access
   - Database for data persistence
   - Backup infrastructure for database
   - Monitoring and metrics collection
   - MCP Registry application

## Configuration

Configuration keys are namespaced. Set them with `pulumi config set <key> [--secret]`.

| Parameter | Description | Required |
|-----------|-------------|----------|
| `mcp-registry:environment` | Deployment environment (local/staging/prod) | Yes |
| `mcp-registry:provider` | Kubernetes provider (local/gcp) | No (default: local) |
| `mcp-registry:nodeMachineType` | GCP node machine type; changing it rolls nodes | No (default: e2-standard-2) |
| `mcp-registry:postgresSharedBuffers` | Registry PostgreSQL shared buffers, e.g. `512MB`; changes restart the singleton | No (PostgreSQL default) |
| `mcp-registry:githubClientId` | GitHub OAuth Client ID | Yes |
| `mcp-registry:githubClientSecret` | GitHub OAuth Client Secret | Yes (secret) |
| `mcp-registry:jwtPrivateKey` | Ed25519 seed used to sign registry JWTs | Yes (secret) |
| `mcp-registry:googleOauthClientSecret` | Google OAuth client secret for Grafana login | Yes (secret) |
| `mcp-registry:alertDiscordWebhook` | Discord webhook for Grafana alert notifications; alerts are provisioned only where it is set | No (secret) |
| `mcp-registry:usageCollector` | `"true"` runs the daily [usage collector](#usage-collector); `"false"` retires it | No (hosted stacks only) |
| `mcp-registry:usageCollectorOrganizationAliases` | Names for organizations' own networks in the usage dashboards | No (secret) |
| `mcp-registry:malwareSweep` | `"true"` runs the daily [malware sweep](#malware-sweep) | No |
| `mcp-registry:malwareCheckMode` | What a publish does with a package OSV lists as malware: `report`, `enforce` or `off`; see [The publish-time check](#the-publish-time-check) | No (default: report) |
| `mcp-registry:imageTag` | Docker image tag for production environment | Yes (prod only) |
| `gcp:project` | GCP Project ID | Yes when `provider=gcp` |
| `gcp:region` | GCP Region | No (default: us-central1) |
| `gcp:credentials` | GCP service account credentials JSON | No (secret; set in staging/prod) |

## Grafana Dashboards

Every Grafana dashboard is kept in this repository and provisioned by Pulumi:

| Dashboard | File | Grafana folder |
|-----------|------|----------------|
| Registry Application Dashboard | `pkg/k8s/dashboards/registry-application.json` | General |
| Registry Deployment/Critical Issues | `pkg/k8s/dashboards/registry-deployment-critical-issues.json` | General |
| Registry Gateway Canary | `pkg/k8s/gatewaycanary/assets/dashboard.json` | Gateway Canary |
| Registry usage | `pkg/k8s/usage/dashboards/registry-usage.json` | General |
| Registry publishing | `pkg/k8s/usage/dashboards/registry-publishing.json` | General |

The last two exist only where the [usage collector](#usage-collector) is on.
They have a provider of their own and read its PostgreSQL data source.

Grafana refuses to save a provisioned dashboard from the UI, so a change goes
through a pull request:

1. Open the dashboard in Grafana, edit it, and use **Export → Export as JSON**
   instead of saving.
2. Replace the file with the export. Keep the `uid` and remove the top-level
   `id` and `version`, which belong to one Grafana database.
3. Run `go -C deploy test ./pkg/k8s/`. The test rejects a changed production
   `uid`, a duplicate title and a data source that is not provisioned.

To add a dashboard, put its JSON in `pkg/k8s/dashboards/`. It may only use the
data sources provisioned in `pkg/k8s/monitoring.go`.

Dashboard changes reach staging when they merge to `main` and production with
the next production deployment. Grafana rereads the files on its own; only a
new mount restarts it.

The two General dashboards were first built in the production UI. Grafana
adopts an existing dashboard whose `uid` matches a provisioned file, so they
kept their links. Removing the `registry` provider, including by reverting the
change that added it, makes Grafana delete the dashboards it manages. The JSON
stays in git, but re-importing it by hand would be needed to bring them back.

Alert rules and their contact point are covered in [Grafana Alerts](#grafana-alerts).

## Grafana Alerts

The alert rules and the Discord contact point they notify are kept in this
repository and provisioned by Pulumi:

| What | File |
|------|------|
| Rule group `application_alerts` in the folder "application alerts" | `pkg/k8s/alerting/rules.yaml` |
| Contact point "Registry Dev" (Discord) | `pkg/k8s/alerting/contact-points.yaml` |

They are provisioned only in an environment where
`mcp-registry:alertDiscordWebhook` is set. Without it nothing alert-related is
deployed there and Grafana is left exactly as it is. Notification policies,
templates and mute timings are still managed inside Grafana; the rules name
their contact point directly and do not use the policy tree.

**Provisioning is a one-way lock.** Once Grafana has provisioned or adopted a
rule or contact point, it can only be changed through this repository: the UI
and the API refuse to edit or delete it. Removing the files, or the webhook,
does not unlock it or delete it. See [Removing rules](#removing-rules).

### Setting the webhook

The webhook is a credential and this repository is public, so it is only ever
stored encrypted. For production (for staging use `make staging-login`,
`gcpStaging` and `passphrase.staging.txt`):

```bash
cd deploy
make prod-login
PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt \
  pulumi config set --stack gcpProd --secret mcp-registry:alertDiscordWebhook
```

Paste the webhook at the prompt rather than putting it on the command line,
which would leave it in the shell history. Commit the resulting `secure:` entry
in `Pulumi.gcpProd.yaml`; merging it deploys production. To rotate the webhook,
set the new value the same way. The deployment restarts Grafana so that it
picks the new value up.

For production, use the webhook of the existing "Registry Dev" contact point
(Discord: channel settings → Integrations → Webhooks → Copy Webhook URL). The
configured value replaces the one Grafana has stored. A deployment refuses a
webhook that is not encrypted or is not an `https://` URL, without printing it.
If the webhook was ever committed unencrypted, replace it in Discord.

Set staging's webhook first, so that the first place these files are applied
is not production. Staging has no alerts until then; it then gets the same
rules, evaluated against staging's metrics. Give it a webhook for a test
channel of its own: staging carries little traffic, so rules whose series go
missing there may send "DatasourceNoData" notifications.

### Changing rules

Grafana refuses to save a provisioned rule or contact point from the UI, so a
change goes through a pull request:

1. For a threshold, a duration or a query, edit `pkg/k8s/alerting/rules.yaml`
   directly. For a larger change, open the rule in Grafana, choose
   **Export → With modifications**, edit it there and export it as YAML, then
   copy the rule into the file.
2. Keep the `uid` of an existing rule. Give a new rule a `uid` of its own and a
   `notification_settings.receiver` that is defined in `contact-points.yaml`.
3. A rule title cannot contain `$`, and a literal `$` in a contact point
   setting is written `$$`: Grafana replaces `$NAME` in those fields with
   environment variables.
4. Run `go -C deploy test ./pkg/k8s/`. The test rejects a changed production
   `uid`, a rule whose contact point, data source or dashboard panel does not
   exist, a webhook written into a file, and a provisioned notification policy.

Changes reach staging when they merge to `main` and production with the next
production deployment. Grafana reads these files only when it starts, so the
deployment restarts it whenever a file or the webhook changes.

Grafana does not start with a file it cannot provision: invalid YAML, an
invalid rule, or a rule that notifies a missing contact point. The new pod then
never becomes ready, the running one keeps serving and the deployment fails.
Fix it promptly, because the running pod would hit the same error if it
restarted.

### Removing rules

Taking a rule out of the file does not delete it, and neither does removing
this provisioning altogether, whether by unsetting the webhook or by reverting
the change that added it. The rules and the contact point stay in Grafana, keep
evaluating and notifying, and stay read-only: neither the UI nor the API can
edit or delete them afterwards, and they cannot be returned to UI editing.

To delete a rule, remove it from `groups` and list its `uid` in the same file:

```yaml
deleteRules:
    - orgId: 1
      uid: <uid of the rule>
```

The entry can stay; it does nothing once the rule is gone. Never list a `uid`
that is still defined, because Grafana would delete and recreate that rule at
every start. `deleteContactPoints` removes a contact point in the same way, but
only once no rule notifies it; Grafana does not start otherwise.

Production's four rules and its contact point were first built in the UI.
Grafana adopts an existing rule or contact point whose `uid` matches a
provisioned one. A rehearsal of that takeover on Grafana 12.4.14 kept the
queries, thresholds, timings, routing and alert state, and neither lost nor
repeated a notification.

## Usage collector

The usage collector records, once a day, how the hosted registry was used on
each full UTC day and what was published to it. Grafana shows the result in the
"Registry usage" and "Registry publishing" dashboards. The program and its
settings are described in [cmd/usage-collector](../cmd/usage-collector/README.md);
this section covers how it is deployed.

Everything is behind one setting, `mcp-registry:usageCollector`:

| Value | Meaning |
|-------|---------|
| not set | Nothing is created and nothing changes. |
| `"true"` | The collector runs daily, with its database, dashboards and alert rules. |
| `"false"` | Retired. The collector is removed, and Grafana deletes its data source, dashboards and alert rules. The stored history is kept. |

It is `"true"` in staging and not set in production. It is only accepted on the
hosted stacks with the standalone Gateway, which produces the request counters
the collector scales its figures by.

### What is created

| Where | What |
|-------|------|
| Google Cloud | `roles/logging.viewer` on the project for the existing `registry-usage-reader` service account, and a key for it; the bucket `mcp-registry-<environment>-usage-backups` and that account's access to it |
| `default` | Role `registry_usage_reader` on `registry-pg`, and its password |
| `monitoring` | Roles `registry_usage_collector` and `registry_usage_grafana` and database `registry_usage` on `grafana-pg`; the CronJob `registry-usage-collector` with its Secrets and a 10Gi cache volume; the CronJob `registry-usage-history-backup`; the dashboards |
| Cluster | PriorityClass `registry-usage-batch` |
| Grafana | Data source "Registry usage", the two dashboards and, where [alerting](#grafana-alerts) is configured, two alert rules |

The image is `ghcr.io/modelcontextprotocol/registry-usage-collector`, built by
the same workflows as the registry's: `main` for staging, and the release tag
set in `imageTag` for production. Neither CNPG cluster is modified; the roles
and the database are separate `DatabaseRole` and `Database` objects.

### Switching it on

1. Set `mcp-registry:usageCollector: "true"` in the stack file. In production,
   `imageTag` must also be a release made after the collector was added, or its
   image does not exist.
2. Once: make the container package `registry-usage-collector` public (GitHub
   → the organization's Packages → the package → Package settings → Change
   visibility). A new package is private, and the clusters pull without
   credentials. Until this is done each day's job waits for its image and
   fails after three hours; nothing else is affected.
3. When the deployment has finished, start the first run by hand instead of
   waiting for 01:15 UTC:

   ```bash
   kubectl -n monitoring create job --from=cronjob/registry-usage-collector usage-first-run
   kubectl -n monitoring logs -f job/usage-first-run
   ```

   It creates its tables, reads every published version once and then the last
   13 days, oldest first. That takes about an hour. The dashboards fill in as
   it goes.

The tables do not exist until that first run starts. Until then the dashboards
show query errors and, where alerting is configured, both rules fail to
evaluate. If step 3 is skipped, expect a `DatasourceError` notification to
"Registry Dev" naming "Usage estimate drifted from the gateway counters" within
an hour of the deployment, and a second one naming "Usage collection missed a
day" if the tables are still missing two hours later. Both resolve on their own
once the first run has started. The rules are evaluated once an hour, so there
is a small chance of the first notification even when step 3 follows within
minutes.

While the first run is filling its 13 days, yesterday is not stored yet. The
missed-day rule waits two hours before it notifies, so the first run does not
set it off.

### The daily run

The CronJob starts at 01:15 UTC. A run collects every full day of the last 13
that is not stored yet, so a missed or failed run is made up by the next one
without anyone doing anything. A gap longer than 13 days cannot be filled: the
gateway counters are kept for 14.

A run cannot hold up a deployment or the registry:

- Pulumi waits for none of it: not the job, the cache volume, the roles or the
  database.
- The pod requests 50m CPU and 512Mi, is limited to one CPU and 1Gi, never runs
  on the database's node, and has a priority below every other workload. Any
  pod that needs the room evicts it, and the cluster autoscaler does not add a
  node for it. An evicted run is started again; a run that fails is not, and
  tomorrow's collects what is missing.
- It reads the registry database over one connection, 500 rows at a time, in
  read-only transactions with a 15 second statement timeout.
- Two runs never overlap, and a run is stopped after three hours. Days are
  stored one by one, so a stopped run keeps what it finished.

Known behaviour: a day on which the gateway wrote no access records at all is
never stored, and every run reports failure while that day is within the 13
days. This does not happen while the environment has any traffic.

To run it outside its schedule:

```bash
kubectl -n monitoring create job --from=cronjob/registry-usage-collector usage-manual
```

Like the scheduled run, this collects only days that are not stored. Collecting
a stored day again takes the collector's `--day` flag.

### Credentials and access

All four credentials are created by Pulumi and exist only in its encrypted
state and in Kubernetes Secrets. None is written to this repository.

| Credential | Used by | What it can do |
|------------|---------|----------------|
| Key of `registry-usage-reader@<project>` | The collector and the backup | Read log entries of its own project; read and write the usage backup bucket, and no other bucket |
| `registry_usage_collector` | The collector and the backup | Owns database `registry_usage` on `grafana-pg`. It has no access to Grafana's own database in that cluster. The backup uses it in read-only transactions. |
| `registry_usage_grafana` | Grafana's data source | Read the tables, the view and the functions the dashboards query. The collector grants this each time it starts. |
| `registry_usage_reader` | The collector | Read the registry database |

**The registry role can read every table in the registry database.** It is a
member of PostgreSQL's `pg_read_all_data`. It cannot write to, create or change
any table or other lasting object there: in staging and in production the
`public` schema lets only the database's owner create in it. Like every role,
it can create temporary tables of its own, which is PostgreSQL's default.
Today that database holds two tables: `servers`, which the
registry publishes in full, and `schema_migrations`. The collector only needs
`servers`. A grant on that one table has to be made by the table's owner and is
dropped by the weekly staging restore, which is why membership is used instead.
If a table with data that is not public is ever added to the registry database,
change this role to a grant on `servers` first.

`roles/logging.viewer` lets the service account read all of the project's
ordinary logs, not only the gateway's access records. A log view restricted to
those records would narrow it; the collector would then need a setting for the
view it reads.

The service account itself is not managed here. An operator created it in each
project, and retiring the collector removes its role and its key but not the
account.

To replace the key and the three passwords, raise `CredentialGeneration` in
`pkg/usagecollector/usagecollector.go`. The deployment creates new ones and
restarts Grafana.

### Naming organizations' networks

The collector can show an organization's own networks under a chosen name. The
value names real organizations and this repository is public, so it is only
accepted encrypted:

```bash
cd deploy
make staging-login
PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.staging.txt \
  pulumi config set --stack gcpStaging --secret mcp-registry:usageCollectorOrganizationAliases
```

Type the value at the prompt, as `KEY=Name;KEY=Name`, and commit the resulting
`secure:` entry. The format is described in the collector's README. The
setting is optional; without it, networks are named from their registered
descriptions. A deployment refuses a value that is not encrypted or not in that
format, without printing it.

### How long the history is kept

Indefinitely: nothing prunes it. It grows by about 400 MB a year at the current
rate of publishing, on the 10Gi volume it shares with Grafana's own database.

### Backups of the history

`grafana-pg` is a single instance, so the history is backed up every day at
03:30 UTC, wherever the collector is on, by the CronJob
`registry-usage-history-backup`. It works like the registry's
[database backup](#database-backups): `pg_dump` writes an uncompressed
custom-format archive of `registry_usage`, reads it back in full and records
its checksum; Restic then stores it, checks the whole repository and keeps the
last 28 daily snapshots. `REGISTRY_USAGE_BACKUP_SUCCESS` is logged only when
every step has succeeded.

| | |
|---|---|
| Where | `gs://mcp-registry-<environment>-usage-backups/usage-history-v1`, a bucket that holds nothing else |
| Who makes the archive | `registry_usage_collector`, in read-only transactions. No credential of Grafana's own database is involved, and that database is not backed up. |
| Who writes to the bucket | The collector's service account, which is granted this one bucket. It cannot reach the registry's backups. |
| Restic password | `registry-usage-history`. This is not a secret. A generated password would exist only in Pulumi's state, and losing it would lose the backups; access to the bucket is what protects them. |
| Cost | The repository is the size of the database, well under a gigabyte for years: a few cents a month. |

Each snapshot holds the whole history, so the newest is all a restore needs;
the older ones are there to undo damage that is noticed late. Unlike the
registry's job, this one creates its repository if there is none and logs
`REGISTRY_USAGE_BACKUP_REPOSITORY_INITIALIZED` when it does, so nothing has to
be initialised by hand.

**A failed backup is not alerted on.** The [backup alerts](#backup-alerts)
cover the registry's job only. Check this one with:

```bash
kubectl -n monitoring get cronjob registry-usage-history-backup
kubectl -n monitoring logs job/JOB_NAME -c pg-dump
kubectl -n monitoring logs job/JOB_NAME -c restic
```

#### Restoring the history

On a workstation with Restic, the PostgreSQL 17 client tools and a Google
account that can read the bucket, fetch the archive and check it:

```bash
export RESTIC_REPOSITORY=gs:mcp-registry-prod-usage-backups:/usage-history-v1
export RESTIC_PASSWORD=registry-usage-history
export GOOGLE_PROJECT_ID=mcp-registry-prod
export GOOGLE_ACCESS_TOKEN=$(gcloud auth print-access-token)
umask 077
restic --no-lock snapshots
# "latest", or the ID of an earlier snapshot.
restic --no-lock dump latest /backup/registry-usage.dump > registry-usage.dump
restic --no-lock dump latest /backup/registry-usage.dump.sha256 > registry-usage.dump.sha256
shasum -a 256 -c registry-usage.dump.sha256
```

The archive goes into the database `registry_usage`, as the role that owns it.
After `grafana-pg` has been rebuilt, the operator creates that database and
the roles again from their `Database` and `DatabaseRole` objects; wait until
`kubectl -n monitoring get databases.postgresql.cnpg.io registry-usage -o jsonpath='{.status.applied}'`
prints `true`. Then make sure nothing is writing to the database:

```bash
# No new scheduled runs.
kubectl -n monitoring patch cronjob registry-usage-collector -p '{"spec":{"suspend":true}}'

# Suspending does not stop a run that has already started, nor one started by
# hand. List the runs, then wait for the active ones to finish or stop them.
# Stopping is safe: a run stores each day in one transaction. Deleting also
# removes the finished runs and their logs.
kubectl -n monitoring get jobs -l app=registry-usage-collector
kubectl -n monitoring delete jobs -l app=registry-usage-collector

# Go on only when this lists no pod: a run that is still stopping would write
# during the restore, or after it.
kubectl -n monitoring get pods -l app=registry-usage-collector \
  --field-selector=status.phase!=Succeeded,status.phase!=Failed
```

Then restore:

```bash
# In another terminal, and left running:
kubectl -n monitoring port-forward svc/grafana-pg-rw 15432:5432

PGSSLMODE=require \
PGPASSWORD=$(kubectl -n monitoring get secret registry-usage-collector-password -o jsonpath='{.data.password}' | base64 -d) \
pg_restore --host 127.0.0.1 --port 15432 --username registry_usage_collector --dbname registry_usage \
  --clean --if-exists --no-owner --exit-on-error --single-transaction registry-usage.dump

# Collect whatever is missing since the snapshot.
kubectl -n monitoring patch cronjob registry-usage-collector -p '{"spec":{"suspend":false}}'
kubectl -n monitoring create job --from=cronjob/registry-usage-collector usage-after-restore
```

The restore replaces the collector's tables, view and functions with the
archive's in one transaction, whether the database is empty or the collector
has already written to it, and brings back Grafana's read access with them.
Anything stored after the snapshot is replaced too; the run that follows
collects again every day of the last 13 that the archive does not have. Days
are lost only if more than 13 days pass between the snapshot and that run.

To test an archive without touching a cluster, create the roles
`registry_usage_collector` and `registry_usage_grafana` and an empty database
owned by the first in a disposable PostgreSQL 17, and run the same
`pg_restore` against it.

### Alert rules

Both are in `pkg/k8s/usage/alerting/rules.yaml`, in the group `registry_usage`,
and notify "Registry Dev". They exist only where the collector is on and
`mcp-registry:alertDiscordWebhook` is set. Removing the webhook while the
collector stays on leaves them in Grafana, as it leaves the registry's own
rules; retiring the collector deletes them whether or not the webhook is still
set.

| Rule | Fires when |
|------|------------|
| Usage collection missed a day | Yesterday was not stored by 04:00 UTC and still is not two hours later. The notification arrives between 06:00 and 07:00 UTC. |
| Usage estimate drifted from the gateway counters | On the newest stored day, requests or data sent estimated from the access records are more than 10% from the gateway's counters. Requests are judged when the gateway counted at least 1,000 that day, so a busy day whose access records went missing is reported. Data is judged when at least 1,000 listing records were sampled; fewer stray that far by chance. |

The first usually heals itself with the next run; look at the job's log if it
repeats. The second means the access record format or the log sampling rule has
changed, and the collector has to follow. [Changing rules](#changing-rules)
applies to this file too; a rule removed from it must be listed in
`retired.yaml` beside it.

### Retiring it

Set the value to `"false"` and deploy. Do not just remove the setting: Grafana
keeps a provisioned data source and provisioned alert rules after their files
are gone, the rules locked and failing, and only the `"false"` deployment
deletes them.

With `"false"`, Pulumi removes the two CronJobs, the Secrets, the cache volume,
the service account's key and its roles, and the dashboards' files. Grafana
restarts and deletes the data source, the two dashboards and the two rules.
What stays:

- The database `registry_usage`, its history and the three database roles, in
  PostgreSQL, with passwords that no longer exist anywhere. Setting `"true"`
  again is meant to take them over and set new passwords; that path has not
  been exercised on a cluster yet.
- The backup bucket and every snapshot in it. No new ones are made, and Restic
  no longer prunes, so the last 28 stay.

Leave the setting at `"false"` rather than removing it. Removing it deletes
nothing, the bucket included, but Pulumi then forgets the bucket, and a later
`"true"` would fail to create a bucket that already exists until it is
imported into the stack.

## Malware sweep

Once a day the malware sweep checks every package the registry lists against
the malware records that [OSV.dev](https://osv.dev) publishes, and reports the
server versions that point at one. It reports only; a maintainer takes the
entry down. What it checks, and how to respond to a match, is described in
[Admin Operations](../docs/administration/admin-operations.md#malware-checks);
this section covers how it is deployed.

It runs where `mcp-registry:malwareSweep` is `"true"`: in staging, and not yet
in production. Removing the setting deletes the CronJob.

### The publish-time check

The registry also checks packages when a server is published or edited. That
check is part of the registry itself, so it arrives with the image and needs
nothing created. `mcp-registry:malwareCheckMode` sets what it does with a match:

| Value | Meaning |
|-------|---------|
| not set, or `report` | The match is logged and the request goes through. Nothing is refused. |
| `enforce` | The match is logged and the request is refused with a `400`. |
| `off` | OSV is not asked. |

It is `report` in staging and not set in production, so both only report. The
plan is to leave it there for a few days and read what it logs: a match that is
wrong appears as `REGISTRY_MALWARE_PUBLISH_MATCH action=allowed` without a
publisher having been turned away, and a match it missed appears in the daily
sweep. To start refusing, set `mcp-registry:malwareCheckMode: enforce` in the
stack file; in production that change is a deployment. Any other value fails
the preview.

### What is created

One CronJob, `registry-malware-sweep`, in `default`, and nothing else. It runs
the registry's own image with `-malware-sweep`, so it is always the same build
as the registry beside it. It has no Secret, database role or service account:
it reads the listing from the `mcp-registry` Service, which bypasses the public
rate limits, and sends package names and versions to `api.osv.dev`.

### Switching it on in production

Set `mcp-registry:malwareSweep: "true"` in `Pulumi.gcpProd.yaml` in the same
change as, or after, an `imageTag` release that contains the sweep. An older
image does not know the flag: the Job fails at once and reports nothing, and
the freshness alert below says so. It does not start a second registry.

### The daily run

The run starts at 03:40 UTC and gives up after 30 minutes. Read through the
public listing, 280,000 server versions took 12 minutes and 160 MB; the run in
the cluster has not been timed yet. It writes to its log:

| Line starts with | Meaning |
|------------------|---------|
| `REGISTRY_MALWARE_SWEEP_HIT` | A server version points at a package version that OSV lists as malware. One line per server version and package. |
| `REGISTRY_MALWARE_SWEEP_UNCHECKED` | A server version names an npm dist-tag as its package version, so there is no release to check. One line per server version and package; it does not alert. |
| `REGISTRY_MALWARE_SWEEP_SUCCESS` | The whole registry was checked. Carries the number of versions, packages, matches and unchecked entries. |
| `REGISTRY_MALWARE_SWEEP_FAILED` | The registry could not be read in full, or OSV did not answer in full after three more attempts at each request. Matches OSV had confirmed are still reported; the Job is retried once. |

Each run's log is kept with its Job, and the last three runs are kept, so read
the newest one rather than everything with the label:

```bash
kubectl --context=YOUR_CONTEXT -n default get cronjob registry-malware-sweep
JOB=$(kubectl --context=YOUR_CONTEXT -n default get jobs -l app=registry-malware-sweep \
  --sort-by=.metadata.creationTimestamp -o name | tail -1)
kubectl --context=YOUR_CONTEXT -n default logs "$JOB" --tail=-1
# Run it now:
kubectl --context=YOUR_CONTEXT -n default create job --from=cronjob/registry-malware-sweep registry-malware-sweep-manual
```

### Malware alerts

Matches are private until the entry is down, so they are sent by email from
Cloud Monitoring and never to Discord. Provision/update the alerts with:

```bash
python3 tools/admin/configure-malware-alerts.py \
  --project mcp-registry-prod --cluster mcp-registry-prod --email YOUR_EMAIL --dry-run
# Review, then rerun without --dry-run.
```

The script reuses an email channel and updates only its labelled policies. It
alerts on any `REGISTRY_MALWARE_SWEEP_HIT` line and on any
`REGISTRY_MALWARE_PUBLISH_MATCH` line in the registry's log, each at most once an
hour, and on no completed sweep in 25 hours, including complete absence of the
success metric.
Log metrics are not backfilled: provision the alerts after the sweep is switched
on, run a fresh job and verify that the success metric appears in Cloud
Monitoring. The freshness policy initially alerts until a sweep completes. Alert
delivery and channel verification should be checked with the notification
recipient. Removing the setting does not remove the policies; delete them in
Cloud Monitoring (label `managed_by: registry-malware-monitoring`).

## Database Backups

The production-only `registry-database-backup` CronJob backs up the registry's
`app` database with PostgreSQL 17 `pg_dump --format=custom --compress=0`. Leaving table data
uncompressed lets Restic deduplicate it before repository compression, rather
than storing changing compressed streams. The temporary volume must fit the
uncompressed archive (currently limited to 2 GiB). An init container connects using
`registry-pg-app` credentials, requires TLS, enables read-only transactions, and
writes to a private temporary volume. A private `registry-pg-backup` service
follows the primary even when the CNPG management readiness probe is slow; the
job independently waits for PostgreSQL to accept connections. It reads the entire
archive back with
`pg_restore --file=/dev/null` and records a SHA-256 checksum before the upload
container can start. No production database volume is mounted.

The second container verifies the checksum, uploads the archive with Restic,
runs `restic check --read-data`, and applies 28-day daily snapshot retention.
It emits `REGISTRY_DATABASE_BACKUP_SUCCESS` only after all steps succeed.
The archive contains database schema, data, ownership and grants. Cluster-wide
roles, role passwords, the maintenance `postgres` database, and Grafana's separate
database are outside this backup. CNPG supplies the registry's `app` role and
credentials when provisioning a replacement cluster.

Production runs daily at **04:46 UTC**, with a 30-minute job deadline and
no overlapping runs. Staging and local development do not create the backup
CronJob or its private Service and require no repository initialization.
The hosted K8up operators and schedules are retired. Staging's legacy backup
credentials and historical recovery assets are retired too; its empty backup
bucket remains managed by Pulumi. Local development retains its existing storage
configuration. No automatic staging backup smoke test is run.
Validate infrastructure changes with builds and Pulumi previews, and periodically
test recovery from a production archive in an isolated database using the
procedure below. This is a daily logical backup, not point-in-time recovery;
the recovery point can be up to one day old. Increase the backup volume size, deadline, and
client major version when database growth or a PostgreSQL upgrade requires it.

### Repository and retention

Restic stores `/backup/registry.dump` and `/backup/registry.dump.sha256` in the
`postgres-custom-v1` prefix of the existing
[production bucket](https://console.cloud.google.com/storage/browser/mcp-registry-prod-backups/postgres-custom-v1?project=mcp-registry-prod).

**Never configure object-age deletion for live Restic objects.** Its keys, config,
and deduplicated data can be older than every retained snapshot and still be
required. Restic alone manages live repository retention. GCS versioning is
retained. A separate lifecycle rule deletes only noncurrent versions under
`postgres-custom-v1/` after 30 days **since becoming noncurrent**, using
`isLive: false` and `daysSinceNoncurrentTime: 30`. It never matches live keys,
config, packs, or snapshots, regardless of their age. GCS soft-delete retention
can delay final removal further (currently seven additional days in production).
Legacy repositories and one-off upgrade archives are retired separately from
this active repository. Do not extend lifecycle deletion to live Restic objects.

The legacy K8up Schedule is removed. Clearing its spec can leave defaulted
backup/prune entries without cron expressions and crash the K8up operator.
The idle operator and its CRDs have been retired. Production credentials remain
required by the archive CronJob. Legacy objects at the bucket root and in
`postgres-logical-v1` have been retired. The obsolete physical-data
production-to-staging sync workflow has been removed; it was incompatible with
the current logical archive format.

On a **new production deployment**, initialize only the new repository before
starting the CronJob. With the existing S3 credentials and repository password securely
loaded into the environment, use:

```bash
export RESTIC_REPOSITORY=s3:https://storage.googleapis.com/mcp-registry-prod-backups/postgres-custom-v1
restic init
```

The existing production repository is already initialized; do not initialize it again.
The scheduled job deliberately never runs `restic init`: a missing repository
must fail visibly, not silently create new encryption keys. Do not run init on
an existing or damaged repository. The password comes from
`default/k8up-repo-password`; S3 credentials come from
`default/k8up-backup-credentials`. Preserve that password/key association;
changing a Secret alone does not rotate Restic keys. Do not print credentials.

### Weekly staging database refresh

Staging's `registry-staging-restore` CronJob refreshes its registry database from
`gs://mcp-registry-prod-backups/postgres-custom-v1` every Sunday at **06:00 UTC**.
This replaces staging-only registry data. Staging downtime is intentional and
acceptable; production and Grafana databases are never restore targets.

`mcp-registry:stagingRestore` provisions the job, controller, namespace-scoped RBAC,
reader account/key, and watchdog. `stagingRestoreScheduleEnabled` controls the
CronJob's suspended state. Bootstrap it suspended and complete a manual restore
before enabling the weekly schedule. The watchdog runs every ten minutes.

The workflow selects the latest complete Restic snapshot from host
`registry-postgres`, tag `postgres-custom`, and rejects snapshots older than 36
hours. It downloads with `--no-lock`, checks the checksum and archive readability,
and checks staging identity before stopping the app. It saves the replica count
and a maintenance lock in `registry-staging-restore-state`, waits for app pods to
exit, and restores with the existing staging app role in a single transaction.
Staging extensions, role passwords and permissions are preserved. The app's
registry tables are replaced; unrelated database objects are not deliberately
removed. A failed SQL restore rolls back that transaction, but staging stays
stopped for operator inspection. No parallel database or automatic rollback copy
is created. Successful completion requires restored rows, completed migrations
on app startup, available app replicas and a successful listing request.

The source reader has only object get/list access to the active repository;
it cannot write or delete production backups. Staging currently has no Workload
Identity configuration, so the reader key is managed by Pulumi in a dedicated
Kubernetes Secret. It has no production Kubernetes access or project-level IAM
role. `registry-staging-restore-password` is a separate bootstrap Secret containing
the existing repository password. Neither production HMAC credentials nor the
retired staging backup credentials are used. Do not print, commit or upload keys,
passwords, dump contents or restore logs containing data. Rotate the reader key
through Pulumi and update the password Secret if the Restic key/password changes.

Before the first deployment, an authorized operator creates the dedicated reader
account. Pulumi adopts that exact account; the staging deployment identity needs
key-management permission only on this account, not project-wide account creation:

```bash
gcloud iam service-accounts create registry-restore-reader \
  --project=mcp-registry-staging --display-name='Staging production-backup reader'
gcloud iam service-accounts add-iam-policy-binding \
  registry-restore-reader@mcp-registry-staging.iam.gserviceaccount.com \
  --project=mcp-registry-staging \
  --member=serviceAccount:pulumi-svc@mcp-registry-staging.iam.gserviceaccount.com \
  --role=roles/iam.serviceAccountKeyAdmin
```

After deploying the suspended job, the operator bootstraps source IAM/password
and email alerts:

```bash
python3 tools/admin/configure-staging-restore.py
kubectl --context=gke_mcp-registry-staging_us-central1-b_mcp-registry-staging -n default create job \
  --from=cronjob/registry-staging-restore registry-staging-restore-initial
```

The bootstrap helper preserves unrelated bucket IAM bindings and uses its etag.
It grants read access only to the active prefix (bucket-level listing is required
by Restic). It copies the repository password in memory and reuses the enabled
production backup email recipient in the staging project. Production backup
Secrets and repository contents are never changed.

Inspect every init container and wait for `REGISTRY_STAGING_RESTORE_SUCCESS` in
the final container, then verify the staging gateway and a real listing. After
that first success, enable freshness alerting and set
`stagingRestoreScheduleEnabled: "true"` through the normal staging deployment:

```bash
python3 tools/admin/configure-staging-restore.py --enable-freshness
```

Failure logs trigger an email; the watchdog also reports failed Jobs and a
maintenance lock older than 40 minutes, covering killed containers and timeout
failures that cannot log for themselves. The watchdog also reports eight days without a successful refresh using the
persisted success timestamp. A separate alert detects a missing watchdog heartbeat
for one hour, confirmed for ten minutes, including complete absence of its metric.
Bootstrap checks the actual PromQL heartbeat result before enabling this alert,
allowing log-metric ingestion to catch up. A missing heartbeat indicates a monitoring
gap and does not by itself mean staging is stopped.
Routine successes do not send email. Test the failure notification with a separate
watchdog-derived Job using controller phase `alert-test`; this emits an explicit
test failure and performs no database operations. Keep alert logs free of data.

Do not run a staging deployment concurrently with a refresh. `Forbid` prevents
scheduled overlap and the maintenance lock prevents concurrent manual restores.
The job never kills database sessions. The existing HPA remains configured;
manually setting app replicas to zero suspends it until a positive count returns.
The 35-minute Job deadline bounds a stuck restore; automatic retries are disabled
so a failed job cannot silently replace data again.

On failure, inspect Job/init-container logs and `registry-staging-restore-state`.
If failure preceded the database commit, the single transaction preserves the
prior tables; if restoration succeeded but app startup failed, inspect migration
and application errors. Resolve the problem before restoring the saved replica
count or retrying. After confirming the failed job has stopped, clear only its
`active_id` lock and restore the saved replica count before starting a new manual
Job. The controller refuses to overwrite another run's lock or start against an
already-stopped application. Any independent staging writes after the refresh
will be replaced by the next scheduled refresh.

### Retiring the idle K8up operator

The production archive CronJob calls `pg_dump` and Restic directly. Removing the
idle K8up Helm chart and CRDs does not remove that CronJob, its database Service,
`k8up-repo-password`, `k8up-backup-credentials`, the HMAC key, or either backup
bucket. Keep their existing Pulumi identities and password/key association.
The operator-retirement step below does not delete storage or credentials. A
subsequent retirement removed the unused dump PVC, staging pre-resize snapshot,
legacy archives, staging-only credentials, and obsolete sync workflow. The
active production repository and both production backup Secrets remain managed.

Before applying this retirement, run the read-only inventory for **both** stacks,
using a new owner-only directory for each invocation:

```bash
python3 scripts/retire_k8up.py staging --output /private/path/k8up-staging-before
python3 scripts/retire_k8up.py prod --output /private/path/k8up-prod-before
```

Keep these metadata archives private. The preflight rejects schedules, unfinished
operations, finalizers, active historical Jobs/Pods, and ownership paths to retained
PVCs. It includes the old K8up `Snapshot` API records, which are metadata for the
retained Restic repositories, not GCE snapshots or stored backup data.

Review both normal full-stack previews. They should delete only the K8up chart,
its operator/RBAC/Service resources, and its CRDs; staging also updates its normal
application deployment annotation/image. The production backup CronJob, credentials,
buckets, volumes, gateway, and capacity settings must remain unchanged. Removing
CRDs also removes the historical custom resources, which are not listed separately
in Pulumi's preview because they were created by the old operator.

After the normal deployment succeeds, clean any remaining historical Jobs/Pods
using their identities in the original inventory:

```bash
python3 scripts/retire_k8up.py staging --cleanup-historical \
  --prior-inventory /private/path/k8up-staging-before/inventory.json \
  --output /private/path/k8up-staging-after
python3 scripts/retire_k8up.py prod --cleanup-historical \
  --prior-inventory /private/path/k8up-prod-before/inventory.json \
  --output /private/path/k8up-prod-after
```

The helper requires the retired APIs/operator to be absent, rejects replaced or
active Jobs, and uses UID/resource-version preconditions with orphan propagation.
It never deletes PVCs, Secrets, CronJobs, or cloud storage. Existing archive-backup
Job history and unrelated certificate verification Jobs are kept.

### Adopting the production repair into Pulumi

The October 2026 repair created `default/registry-pg-backup` and
`default/registry-database-backup` directly in production. They must be imported
before the first production `pulumi up` using this code; a normal refresh does
not adopt unmanaged resources. The existing bucket and credentials are already in the production state and must
not be re-imported. The legacy operator has since been removed. The retired
K8up Schedule is deliberately deleted during deployment.
The GCP alert script manages its own policies separately from Pulumi.

From this directory, with production backend credentials and the existing stack
passphrase available through the usual secure mechanism, run:

```bash
export PULUMI_BACKEND_URL=gs://mcp-registry-prod-pulumi-state
export PULUMI_CONFIG_PASSPHRASE_FILE=passphrase.prod.txt
pulumi import --stack gcpProd --file imports/gcpProd-database-backups.json \
  --generate-code=false
make build
pulumi preview --stack gcpProd --refresh --diff
```

The import file includes the YAML component parent and the explicit production
Kubernetes provider, so the imported URNs match this program. Import reads the
existing Kubernetes objects and records them in state; it does not recreate
them. Keep the default import deletion protection until this code lands, so an
older checkout cannot delete the newly managed backup resources. A deployment
that includes these resource definitions clears that temporary protection.
The Service preserves its Kubernetes-allocated IPs during adoption.
Review the preview before any deployment: the backup Service and CronJob
must not be created, replaced, or deleted, and no production database replacement
is expected. Resolve any unrelated drift separately. Do not run this one-time
import on staging, local, a fresh production cluster, or resources already
imported. Successful import and preview are a rollout gate, not implied by a
successful code build.

### Verification and isolated recovery

Keep downloaded archives private (`umask 077`). With the above credentials and
repository configured, select a specific snapshot and verify it:

```bash
restic --no-lock snapshots
restic --no-lock check --read-data
umask 077
# Replace SNAPSHOT_ID with the full ID selected above.
restic --no-lock dump SNAPSHOT_ID /backup/registry.dump > registry.dump
restic --no-lock dump SNAPSHOT_ID /backup/registry.dump.sha256 > registry.dump.sha256
shasum -a 256 -c registry.dump.sha256
```

Do not stream an unverified transfer directly into a database. Check the archive
checksum at the destination before importing. Test recovery in an isolated,
disposable PostgreSQL 17 instance, never the public database. For example:

```bash
docker run -d --name registry-backup-verify --network none \
  -e POSTGRES_USER=restore_verifier -e POSTGRES_DB=postgres \
  -e POSTGRES_HOST_AUTH_METHOD=trust postgres:17
# Wait until this succeeds:
docker exec registry-backup-verify pg_isready -U restore_verifier
docker cp registry.dump registry-backup-verify:/tmp/registry.dump
docker cp registry.dump.sha256 registry-backup-verify:/tmp/registry.dump.sha256
docker exec -w /tmp registry-backup-verify sha256sum -c registry.dump.sha256
docker exec registry-backup-verify psql -X -U restore_verifier -d postgres \
  -v ON_ERROR_STOP=1 -c 'CREATE ROLE app LOGIN;' -c 'CREATE DATABASE app OWNER app;'
docker exec registry-backup-verify pg_restore --exit-on-error \
  -U restore_verifier -d app /tmp/registry.dump
docker exec registry-backup-verify psql -X -U restore_verifier -d app \
  -c 'SELECT count(*) FROM servers;' \
  -c 'SELECT version FROM schema_migrations ORDER BY version;'
docker rm -f -v registry-backup-verify
```

Check restore exit status, schema, row counts, and application queries. Use a
compatible PostgreSQL client major version. Repeat isolated recovery drills
after backup or database upgrades; repository integrity alone does not prove
that PostgreSQL can recover the archive.

### Backup alerts

Provision/update GCP failure and freshness alerts with:

```bash
python3 tools/admin/configure-backup-alerts.py \
  --project mcp-registry-prod --cluster mcp-registry-prod --email YOUR_EMAIL --dry-run
# Review, then rerun without --dry-run.
```

The script reuses an email channel and updates only its labelled policies. It
alerts on dump/restore-tool/repository errors and on no successful, fully checked
backup in 25 hours, including complete absence of the success metric. Log metrics
are not backfilled: run a fresh job after provisioning and verify that the success
metric appears in Cloud Monitoring. The freshness policy initially alerts until
success is observed. Alert delivery and channel verification should be checked
with the notification recipient.

Use explicit Kubernetes contexts when inspecting status:

```bash
kubectl --context=YOUR_CONTEXT -n default get cronjob registry-database-backup
kubectl --context=YOUR_CONTEXT -n default get pods -l app=registry-database-backup
kubectl --context=YOUR_CONTEXT -n default logs job/JOB_NAME -c pg-dump
kubectl --context=YOUR_CONTEXT -n default logs job/JOB_NAME -c restic
```

## Troubleshooting

To configure kubectl to access an existing GKE cluster:

```bash
# Login
gcloud auth login
gcloud auth application-default login

# For production
gcloud container clusters get-credentials mcp-registry-prod --zone us-central1-b --project mcp-registry-prod

# For staging
gcloud container clusters get-credentials mcp-registry-staging --zone us-central1-b --project mcp-registry-staging
```

### Check Status

```bash
kubectl get pods
kubectl get deployment
kubectl get svc
kubectl get gateway,httproute -n ingress-nginx
kubectl get svc -n ingress-nginx
```

### View Logs

```bash
kubectl logs -l app=mcp-registry
kubectl logs -l app=postgres
```

### Check Backup Status
```bash
kubectl describe cronjob registry-database-backup -n default
kubectl get jobs -n default
```

### Current hosted capacity

Staging uses **two e2-standard-2 nodes**: one isolated registry database node and
one shared worker. `gatewayStagingCompact: true` fixes the application and Gateway
at one replica. The worker pool has minimum=maximum=1; node upgrades use zero surge.
This cost-limited environment allows interruption and is not highly available.
See [compact staging](gateway-canary/compact-staging.md) for the resource budget.

Production retains **2–7 e2-standard-4 workers plus one isolated database node**.
Gateway proxies have an HPA of 3–16 replicas and Vector runs on the workers.
The application HPA is 2–6 replicas with a 60% CPU target; see
[production app CPU target](#production-app-cpu-target). Do not copy staging's node cap or
compact requests into production. The instantaneous production node count can
change within these bounds; the migration did not reduce the capacity limits.

Staging follows the current main build digest, with no committed `stagingImage`
or `registryRolloutCommit` override. Production follows the explicit `imageTag`
release configuration. The existing production rollout annotation does not block
new images and is retained to avoid an unrelated rollout.

The prior idle/burst experiments and one-off capacity adoption are complete.
Their procedures are available in [the pre-cleanup deployment guide](https://github.com/modelcontextprotocol/registry/blob/d6d6b5ea1664c6b2b449ef9a518d9c92a825547d/deploy/README.md#node-capacity).
Historical comparison and failed load gates remain in `tools/ingressbench/` and
`gateway-canary/`; they are evidence, not the current desired topology.
Use full-stack previews and the normal release flow for subsequent changes.

## Historical scaling rollout procedures

The following isolation/scaling procedures describe earlier promotions and remain
for recovery context. They are not required for ordinary releases or the completed
Gateway migration. Use [current hosted capacity](#current-hosted-capacity) and the
normal full-stack deployment commands for the active configuration. Legacy ingress
scaling sections apply only to the retained recovery code path.

### Production infrastructure-only isolation

`productionInfrastructureOnly: true` explicitly enables the isolated capacity
profile in production while retaining the configured application release. It
requires `coordinatedCapacity`, `isolatedDatabase`, `burstAutoscaling`,
`e2-standard-4` nodes, total node bounds 3–4, an 800m app CPU request, and a
PostgreSQL CPU request/limit of 3/3.

By default this profile keeps **two fixed app replicas** and leaves pool
configuration at the release defaults. Staging retains its separate 3–4 app
HPA and 30/5 pools.

The layout is one protected database node plus 2–3 worker nodes. Ingress scales
3–6 on CPU with 800m CPU/512Mi memory requests, a 70% target, and conservative
scale-down. Existing connections have a 60-second maximum keepalive age so new
controllers can receive traffic. Both singleton databases are protected from
node-autoscaler eviction; this does not provide database failover. The registry
DB move can interrupt service while its existing volume is reattached.

Before merging, an authorized maintainer should check out the proposed ref locally
and run `make -C deploy prod-preview` from the repository root, using production
credentials and the existing `deploy/passphrase.prod.txt` file. The GitHub Actions
production environment permits only `main`, so manual workflow previews must
select `main` and cannot preview an unmerged branch.
A production stack-config merge still triggers the automatic apply without an
additional deployment approval. The local preview and deployment both refresh
state first. Review the preview for unexpected replacement/deletion, especially
the cluster, registry database and PVC, before merging. Keep a fresh successful
backup and capture health, workload placement and request metrics through the
transition. Observe a real traffic cycle before declaring a performance fix.

If recovery requires reverting placement, retain the protected database pool
and existing volume while restoring workload scheduling/resources. A wholesale
configuration revert that attempts to delete the protected pool is not a safe
rollback procedure. Public node IPs, gateway configuration and application
release are outside this infrastructure promotion.

### Bounded production app scaling

Production releases from 1.8.7 support configurable connection pools. The
`productionAppScaling` opt-in extends the isolated production profile in two
separate applies:

1. Set `productionAppScaling: prepare`, `registryCPURequest: "1200m"` and
   `nodeMaxCount: "5"`. This keeps two fixed apps and rolls out explicit max/min
   pools of 30/5. Total node bounds become 3–5: one protected database node plus
   2–4 workers. Verify the rollout and pool settings before proceeding.
2. Set only `productionAppScaling: enabled`. This creates the app HPA with
   2–4 replicas and a 70% CPU target (840m per app). Scale-up permits two apps
   per minute; scale-down waits ten minutes and removes at most one every two
   minutes. Pulumi then leaves the replica count to the HPA.

The historical scaling promotion uses `scripts/production_apply.py` to keep raw
Pulumi output and plans in a private temporary directory outside the checkout,
then deletes them. Public logs contain fixed validation labels only; no raw
logs or plans are uploaded as artifacts. The wrapper validates the complete
input differences against this scaling profile, rejects unrelated changes or
replacements, and applies only the validated saved plan. Completed, TTL-expired ingress
setup jobs may be excluded only when their complete definitions match prior
state. RBAC reconciliation is likewise excluded only if its complete desired
definition matches prior state. A second preview targets just the reviewed
scaling resources and rejects any setup job or RBAC operation. All other
unrelated changes remain blocked. Future changes outside
this historical profile are handled by the normal full-stack release path above;
capacity transitions still require review and the live preflight.

A targeted preview can retain old dependency metadata for untargeted resources.
The wrapper reconciles only those dependency fields from two agreeing full
previews. Targeted resources and providers cannot be reconciled this way; all
input, output, operation, protection and identity constraints remain unchanged.
Pulumi still checks the resulting saved plan during apply.
For initial HPA creation, the targeted preview and apply omit refresh because
Pulumi requires refresh targets to exist already. The full preview still
refreshes live state, and saved-plan checks and the pre-apply live guard remain.

`make prod-preview` and `make prod-up` run a read-only transition preflight.
Activation requires a completed, healthy deployment with explicit 30/5 pools
on every live app and no old or terminating app pods. The preparation apply
must finish before activation: combining old 60-connection pools with four
new apps could exhaust database connections. Do not bypass the preflight with
an unguarded `pulumi up`.

The steady-state app connection ceiling is 120. Terminating pods can still
hold connections: zero surge is not a hard connection cap. Replacements must
remain Ready for 35 seconds, longer than the normal 30-second termination
window, to reduce routine rollout overlap. Monitor total database connections
and draining pods during changes; stalled termination or unreachable nodes
require investigation before continuing. Leave capacity for database
administration and other clients.

The legacy production app anti-affinity requires a different worker for each app,
including across revisions. A third or fourth app therefore needs a third or
fourth eligible worker. Zero surge lets replacements reuse a worker after its
old app terminates. If a worker fails at the ceiling, a replacement app may
remain Pending until worker capacity recovers; existing apps continue serving.

The CPU request reserves scheduling capacity and gives relative CPU weight;
it is neither a CPU limit nor an exclusive core. Check system pod requests,
memory and hostname spread when reviewing worker fit. Node autoscaling reacts
to unschedulable pods, not CPU utilization alone; raising its maximum does not
create ready capacity immediately. The extra worker is available on demand,
so scaling cannot guarantee absorption of an instantaneous burst.

For rollback, first return to `prepare` while keeping 1200m, 30/5 pools and the
3–5 node bounds. Verify the HPA is gone and exactly two healthy apps remain,
with no terminating pods. Only then set `productionAppScaling: off`, restore
800m and total node bounds 3–4. The transition preflight blocks a direct
return to legacy pools while the HPA or extra apps remain. Preserve the
protected database pool, existing volume, application release and networking.

### Incremental-sync production capacity

After migration 019 is prebuilt and release 1.8.9 is available, use two applies
on the existing production burst-headroom profile:

1. Set `productionSyncCapacity: prepare`, promote `imageTag` to 1.8.9 (or a later
   tested stable release), and update `registryRolloutCommit`. This rolls out
   max/min pools of 20/5 while retaining HPA 2–4 at 70% CPU.
2. Once every app is Ready on the new release with 20/5 pools and no old pods
   remain, set `productionSyncCapacity: enabled`. The HPA becomes 2–6 at 40%
   CPU (480m per app), adding at most two replicas per 30 seconds. Existing
   ten-minute scale-down stabilization and one-pod-per-two-minutes removal stay.

The steady-state app connection budget remains 120. Worker bounds, database
resources, gateway configuration and staging capacity are unchanged. The live
preflight rejects activation with binaries lacking configurable pools or any
30-connection app. A pool-capable prior release can be restored independently
while keeping the smaller pools and connection ceiling.

To roll back capacity, first return to `productionSyncCapacity: prepare` and
wait for at most four Ready apps and no terminating pods. Only then remove the
setting if 30/5 pools are desired. The preflight rejects larger pools while the
six-replica HPA remains. An application rollback can leave the additive index
in place; retain 20/5 pools until the smaller HPA ceiling has settled.

### Bounded production ingress scaling

With production app scaling enabled, `productionIngressScaling: true` and
`nodeMaxCount: "6"` enable an ingress-only capacity promotion. Ingress requests
800m CPU with 3–8 replicas. Its 70% CPU target preserves the 560m absolute
scaling target while retaining the lower scheduling reservation. There is no
CPU limit. Total node bounds are 3–6: one protected database
node and 2–5 workers. The worker minimum, app settings and database remain
unchanged. Additional workers are billed while provisioned, including any
scale-down delay; the higher maximum does not itself provision nodes.

The historical ingress-scaling wrapper permits only the ingress CPU request, ingress HPA
maximum/CPU target and worker maximum to change in this mode. It rejects app
changes and unrelated ingress configuration changes. Check rollout surge,
system pod requests, topology constraints and protected volumes when reviewing
scheduling capacity and eventual scale-down. New ingress replicas start with
cold caches, and per-controller rate limits mean additional replicas can
increase aggregate admitted traffic. Verify origin health as well as ingress
and worker CPU after promotion.

This opt-in requires the complete enabled production profile; staging keeps
its existing settings. Reverting this promotion, or subsequently changing app
scaling, requires a reviewed rollback plan and corresponding deployment guard
policy before changing the configuration.

### Prepared listing JSON activation

`MCP_REGISTRY_PREPARED_LISTING_JSON` defaults to `false`. This keeps typed
listing responses active while migration 018 adds nullable derived columns
and `tools/prepare-listing-json` backfills existing records in bounded batches.
New writes prepare their derived bytes regardless of the read-path switch.
For hosted deployments, explicitly set the Pulumi `preparedListingJSON`
boolean to control this variable. Omitting the setting preserves existing
pod templates during unrelated infrastructure changes.
Before enabling the switch, verify codec-1 coverage, response equivalence,
fresh metadata after writes, and representative burst/publish/recovery results
with the intended application resources and connection budget. The fallback
path preserves correctness but is not a substitute for backfill coverage.

For rollback, disable the switch to return to typed listing responses. Keep
migration 018 and the derived columns: older writers invalidate derived bytes
through its trigger. Recheck coverage before re-enabling after an older writer
has run. Rolling back the application does not require a database downgrade,
node-pool deletion, or volume replacement.

### Additional production burst headroom

`productionBurstHeadroom: true` extends the enabled production ingress profile
to 3–16 ingress replicas and 2–7 workers (`nodeMaxCount: "8"` includes the
protected database node). The 800m ingress reservation, 70% target, application
2–4 HPA, pools, and minimum node count remain unchanged. The private apply
guard permits only the ingress HPA maximum and worker maximum to change.
This adds on-demand headroom, not a latency guarantee or an immediate node
allocation. Validate placement, origin pressure, and recovery during real
traffic. App scaling beyond four requires a separate database budget review.

### Always-on functional staging

The small staging profile is already enabled. Its capacity and failure tradeoffs
are described in [current hosted capacity](#current-hosted-capacity) and
[compact staging](gateway-canary/compact-staging.md). The one-off resize helper is
retired. Any future node resize needs a new reviewed plan, database backups,
PVC identity checks and a maintenance window; ordinary releases do not resize nodes.

### Public listing bandwidth and access logs

`mcp-registry:publicReadCompression` enables gzip level 1 for unauthenticated,
cookie-free GET/HEAD requests to the public `/v0/servers` and `/v0.1/servers`
listing paths on the registry hosts. Compression runs after cache lookup, retains
identity responses for clients that do not accept gzip, and adds `Vary:
Accept-Encoding`. Compression does not change JSON payloads, authentication,
publishing, resource reservations or autoscaling bounds.

The Gateway public-read policy caches successful listing responses for 60 seconds
in staging and production. This reduces repeated origin reads at the cost of
allowing cached listings to remain fresh for an additional 30 seconds. Background
refresh and serving stale responses during refresh or upstream errors remain
enabled, so 60 seconds is not a strict maximum response age. Credential/cookie
bypass and cache keys are unchanged.

Before enabling sampling, the existing Pulumi service account needs exclusion
management permissions. Bootstrap a project custom role with only
`logging.exclusions.get`, `logging.exclusions.list`, `logging.exclusions.create`
and `logging.exclusions.update`, and bind it to that deployment account in each
environment. The role used for this deployment is `registryPublicReadLogExclusions`.
It does not grant log/bucket deletion, sink management or general Logging Admin
access. Rollback disables the managed exclusion instead of deleting it.

`mcp-registry:publicReadLogSampling` retains approximately 10% of fast successful
listing access records in the default Cloud Logging bucket. Only recognized
stdout records with status 200/304, request time below one second, and no failed
or retried upstream response are eligible. Errors, 499/429 responses, slow
requests, authentication, publishing, malformed/unrecognized records, other
containers and audit logs remain retained. This also samples eligible successful
background cache-refresh records. The rule uses `insertId` for sampling.

Gateway access records also carry `user_agent` and `content_encoding`. The first
is the request's `User-Agent` header as sent: a client claim, not a verified
identity. The second is `gzip` when the response left compressed and empty
otherwise. Use them to find clients that fetch listings uncompressed. Vector
metrics and the sampling rule read neither field. A user agent with bytes that
are not valid UTF-8 is logged unchanged, as the request URI already is; a parser
that rejects such a record counts a parse error instead of a request.

**Stored access-log counts and their latency distribution are no longer a census.**
A fully paginated log query only proves retrieval completeness. Use unsampled
Prometheus/Cloud Monitoring counters and histograms for traffic, error rates and
latency; use retained logs for examples and investigation. Do not extrapolate a
mixed retained log population by a single multiplier. Check log-based alert and
metric dependencies before changing the filter.

Both hosted environments have compression and log sampling enabled. Gateway
policies preserve the public listing cache and gzip/Vary behavior; the former
ingress ConfigMap-only apply wrapper is retired. Use the normal full-stack
preview/apply workflow for changes, and recheck gzip/identity equivalence,
cache behavior, credential/cookie handling and metric/log selection in staging.
Keep the sampling configuration key when disabling sampling so the exclusion is
disabled rather than removed. Previously excluded log records cannot be recovered.

### November 2026 component security upgrade

`mcp-registry:securityPatch202611: "true"` remains enabled in both hosted stacks.
It selects CloudNativePG 1.30.1 (chart 0.29.1) and Grafana 12.4.14 (chart 9.4.4).
Its ingress-nginx 1.15.1/chart 4.15.1 selection remains available for the legacy
code path, but neither hosted environment runs that controller. PostgreSQL stays
pinned to 17.5; an engine upgrade requires its own qualification.

The one-off security upgrade wrapper and its exact-transition allowlist are
retired. Future component upgrades use reviewed configuration changes, staging
qualification and the normal saved-plan release path. Do not disable the security
profile or downgrade CNPG CRDs as an unreviewed rollback. Grafana schema changes
may require restoring a compatible database backup rather than changing its image.
The original upgrade's one-off private backup archives are retired after the
rollback window. Future rollback work must use an appropriate current backup.
Historical procedures and archive hashes remain in
[the pre-cleanup guide](https://github.com/modelcontextprotocol/registry/blob/d6d6b5ea1664c6b2b449ef9a518d9c92a825547d/deploy/README.md#november-2026-component-security-upgrade).

#### ingress-nginx replacement

Both staging and production now use OSS Gateway Fabric. The old controller,
Ingress/IngressClass objects, admission resources, duplicate canary and legacy
ACME proxy are retired. Historical `ingress-nginx` Service/namespace and
`gateway-canary` metric/resource names identify active infrastructure and must
not be removed or renamed as cleanup.

See [production completion](gateway-canary/production-completion.md) for final
validation and [production recovery](gateway-canary/production-migration.md#rollback)
for Git-based recovery. There is no running legacy fallback. The migration-phase
code and its recovery tests remain to preserve that reviewed path and local
support. Historical [staging qualification](gateway-canary/standalone-validation.md)
and [handoff](gateway-canary/handoff-validation.md) reports retain their original
failures and scope limits; functional production validation is not a maximum-capacity benchmark.

## Incremental-sync page index

Migration 020 replaces migration 019's included-column timestamp index with
`idx_servers_updated_name_cover` on `(server_name, version, updated_at)` including
`is_latest` and `status`. It also replaces the single-column timestamp index with
`idx_servers_updated_time_cover` on `(updated_at DESC, server_name, version)`.
Both include deleted versions needed by incremental-sync clients. Migration 020
retains the old indexes; remove them concurrently in a separate post-rollout step.

For first-page `updated_since` queries with `limit=100`, the query probes up to
513 matching keys in timestamp order. With at most 512 matches it sorts that
complete set into name/version order; otherwise it fetches the requested page in
name order. Other first-page incremental shapes without substring search retain
the smaller 129-key probe and 128-match boundary. The larger probe avoids switching to a
costly name scan for moderately sparse pages. To avoid repeatedly paying for that
probe on large histories, each app samples one in 16 eligible plain 100-row
requests and retains up to 32 query-strategy hints for five seconds. A hint
records a timestamp/name range containing more than 512 matches. Requests
covered by that range can skip the probe. Plain cursor pages first inspect up to
512 eligible keys in name/version order after the cursor and apply the timestamp
filter. If that window supplies the requested page, or exhausts the remaining
name range, it returns those matches without a timestamp scan. Otherwise it
probes up to 513 matching keys in timestamp order, sorts a complete result of at
most 512 keys, or falls back to the full name-ordered query. This preserves the
cheap timestamp path for sparse pages while avoiding the consumed timestamp
prefix for dense deep pages and exhausted suffixes. It also supports `limit=1`,
the default page size, and legacy name-only cursors. Cursor pages with additional
filters retain the direct name seek. The timestamp probe bounds returned keys,
not all index entries visited; sparse deep pages can still scan a consumed
timestamp prefix. No selection path caches response rows.
The plain cursor query evaluates its cutoff and cursor parameters in one
materialized row so varying parameter estimates do not prevent plan reuse.
Hints are separated by deletion filtering and are not used for cursor pages,
caller-owned transactions or additional filters.
They contain no response data: writes can make a hint suboptimal until it expires,
but every request still applies the
complete predicate to current data. Sampled queries return an internal marker
row that is removed before pagination, serialization and returned-row metrics.
All selection branches use the full predicate and one statement snapshot, with
payloads fetched only for the selected page. Timestamp order never becomes the
public pagination order. Plain incremental queries with page sizes 1 or 100 use
the validated page size embedded in the query; cutoffs and cursors remain
parameters. The exact page bound lets PostgreSQL consider reusing a generic plan
instead of repeatedly planning the common shapes. All incremental shapes respect
the connection's configured execution mode (including a disabled statement
cache); the default mode preserves prepared-statement reuse and binary payload
transfer. Substring searches retain their existing parameter-specific planning.
No global plan-cache setting is changed.
The probe bounds returned keys, not all index entries examined. Very large recent
histories, sparse cursors and different filter mixes still need measurement.

On a busy existing database, **prebuild both indexes before deploying** the
application. These steps assume migration 019 is already applied; older
deployments must also prebuild indexes for their earlier pending migrations.
Startup migrations run inside a transaction and cannot use
`CONCURRENTLY`; without a prebuild they use regular index creation, which blocks
writes during the build. Use a maintenance window if prebuilding is not possible.
Run these as separate statements in autocommit, outside a transaction:

```sql
SET application_name = 'registry-updated-listing-index-build';
SET lock_timeout = '5s';
SET statement_timeout = '10min';
CREATE INDEX CONCURRENTLY idx_servers_updated_name_cover
    ON public.servers (server_name, version, updated_at) INCLUDE (is_latest, status);
CREATE INDEX CONCURRENTLY idx_servers_updated_time_cover
    ON public.servers (updated_at DESC, server_name, version);

SELECT c.relname, i.indisvalid, i.indisready, pg_get_indexdef(i.indexrelid)
FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
WHERE c.relname IN ('idx_servers_updated_name_cover', 'idx_servers_updated_time_cover');
```

Check both definitions and both readiness flags before deployment. Concurrent
builds consume CPU, I/O and temporary disk space; the old indexes remain until
the separate post-rollout cleanup. If a build fails, inspect its state, drop only that new
incomplete index with `DROP INDEX CONCURRENTLY`, and retry in autocommit. Do not
use `IF NOT EXISTS` to assume a failed prebuild is usable.

Migration 020 checks for each index before issuing CREATE INDEX, avoiding the
write-blocking lock that even CREATE INDEX IF NOT EXISTS requests for an existing
index. It validates both replacements and retains the old indexes. Its five-second
lock timeout bounds lock acquisition, not the duration of a missing-index build.
A failure rolls back the migration; investigate before retrying. It does not
change table data, connection limits, HPA targets or replica counts.

After rollout validation and the application rollback observation window, remove
only the two obsolete indexes using separate statements in autocommit. Ordinary
DROP INDEX inside a startup transaction can block both readers and writers.
Keeping all four temporarily costs disk space and write maintenance, so schedule
this cleanup explicitly. These commands must not run inside a transaction:

```sql
SET lock_timeout = '5s';
SET statement_timeout = '10min';
DROP INDEX CONCURRENTLY IF EXISTS public.idx_servers_updated_page_cover;
DROP INDEX CONCURRENTLY IF EXISTS public.idx_servers_updated_at;
```

Inspect index validity/state after any interrupted concurrent drop before retrying.
An application rollback remains schema-compatible with the replacement indexes,
but its planner choices and performance may differ. Leave the replacements and
any retained old indexes in place during an application rollback and measure
behavior. After cleanup, restoring the exact old physical layout requires a
separate concurrent prebuild and reviewed cleanup.
Validate sparse/empty windows, early/deep cursors, `limit=1`, publishing, deleted
records and latest/version/search filters before production deployment. Compare
completed request rate, latency, CPU and pool queues at matched workloads.

### Packed production workers

`productionPackedPlacement: true` requires enabled `productionSyncCapacity` and
standalone production Gateway. App anti-affinity becomes a preference, while
revision-aware hostname spreading remains required. With the current
`e2-standard-4` allocatable CPU, system/DaemonSet reservations and 1200m app
request, at most two apps fit on a worker. This is a resource budget, not a
portable pod-count cap; reassess it before changing worker size or reservations.
The HPA remains 2–6 and each app keeps pools 20/5 (120 maximum
steady-state app connections, with possible terminating-pod overlap).

Production gateways also spread each revision independently, so draining old
pods cannot bias replacement placement. There is no minimum domain count or
required gateway anti-affinity: three gateways may share two workers as 2+1.
Topology spread guides scheduling, not ReplicaSet deletion, so later HPA
scale-down can leave temporary skew until pods are rescheduled. Monitor actual
placement and autoscaler decisions instead of assuming a permanent even layout.

The configured total node bounds 3–8 include the protected database node;
worker bounds remain 2–7. At idle, two apps and three gateways fit across two
workers. Consolidation is automatic and depends on load, requested resources,
PDBs and autoscaler eligibility; do not manually drain to force the target.
Disabling packed placement restores one-app-per-worker behavior and may add
workers. Keep the smaller connection pools and inspect a refreshed preview.

### Production app CPU target

`productionAppCPUTarget` sets the app HPA's CPU utilization target as a whole
percentage of the app CPU request. It requires enabled `productionSyncCapacity`
and accepts 40–70. Without it the target stays at 40%.

Production uses 60%, which is 720m per app with the 1200m request. At 40% (480m),
two idle apps ran at 30–36% of their request, so ordinary variation kept adding a
third app, and a third app does not fit on the two baseline workers. Overnight on
November 10–11 the cluster held four nodes 86% of the time. With the earlier 70%
target it held three nodes 88% of the time.

The replica range, scale-up and scale-down rates, connection pools and node bounds
are unchanged. Scale-out starts later, so a sharp burst can queue while a new app
and worker start; one such start took about a minute. After changing the value,
compare listing latency, in-flight requests and node count through a daily peak
and an idle period. To roll back, set 40 or remove the key.
