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
| `mcp-registry:imageTag` | Docker image tag for production environment | Yes (prod only) |
| `gcp:project` | GCP Project ID | Yes when `provider=gcp` |
| `gcp:region` | GCP Region | No (default: us-central1) |
| `gcp:credentials` | GCP service account credentials JSON | No (secret; set in staging/prod) |

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
The application retains its 2–4 replica HPA. Do not copy staging's node cap or
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

Production app anti-affinity requires a different worker for each app,
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
preflight rejects activation with older binaries or any 30-connection app.

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

Migration 019 adds `idx_servers_updated_page_cover` on `(server_name, version)`
including `updated_at`, `is_latest`, and `status`. Unlike the partial search
index, it includes deleted versions required by incremental sync. Incremental
queries select a page of keys before fetching payloads. They retain prepared
statement reuse, binary payload transfer, and PostgreSQL automatic plan selection.
Forcing unnamed text-format execution regressed concurrent throughput in staging. The
watermark comparison, cursor ordering, filters, and response format are unchanged.

On a busy existing database, prebuild before deploying the application:

```sql
SET application_name = 'registry-updated-page-index-build';
SET lock_timeout = '5s';
SET statement_timeout = '10min';
CREATE INDEX CONCURRENTLY idx_servers_updated_page_cover
    ON public.servers (server_name, version) INCLUDE (updated_at, is_latest, status);
```

Run in autocommit, outside a transaction. Verify the definition and both
`indisvalid` and `indisready` before deploying. The migration creates the index
on fresh databases and rejects an existing invalid index. A concurrent build
uses CPU, I/O, and disk space; monitor it and repair any incomplete build before
retrying.

Compare recent, old, and empty incremental-sync windows, deep cursors, deleted
records, and latest/version filters. Measure SQL execution and buffer access as
well as end-to-end latency, pool queues, CPU, and actual completed request rate.
Rolling back the application can leave this additive index in place; removal
is optional and should happen only after all candidate instances are gone.
