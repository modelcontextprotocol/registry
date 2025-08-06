# MCP Registry Kubernetes Deployment

This directory contains Pulumi infrastructure code to deploy the MCP Registry service to a Kubernetes cluster. It supports multiple Kubernetes providers: Azure Kubernetes Service (AKS) and local (using existing kubeconfig).

## Prerequisites

- Go 1.23.x
- Pulumi CLI installed
- kubectl configured
- For AKS: Azure CLI installed and authenticated
- For local: Access to a Kubernetes cluster via kubeconfig

## Structure

- `main.go` - Entry point for the Pulumi program
- `cluster.go` - Kubernetes cluster setup with support for AKS and local
- `registry.go` - MCP Registry application deployment
- `mongodb.go` - MongoDB deployment for Kubernetes
- `Pulumi.yaml` - Pulumi project configuration
- `Pulumi.dev.yaml` - Development environment configuration (local by default)
- `Pulumi.prod.yaml` - Production environment configuration (AKS by default)

## Supported Providers

| Provider | Description | Use Case |
|----------|-------------|----------|
| `local` | Local Kubernetes (default) | Development with existing cluster |
| `aks` | Azure Kubernetes Service | Production deployment |

## Configuration

Required configuration:

| Parameter | Description | Required |
|-----------|-------------|----------|
| `environment` | Deployment environment (dev/prod) | Yes |
| `provider` | Kubernetes provider (local/aks) | No (default: local) |
| `githubClientId` | GitHub OAuth Client ID | Yes |
| `githubClientSecret` | GitHub OAuth Client Secret | Yes |

Optional configuration:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `kubeconfigPath` | Path to kubeconfig (for local providers) | ~/.kube/config |
| `azureLocation` | Azure region (for AKS) | East US |

Hardcoded defaults:
- MongoDB: Deployed as part of the stack with persistent storage
- Registry Image: `registry:latest` (local build)
- Replicas: 3 for dev, 5 for prod
- Ingress: NGINX with TLS (LoadBalancer for AKS, NodePort for local)

## Quick Start

### Local Development

Pre-requisites:
- [Pulumi CLI installed](https://www.pulumi.com/docs/iac/download-install/)
- One of the following providers to deploy to:
  - A Kubernetes cluster via kubeconfig. You can run one locally with [minikube](https://minikube.sigs.k8s.io/docs/start/)
  - A Microsoft Azure account.

1. Set Pulumi's backend to local: `pulumi login --local`
2. Select the development stack: `pulumi stack init local` (fine to leave `password` blank)
3. Set your config:
    ```bash
    # General environment
    pulumi config set mcp-registry:environment local

    # To use your local kubeconfig (default)
    pulumi config set mcp-registry:provider local
    # Alternative: To use AKS
    # pulumi config set mcp-registry:provider aks
    
    # GitHub OAuth
    pulumi config set mcp-registry:githubClientId <your-github-client-id>
    pulumi config set --secret mcp-registry:githubClientSecret <your-github-client-secret>
    ```
4. Deploy: `pulumi up`
5. Access the MCP Registry:
```bash
# Port forward for local access
kubectl port-forward -n dev svc/mcp-registry 8080:8080
```

### Production Deployment (AKS)

1. Select the production stack:
```bash
cd deploy/infra
pulumi stack select prod
```

2. Configure Azure location (optional):
```bash
pulumi config set mcp-registry:azureLocation "East US"
```

3. Configure GitHub OAuth:
```bash
pulumi config set mcp-registry:githubClientId <your-github-client-id>
pulumi config set --secret mcp-registry:githubClientSecret <your-github-client-secret>
```

4. Deploy:
```bash
pulumi up
```

## Components Deployed

### Kubernetes Resources
- Namespace for the environment
- NGINX Ingress Controller (LoadBalancer for AKS, NodePort for local)
- cert-manager for TLS certificates

### MongoDB
- Single instance MongoDB deployment
- PersistentVolumeClaim for data storage (10Gi)
- ClusterIP service

### MCP Registry
- ConfigMap with non-sensitive configuration
- Secret with sensitive configuration
- Deployment (3 replicas for dev, 5 for prod)
- ClusterIP Service
- Ingress with TLS

### AKS Specific (when provider=aks)
- Azure Resource Group
- AKS Cluster with system-assigned managed identity
- Node pool with 3 nodes (dev) or 5 nodes (prod)
- Azure CNI networking

## Monitoring

The deployment includes health checks:
- Liveness probe: `/v0/health`
- Readiness probe: `/v0/health`

## Building the Docker Image

Before deployment, build the MCP Registry Docker image:

```bash
# From the registry root directory
docker build -t registry:latest .
```

## Troubleshooting

### Check Deployment Status
```bash
kubectl get pods -n <environment>
kubectl get svc -n <environment>
```

### View Logs
```bash
# MCP Registry logs
kubectl logs -n <environment> -l app=mcp-registry

# MongoDB logs
kubectl logs -n <environment> -l app=mongodb
```

### MongoDB Connection
The MCP Registry connects to MongoDB using the Kubernetes service DNS:
- Development: `mongodb://mongodb.dev.svc.cluster.local:27017`
- Production: `mongodb://mongodb.prod.svc.cluster.local:27017`

### Local Provider Issues
- Ensure your kubeconfig is correctly configured
- Verify the cluster is running: `kubectl cluster-info`
- Check if you have the correct context: `kubectl config current-context`

### AKS Provider Issues
- Ensure Azure CLI is authenticated: `az login`
- Check subscription: `az account show`
- Verify AKS credentials: `az aks get-credentials --resource-group <rg> --name <cluster>`

## Cleanup

To destroy the infrastructure:
```bash
pulumi destroy
```

For AKS, this will also delete the Azure Resource Group and all contained resources.