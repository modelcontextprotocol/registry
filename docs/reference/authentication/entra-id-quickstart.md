# Quick Start: Azure Entra ID Authentication

This guide shows you how to quickly set up and use Azure Entra ID authentication with the MCP Registry.

## Prerequisites

- Azure subscription
- Azure CLI installed (`az`)
- Access to create App Registrations in Azure AD

## Step 1: Create Azure App Registration

```bash
# Login to Azure
az login

# Create app registration
az ad app create \
  --display-name "MCP Registry Authentication" \
  --sign-in-audience AzureADMyOrg

# Get the Application (client) ID and Tenant ID
APP_ID=$(az ad app list --display-name "MCP Registry Authentication" --query "[0].appId" -o tsv)
TENANT_ID=$(az account show --query tenantId -o tsv)

echo "Application ID: $APP_ID"
echo "Tenant ID: $TENANT_ID"
```

## Step 2: Configure Registry

Set these environment variables on your registry server:

```bash
export MCP_REGISTRY_ENTRA_ID_ENABLED=true
export MCP_REGISTRY_ENTRA_ID_TENANT_ID="$TENANT_ID"
export MCP_REGISTRY_ENTRA_ID_CLIENT_ID="$APP_ID"

# Option 1: Grant access to ALL namespaces (recommended for internal registries)
export MCP_REGISTRY_ENTRA_ID_NAMESPACE_PATTERN="*"

# Option 2: Restrict to specific namespaces
# export MCP_REGISTRY_ENTRA_ID_NAMESPACE_PATTERN="com.{reversed_domain}.*"
# export MCP_REGISTRY_ENTRA_ID_SIMPLE_NAMESPACE="{company}/*"
```

Restart the registry for changes to take effect.

## Step 3: Publish Using Entra ID

```bash
#!/bin/bash

# Get your Azure token
AZURE_TOKEN=$(az account get-access-token \
  --resource "$APP_ID" \
  --query accessToken -o tsv)

# Exchange for registry token
REGISTRY_TOKEN=$(curl -s -X POST \
  https://registry.modelcontextprotocol.io/v0/auth/entra-id \
  -H "Content-Type: application/json" \
  -d "{\"access_token\": \"$AZURE_TOKEN\"}" \
  | jq -r '.registry_token')

# Publish your server
curl -X POST \
  https://registry.modelcontextprotocol.io/v0/publish \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $REGISTRY_TOKEN" \
  -d @server.json
```

## Azure Pipeline Example

```yaml
# azure-pipelines.yml
trigger:
  - main

pool:
  vmImage: 'ubuntu-latest'

steps:
  - task: AzureCLI@2
    displayName: 'Publish to MCP Registry'
    inputs:
      azureSubscription: 'your-azure-service-connection'
      scriptType: 'bash'
      scriptLocation: 'inlineScript'
      addSpnToEnvironment: true
      inlineScript: |
        # Get token
        TOKEN=$(az account get-access-token \
          --resource $(ENTRA_ID_CLIENT_ID) \
          --query accessToken -o tsv)
        
        # Get registry token
        REGISTRY_TOKEN=$(curl -s -X POST \
          https://registry.modelcontextprotocol.io/v0/auth/entra-id \
          -H "Content-Type: application/json" \
          -d "{\"access_token\": \"$TOKEN\"}" \
          | jq -r '.registry_token')
        
        # Publish
        curl -X POST \
          https://registry.modelcontextprotocol.io/v0/publish \
          -H "Content-Type: application/json" \
          -H "Authorization: Bearer $REGISTRY_TOKEN" \
          -d @server.json
    env:
      ENTRA_ID_CLIENT_ID: $(APP_ID)
```

## Namespace Pattern Examples

Your namespace pattern determines what you can publish:

### Option 1: Wildcard (Internal Registries)

**Configuration:**
```bash
ENTRA_ID_NAMESPACE_PATTERN="*"
```

**Allows publishing ANY server:**
- ✅ `io.github.domdomegg/airtable-mcp-server` (public servers)
- ✅ `microsoft/azure-devops-mcp` (third-party servers)
- ✅ `yourcompany/internal-server` (your servers)

**Use case:** Internal company registry where authenticated users curate external MCP servers.

### Option 2: Specific Namespaces

| Pattern | User Email | Allowed Namespace |
|---------|-----------|-------------------|
| `com.{reversed_domain}.*` | `user@contoso.com` | `com.contoso.*` |
| `io.azure.{domain}.*` | `user@fabrikam.com` | `io.azure.fabrikam.com.*` |
| `com.microsoft.entra.{tenant_id}.*` | (any user) | `com.microsoft.entra.<tenant-guid>.*` |

**Use case:** Public or shared registry with namespace-based access control.

## Next Steps

- Read the [full Entra ID documentation](./entra-id.md) for advanced configuration
- Learn about [namespace patterns](./entra-id.md#namespace-pattern-configuration)
- Set up [managed identities](./entra-id.md#3-using-managed-identity-azure-vmcontainer)
- Configure [service principals](./entra-id.md#1-using-service-principal-azure-pipeline)

## Troubleshooting

**Can't get token?**
```bash
# Check you're logged in
az account show

# Try interactive login
az login --allow-no-subscriptions
```

**Token validation fails?**
- Verify tenant ID matches: `az account show --query tenantId`
- Check app ID is correct: `az ad app list --display-name "MCP Registry Authentication"`
- Ensure registry configuration is correct

**Wrong namespace?**
- Check your email domain: It's extracted from your Azure AD user principal name
- Verify namespace pattern configuration in registry
