# MCP Registry Kubernetes Deployment

This directory contains Pulumi infrastructure code to deploy the MCP Registry service to a Kubernetes cluster. It supports multiple Kubernetes providers: Azure Kubernetes Service (AKS) and local (using existing kubeconfig).

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
4. Deploy: `go build && PULUMI_CONFIG_PASSPHRASE="" pulumi up --yes`
5. Access the repository via the ingress load balancer. You can find its external IP with `kubectl get svc nginx-ingress-ingress-nginx-controller -n ingress-nginx` (with minikube, if it's 'pending' you might need `minikube tunnel`). Then run `curl -H "Host: mcp-registry-local.example.com" -k https://<EXTERNAL-IP>/v0/ping` to check that the service is up.

### Production Deployment (AKS)

WIP!

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

## Structure

```
├── main.go              # Pulumi program entry point
├── Pulumi.yaml          # Project configuration
├── Pulumi.local.yaml    # Local stack configuration
├── Pulumi.prod.yaml     # Production stack configuration
├── Makefile             # Build and deployment targets
├── go.mod               # Go module dependencies
├── go.sum               # Go module checksums
└── pkg/                 # Infrastructure packages
    ├── k8s/             # Kubernetes deployment components
    │   ├── cert_manager.go    # SSL certificate management
    │   ├── deploy.go          # Deployment orchestration
    │   ├── ingress.go         # Ingress controller setup
    │   ├── mongodb.go         # MongoDB deployment
    │   └── registry.go        # MCP Registry deployment
    └── providers/       # Kubernetes cluster providers
        ├── types.go           # Provider interface definitions
        ├── aks/               # Azure Kubernetes Service provider
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
   - MongoDB for data persistence
   - MCP Registry application

## Configuration

| Parameter | Description | Required |
|-----------|-------------|----------|
| `environment` | Deployment environment (dev/prod) | Yes |
| `provider` | Kubernetes provider (local/aks) | No (default: local) |
| `githubClientId` | GitHub OAuth Client ID | Yes |
| `githubClientSecret` | GitHub OAuth Client Secret | Yes |

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
kubectl logs -l app=mongodb
```
