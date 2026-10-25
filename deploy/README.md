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
