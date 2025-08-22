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

**Note:** The production deployment is automatically handled by GitHub Actions. All merges to the `main` branch trigger an automatic deployment to GCP via [the configured GitHub Actions workflow](../.github/workflows/deploy.yml). The steps below are preserved as a log of what we did, or if a manual override is needed.

Pre-requisites:
- [Pulumi CLI installed](https://www.pulumi.com/docs/iac/download-install/)
- A Google Cloud Platform (GCP) account
- A GCP Service Account with appropriate permissions

1. Create a project: `gcloud projects create mcp-registry-prod`
2. Set the project: `gcloud config set project mcp-registry-prod`
3. Enable required APIs: `gcloud services enable storage.googleapis.com && gcloud services enable container.googleapis.com`
4. Create a service account with necessary permissions, and get the key:
   ```bash
   gcloud iam service-accounts create pulumi-svc
   sleep 10
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/container.admin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/compute.admin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/storage.admin"
   gcloud projects add-iam-policy-binding mcp-registry-prod --member="serviceAccount:pulumi-svc@mcp-registry-prod.iam.gserviceaccount.com" --role="roles/storage.hmacKeyAdmin"
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

<!--

### Production Deployment (Azure)

**Note:** This is how the production deployment will be set up once. But then the plan will be future updates are effectively a login + `pulumi up` from GitHub Actions.

Pre-requisites:
- [Pulumi CLI installed](https://www.pulumi.com/docs/iac/download-install/)
- A Microsoft Azure account
- [Azure CLI](https://learn.microsoft.com/en-gb/cli/azure/get-started-with-azure-cli) installed

1. Login to Azure: `az login`
2. Create a resource group: `az group create --name official-mcp-registry-prod --location eastus`
3. Add the storage resource provider: `az provider register --namespace Microsoft.Storage`
4. Create a storage account: `az storage account create --name officialmcpregistryprod --resource-group official-mcp-registry-prod --location eastus --sku Standard_LRS`
5. Add the 'Storage Blob Data Contributor' role assignment for yourself on the storage account: `az role assignment create --assignee $(az ad signed-in-user show --query id -o tsv) --role "Storage Blob Data Contributor" --scope "/subscriptions/$(az account show --query id -o tsv)/resourceGroups/official-mcp-registry-prod"`
6. Create a container: `az storage container create --name pulumi-state --account-name officialmcpregistryprod`
7. Set Pulumi's backend to Azure: `pulumi login 'azblob://pulumi-state?storage_account=officialmcpregistryprod'`
8. Init the production stack: `pulumi stack init aksProd`
  - TODO: This has a password that maybe needs to be shared with select contributors?
9. Deploy: `go build && PULUMI_CONFIG_PASSPHRASE="" pulumi up --yes`
10. Access the repository via the ingress load balancer. You can find its external IP with `kubectl get svc ingress-nginx-controller -n ingress-nginx` or view it in the Pulumi outputs. Then run `curl -H "Host: prod.registry.modelcontextprotocol.io" -k https://<EXTERNAL-IP>/v0/ping` to check that the service is up.

-->

## Structure

```
├── main.go              # Pulumi program entry point
├── Pulumi.yaml          # Project configuration
├── Pulumi.local.yaml    # Local stack configuration
├── Pulumi.prod.yaml     # Production stack configuration (Azure)
├── Pulumi.gcp.yaml      # Production stack configuration (GCP)
├── Makefile             # Build and deployment targets
├── go.mod               # Go module dependencies
├── go.sum               # Go module checksums
└── pkg/                 # Infrastructure packages
    ├── k8s/             # Kubernetes deployment components
    │   ├── backup.go          # Database backup configuration
    │   ├── cert_manager.go    # SSL certificate management
    │   ├── deploy.go          # Deployment orchestration
    │   ├── ingress.go         # Ingress controller setup
    │   ├── postgres.go        # PostgreSQL database deployment
    │   └── registry.go        # MCP Registry deployment
    └── providers/       # Kubernetes cluster providers
        ├── types.go           # Provider interface definitions
        ├── aks/               # Azure Kubernetes Service provider
        ├── gcp/               # Google Kubernetes Engine provider
        └── local/             # Local kubeconfig provider
```

### Architecture Overview

#### Deployment Flow
1. Pulumi program starts in `main.go`
2. Configuration is loaded from Pulumi config files
3. Provider factory creates appropriate cluster provider (AKS or local)
4. Cluster provider sets up Kubernetes access
5. `k8s.DeployAll()` orchestrates complete deployment:
   - Certificate manager for SSL/TLS
   - Ingress controller for external access
   - Database for data persistence
   - Backup infrastructure for database
   - MCP Registry application

## Configuration

| Parameter | Description | Required |
|-----------|-------------|----------|
| `environment` | Deployment environment (dev/prod) | Yes |
| `provider` | Kubernetes provider (local/aks/gcp) | No (default: local) |
| `githubClientId` | GitHub OAuth Client ID | Yes |
| `githubClientSecret` | GitHub OAuth Client Secret | Yes |
| `gcpProjectId` | GCP Project ID (required when provider=gcp) | No |
| `gcpRegion` | GCP Region (default: us-central1) | No |

## Database Backups

The deployment uses [K8up](https://k8up.io/) (a Kubernetes backup operator) that uses [Restic](https://restic.net/) under the hood.

When running locally they are stored in a Minio bucket. In staging and production, backups are stored in a GCS bucket.

### Accessing Backup Files

#### Local Development (MinIO)

```bash
# Expose MinIO web console
kubectl port-forward -n minio svc/minio 9000:9000 9001:9001
```

Then open [localhost:9001](http://localhost:9001), login with username `minioadmin` and password `minioadmin`, and navigate to the k8up-backups bucket.

##### Staging and Production (GCS)

- [Staging](https://console.cloud.google.com/storage/browser/mcp-registry-staging-backups?project=mcp-registry-staging)
- [Production](https://console.cloud.google.com/storage/browser/mcp-registry-prod-backups?project=mcp-registry-prod)

#### Decrypting and Restoring Backups

Backups are encrypted using Restic. To access the backup data:

1. **Download the backup files from the bucket:**
   ```bash
   # Local (MinIO) - ensure port-forward is active: kubectl port-forward -n minio svc/minio 9000:9000 9001:9001
   AWS_ACCESS_KEY_ID=minioadmin AWS_SECRET_ACCESS_KEY=minioadmin \
     aws --endpoint-url http://localhost:9000 s3 sync s3://k8up-backups/ ./backup-files/
   
   # GCS (staging/production)
   gsutil -m cp -r gs://mcp-registry-{staging|prod}-backups/* ./backup-files/
   ```
2. **[Install Restic](https://restic.readthedocs.io/en/latest/020_installation.html)**
3. **Restore the backup:**
   ```bash
   RESTIC_PASSWORD=password restic -r ./backup-files restore latest --target ./restored-files
   ```
   PostgreSQL data will be in `./restored-files/data/registry-pg-1/pgdata/`

## Troubleshooting

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
