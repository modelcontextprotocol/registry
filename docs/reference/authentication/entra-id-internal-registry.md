# Internal Registry with Entra ID - Quick Setup

This guide is for setting up an **internal company registry** where authenticated colleagues can publish ANY MCP server (both public and internal).

## Use Case

You want to:
- ✅ Use Entra ID as the ONLY authentication method
- ✅ Allow colleagues to publish public MCP servers (e.g., `io.github.domdomegg/airtable-mcp-server`)
- ✅ Allow colleagues to publish internal servers (e.g., `yourcompany/internal-tool`)
- ✅ Trust authenticated users (they're your colleagues)

## Setup

### 1. Create Azure App Registration

```bash
az login
az ad app create --display-name "Internal MCP Registry"

APP_ID=$(az ad app list --display-name "Internal MCP Registry" --query "[0].appId" -o tsv)
TENANT_ID=$(az account show --query tenantId -o tsv)

echo "APP_ID=$APP_ID"
echo "TENANT_ID=$TENANT_ID"
```

### 2. Configure Registry (Wildcard Mode)

```bash
# Enable Entra ID with wildcard permissions
export MCP_REGISTRY_ENTRA_ID_ENABLED=true
export MCP_REGISTRY_ENTRA_ID_TENANT_ID="$TENANT_ID"
export MCP_REGISTRY_ENTRA_ID_CLIENT_ID="$APP_ID"
export MCP_REGISTRY_ENTRA_ID_NAMESPACE_PATTERN="*"
export MCP_REGISTRY_ENTRA_ID_ALLOW_EDIT=true
```

**Important:** `NAMESPACE_PATTERN="*"` grants permission to publish ANY server name.

### 3. Publish Any Server

Your colleagues can now publish any server:

```bash
# Authenticate
TOKEN=$(az account get-access-token --resource $APP_ID --query accessToken -o tsv)
REGISTRY_TOKEN=$(curl -s -X POST https://your-registry.com/v0/auth/entra-id \
  -H "Content-Type: application/json" \
  -d "{\"access_token\": \"$TOKEN\"}" \
  | jq -r '.registry_token')

# Publish public server
curl -X POST https://your-registry.com/v0/publish \
  -H "Authorization: Bearer $REGISTRY_TOKEN" \
  -d @server.json
```

## Example: Publishing Public MCP Servers

### Airtable MCP Server (from GitHub)

```json
{
  "name": "io.github.domdomegg/airtable-mcp-server",
  "description": "Read and write access to Airtable",
  "version": "1.7.2",
  "packages": [...]
}
```

✅ **Allowed** - Wildcard `*` matches `io.github.domdomegg/airtable-mcp-server`

### Microsoft Azure DevOps MCP

```json
{
  "name": "microsoft/azure-devops-mcp",
  "description": "Azure DevOps integration",
  "version": "1.0.0",
  "packages": [...]
}
```

✅ **Allowed** - Wildcard `*` matches `microsoft/azure-devops-mcp`

### Your Internal Server

```json
{
  "name": "yourcompany/inventory-system",
  "description": "Internal inventory MCP server",
  "version": "1.0.0",
  "packages": [...]
}
```

✅ **Allowed** - Wildcard `*` matches `yourcompany/inventory-system`

## Security Considerations

### ✅ Good for Internal Registries

- Registry is **not public** (internal network only)
- Users are **authenticated** via company Entra ID
- Team is **trusted** (colleagues, not anonymous users)
- Curating **known servers** (not accepting arbitrary submissions)

### ⚠️ Not Recommended for Public Registries

If your registry is public-facing, consider namespace restrictions:

```bash
# Restrict to company namespaces
ENTRA_ID_NAMESPACE_PATTERN=com.yourcompany.*,yourcompany/*
```

## Automated Publishing Workflow

### Azure DevOps Pipeline

```yaml
trigger:
  - main

pool:
  vmImage: 'ubuntu-latest'

steps:
  - task: AzureCLI@2
    displayName: 'Publish MCP Server to Internal Registry'
    inputs:
      azureSubscription: 'your-service-connection'
      scriptType: 'bash'
      scriptLocation: 'inlineScript'
      inlineScript: |
        # Get token
        TOKEN=$(az account get-access-token \
          --resource $(ENTRA_ID_CLIENT_ID) \
          --query accessToken -o tsv)
        
        # Exchange for registry token
        REGISTRY_TOKEN=$(curl -s -X POST \
          https://your-registry.com/v0/auth/entra-id \
          -H "Content-Type: application/json" \
          -d "{\"access_token\": \"$TOKEN\"}" \
          | jq -r '.registry_token')
        
        # Publish (can be ANY server name)
        curl -X POST https://your-registry.com/v0/publish \
          -H "Authorization: Bearer $REGISTRY_TOKEN" \
          -d @server.json
    env:
      ENTRA_ID_CLIENT_ID: $(APP_ID)
```

## Token Permissions

With `NAMESPACE_PATTERN="*"`, the JWT token contains:

```json
{
  "auth_method": "entra-id",
  "auth_method_subject": "user:user@yourcompany.com",
  "permissions": [
    {
      "action": "publish",
      "resource": "*"
    },
    {
      "action": "edit",
      "resource": "*"
    }
  ]
}
```

This allows publishing and editing ANY server in the registry.

## Additional Controls (Optional)

If you want additional guardrails while still using wildcard:

### 1. Manual Approval Workflow

Add approval step in your DevOps pipeline:

```yaml
stages:
  - stage: Review
    jobs:
      - job: waitForValidation
        displayName: 'Wait for Manual Approval'
        pool: server
        steps:
          - task: ManualValidation@0
            inputs:
              notifyUsers: 'approvers@yourcompany.com'
              instructions: 'Review the MCP server before publishing'
  
  - stage: Publish
    dependsOn: Review
    jobs:
      - job: PublishServer
        steps:
          # ... publish steps
```

### 2. Audit Logging

Monitor all publishes:

```bash
# The registry logs will show who published what
# auth_method_subject: "user:developer@yourcompany.com"
# published: "io.github.domdomegg/airtable-mcp-server"
```

### 3. Version Pinning

Lock down specific versions in your internal registry to prevent unwanted updates.

## Summary

**Configuration:**
```bash
ENTRA_ID_ENABLED=true
ENTRA_ID_TENANT_ID=<your-tenant>
ENTRA_ID_CLIENT_ID=<your-app>
ENTRA_ID_NAMESPACE_PATTERN=*        # ← Wildcard for any server
ENTRA_ID_ALLOW_EDIT=true
```

**Result:**
- ✅ Colleagues authenticate via Entra ID
- ✅ Can publish ANY MCP server (public or internal)
- ✅ No mcp-publisher CLI needed
- ✅ No DNS validation needed
- ✅ Simple, trusted internal registry

This is the recommended setup for internal company registries where you're curating MCP servers for your organization! 🎉
