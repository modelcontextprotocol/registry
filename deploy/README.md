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

**Note:** Deployments are automatically handled by GitHub Actions with separate workflows for staging and production:

- **Staging:** All merges to the `main` branch automatically build and deploy the latest code to the `staging` environment via [deploy-staging.yml](../.github/workflows/deploy-staging.yml). Staging always uses the `:main` Docker image tag.

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

3. The production deployment workflow will automatically trigger and deploy the specified version

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
Their legacy operator, credentials and stored objects are retained; the old
K8up Schedule is removed. No automatic staging backup smoke test is run.
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
Legacy prefixes are excluded so their recovery evidence is preserved; since
their schedules are retired, they do not continually accumulate new versions.

The legacy K8up Schedule is removed. Clearing its spec can leave defaulted
backup/prune entries without cron expressions and crash the K8up operator.
The operator and credentials remain available. Existing objects
at the bucket root and in `postgres-logical-v1` are preserved for investigation;
they are not a verified recovery source and are never pruned by the new job.
The old physical-data production-to-staging sync workflow is guarded against
execution because it is incompatible with the current archive format.

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

### Adopting the production repair into Pulumi

The October 2026 repair created `default/registry-pg-backup` and
`default/registry-database-backup` directly in production. They must be imported
before the first production `pulumi up` using this code; a normal refresh does
not adopt unmanaged resources. The existing bucket, operator, and credentials
are already in the production state and must not be re-imported. The retired
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
kubectl get ingress
kubectl get svc -n ingress-nginx
```

### View Logs

```bash
kubectl logs -l app=mcp-registry
kubectl logs -l app=postgres
```

### Check Backup Status
```bash
kubectl describe schedule.k8up.io 
kubectl get backup
```

### Node capacity

`mcp-registry:nodeAutoscaling` enables GKE node scaling independently of the
experimental ingress/app profile. `nodeMinCount` and `nodeMaxCount` specify total
node bounds. Staging proposes **3–4 e2-standard-4 nodes**; production configuration
is unchanged until acceptance and a separate promotion. Changing its stack file
triggers the production workflow, so keep that change out of staging-only PRs. Always
refresh Pulumi state before applying, since the autoscaler owns `nodeCount`.
Both singleton database pods retain voluntary-eviction protection with either
node autoscaling or the legacy burst profile enabled.

The coordinated candidate keeps three app and three ingress replicas ready,
with maxima of four and six respectively. Reserving three nodes alone does not
prewarm caches or redistribute existing pods. Verify Ready replicas, placement,
load-balancer health and actual node consolidation during the acceptance test.
Keep staging's image, CPU/memory budget, pools and scaling settings aligned with
the intended production configuration until that test passes. Existing live
production remains the comparison baseline until promotion.

Staging's node bounds can be reduced independently later. Disable or revise the
coordinated profile first: its guard deliberately requires the tested 3–4 node
budget. Recheck pod fit and database protection before reducing capacity, and
restore production parity for future capacity acceptance tests. Scale-down can
be delayed by placement and disruption budgets; a minimum is not a guarantee.

### Burst capacity (staging validation)

The `coordinatedCapacity` and `isolatedDatabase` opt-ins are enabled in the
staging candidate. The latter creates one fixed, tainted database node and
subtracts it from the total node bounds, leaving two–three autoscaled workers.
PostgreSQL requests its full 3-CPU limit on the reserved node. App and ingress
pods require worker nodes and retain their per-revision hostname spread;
three idle replicas can therefore occupy two workers as 2+1. System agents
still share the database node. This remains a single PostgreSQL instance,
not database high availability. Node-upgrade surge can temporarily exceed the
four-node steady-state ceiling.

The isolated profile requires a digest-pinned `stagingImage` containing the
prepared-listing app changes. The override is staging-only; production retains
its release image. Import any manually created database pool and app HPA into
the staging Pulumi stack before applying this configuration, then refresh and
review a full preview. Do not create a second pool or replace the existing one.
The staging GitHub Actions job runs `scripts/adopt_staging_capacity.py` before
the normal apply, using its existing staging passphrase secret. The script
imports only missing resources with the exact expected staging identities and
providers, rejects conflicting ownership, and skips completed imports on retry.
The following apply refreshes state before planning. Keep this transition step
until the first successful adoption is recorded; it can then be removed.
See the [candidate and test gates](../tools/ingressbench/COORDINATED_CAPACITY.md)
for its 3–4 app HPA, 30/5 pools and required CPU settings. It is rejected outside
staging until the full validation passes. The configuration below describes the
earlier ingress-only profile.

This is an experimental configuration, not an approved production fix. In the
2026-10-27 staging comparison, CPU reservation protected database readiness, but
ingress/node scaling alone left long application pool queues and publish
timeouts. Adding a 60-second client connection lifetime did not resolve those
failures. The ingress HPA could remain below its average CPU target while the
database node was saturated and the app on that node queued requests. Do not
enable this in production without a successful end-to-end burst validation.
See `tools/ingressbench/VALIDATION.md` for the measured comparison and limitations.
The follow-up [ready-capacity comparison](../tools/ingressbench/WARM_CAPACITY_VALIDATION.md)
also failed with four nodes and six ingress replicas ready before the burst.
Keep the rollout gate on complete offered load and recovery, not only successful
HTTP attempts or quiet current metrics.

`mcp-registry:registryCPURequest` can override the application CPU reservation
for controlled staging comparisons. The default remains 250m per hosted app
replica (100m locally), with no CPU limit. Raising the reservation changes both
CPU sharing under contention and pod placement; it can prevent consolidation
onto two nodes even when the autoscaler's configured minimum remains two.
Measure node count and placement through recovery, not just request latency.
The replica count and each pool's 60 maximum / 10 minimum remain unchanged.

`mcp-registry:burstAutoscaling` opts a hosted stack into ingress and GKE node
scaling (legacy default 2–4 nodes; explicit node bounds take precedence). It is enabled in staging for validation; production keeps its existing
configuration until a separate promotion. Application replicas and their
connection pools stay fixed at two replicas, each with a maximum of 60 connections.

| Setting | Fixed baseline | With burst capacity |
| --- | --- | --- |
| GKE nodes | 2 | Configured bounds; hosted stack proposal is 3–4 |
| Ingress controllers | 2 | 2–6 |
| Ingress CPU request | 300m | 800m, no CPU limit |
| Ingress memory request | 90 MiB | 512 MiB |
| Registry DB CPU request | 200m | Configurable separately with `postgresCPURequest` |
| Registry DB CPU limit | 3 | Unchanged |

The HPA targets 88% of the 800m ingress request (704m), including work serving
cache hits. The request leaves room for the 2-CPU database and existing system
workloads on a baseline node; a 1-CPU ingress request did not fit that placement.
A rolling-update surge can still require a temporary extra node. Scale-up allows
two additional controllers per minute. Scale-down waits
for a ten-minute stabilization window, then removes at most one controller every
two minutes. These are bounds, not a guarantee of immediate capacity: metric
collection, pending-pod detection, node provisioning, readiness and load-balancer
health checks all add delay. The cluster autoscaler adds nodes for unschedulable
pods; high node CPU alone does not trigger it. It can remove spare nodes later,
subject to pod placement, disruption budgets and its own delays.

Both singleton PostgreSQL clusters inherit `safe-to-evict=false` on their pods
and enable CloudNativePG disruption budgets. This protects them from voluntary
autoscaler eviction, not node failure, operator restarts or manual deletion.
It can also block node maintenance; plan explicit database interruption when
upgrading nodes. Two protected database placements can constrain the minimum
node count. Verify actual scale-down rather than assuming the cluster reaches two.

`postgresCPURequest` reserves scheduling capacity and increases relative CPU
shares under contention. It is not a dedicated-core guarantee. Raising the
request can restart the singleton database, and the request must fit beneath
its CPU limit and alongside other workloads. Validate a request of 2 CPUs in
staging before promoting it. The initial staging rollout interrupted application
readiness while the singleton database restarted and moved to a new node. Plan
that interruption explicitly; ensure spare schedulable capacity is ready before
changing the database request. Memory, buffers, connection limits and readiness
thresholds are unchanged.

Each ingress has an independent cache. New replicas start cold and can increase
origin traffic before warming. Existing client connections may stay on older
replicas. With burst capacity enabled, client connections have a 60-second
active lifetime (`keepalive_time`); NGINX finishes the current request before
closing so clients can reconnect to newly added capacity. This differs from the
idle keep-alive timeout and does not abort a long-running request at 60 seconds.
It adds connection/TLS setup work, so validate both HTTP/1.1 and HTTP/2.
Rate limits are also per controller, so the aggregate allowance for a
client can rise as replicas are added; this does not introduce a global limiter.
Ingress metrics are scraped per pod every 30 seconds when burst capacity is on,
so cache-serving demand is visible separately from application arrivals. Cache
outcomes still require response headers or the existing ingress logs.

Before an update, refresh Pulumi state and inspect the full preview. The node
pool must update in place. Pulumi ignores `nodeCount` and ingress `spec.replicas`
while autoscaling is enabled, so subsequent deploys do not reset controller-owned
counts. Verify this again with a preview while scaled out. A configuration-map
replacement for the metrics scrape config is expected.

Validate the same sustained workload before and after: per-replica demand,
cache misses, listing and publish latency, errors, pool queues, PostgreSQL CPU
pressure/throttling, readiness, pending pods and node allocation. Require both
scale-up under load and recovery/scale-down after it stops. Do not promote based
on a quiet run or a synthetic workload that fails to reproduce the failure.
To disable autoscaling, remove the opt-in and preview the transition back to
fixed counts; scale-in and a database request rollback may interrupt service.

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

Run the production workflow manually on the proposed ref for a **preview only**.
A production stack-config merge still triggers the apply. Both paths refresh
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

The production GitHub workflow uses `scripts/production_apply.py` to keep raw
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
this profile need an explicitly reviewed validation policy; do not bypass it.

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
