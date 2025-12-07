# Migrating from mcp-publisher to Direct API with Entra ID

This guide shows how to migrate from using `mcp-publisher` to direct API calls with Azure Entra ID authentication.

## Why Migrate?

**Benefits of Direct API Approach:**
- ✅ No CLI tool installation required
- ✅ Native integration with Azure pipelines
- ✅ Use existing Azure service connections
- ✅ Leverage managed identities (no secrets)
- ✅ Simpler CI/CD configuration
- ✅ Better error handling and debugging

## Prerequisites

- Azure subscription with App Registration
- Azure CLI installed (`az`)
- Existing `server.json` file

## Migration Steps

### Step 1: Create Azure App Registration

Replace `mcp-publisher login` authentication with Azure App Registration:

```bash
# Login to Azure
az login

# Create app registration
az ad app create --display-name "MCP Registry Authentication"

# Get IDs
APP_ID=$(az ad app list --display-name "MCP Registry Authentication" --query "[0].appId" -o tsv)
TENANT_ID=$(az account show --query tenantId -o tsv)

echo "Save these for later:"
echo "APP_ID=$APP_ID"
echo "TENANT_ID=$TENANT_ID"
```

### Step 2: Update Your Publishing Script

**Before (with mcp-publisher):**
```bash
#!/bin/bash
# Old approach using mcp-publisher

# Authenticate
mcp-publisher login github-oidc

# Publish
mcp-publisher publish
```

**After (direct API with Entra ID):**
```bash
#!/bin/bash
# New approach using direct API

# Get Azure token
TOKEN=$(az account get-access-token \
  --resource "$APP_ID" \
  --query accessToken -o tsv)

# Exchange for registry token
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
```

### Step 3: Update CI/CD Pipeline

#### GitHub Actions

**Before:**
```yaml
- name: Install mcp-publisher
  run: |
    curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_linux_amd64.tar.gz" | tar xz

- name: Authenticate
  run: ./mcp-publisher login github-oidc
  env:
    GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}

- name: Publish
  run: ./mcp-publisher publish
```

**After:**
```yaml
- name: Azure Login
  uses: azure/login@v1
  with:
    creds: ${{ secrets.AZURE_CREDENTIALS }}

- name: Publish to Registry
  run: |
    TOKEN=$(az account get-access-token --resource ${{ vars.APP_ID }} --query accessToken -o tsv)
    REGISTRY_TOKEN=$(curl -s -X POST https://registry.modelcontextprotocol.io/v0/auth/entra-id \
      -H "Content-Type: application/json" -d "{\"access_token\": \"$TOKEN\"}" | jq -r '.registry_token')
    curl -X POST https://registry.modelcontextprotocol.io/v0/publish \
      -H "Authorization: Bearer $REGISTRY_TOKEN" -d @server.json
```

#### Azure DevOps

**Before:**
```yaml
- script: |
    curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_linux_amd64.tar.gz" | tar xz
    ./mcp-publisher login dns --domain=$(DOMAIN) --private-key=$(PRIVATE_KEY)
    ./mcp-publisher publish
  displayName: 'Publish with mcp-publisher'
```

**After:**
```yaml
- task: AzureCLI@2
  displayName: 'Publish to MCP Registry'
  inputs:
    azureSubscription: 'your-service-connection'
    scriptType: 'bash'
    scriptLocation: 'inlineScript'
    inlineScript: |
      TOKEN=$(az account get-access-token --resource $(APP_ID) --query accessToken -o tsv)
      REGISTRY_TOKEN=$(curl -s -X POST https://registry.modelcontextprotocol.io/v0/auth/entra-id \
        -H "Content-Type: application/json" -d "{\"access_token\": \"$TOKEN\"}" | jq -r '.registry_token')
      curl -X POST https://registry.modelcontextprotocol.io/v0/publish \
        -H "Authorization: Bearer $REGISTRY_TOKEN" -d @server.json
  env:
    APP_ID: $(ENTRA_ID_CLIENT_ID)
```

### Step 4: Update Secrets/Variables

**Remove:**
- `PRIVATE_KEY` (if using DNS/HTTP auth)
- `GITHUB_TOKEN` (if using GitHub auth)
- Downloaded mcp-publisher binaries

**Add:**
- `APP_ID` - Your Azure App Registration client ID
- Azure service connection (for Azure DevOps)
- Azure credentials secret (for GitHub Actions)

## Comparison: Old vs New

| Aspect | mcp-publisher | Direct API + Entra ID |
|--------|--------------|----------------------|
| Installation | Required | Not needed |
| Binary Size | ~10-20 MB | 0 MB |
| Auth Method | Multiple options | Azure AD (native) |
| CI/CD Setup | Install + Login + Publish | Single API call |
| Secrets | Private keys / tokens | Azure service connection |
| Error Handling | CLI output parsing | HTTP status codes |
| Debugging | Limited | Full HTTP debugging |
| Managed Identity | Not supported | ✅ Supported |

## Advanced: Managed Identity

If running on Azure infrastructure (VM, Container, Function), you can eliminate secrets entirely:

```bash
#!/bin/bash
# No credentials needed - uses managed identity

# Get token from instance metadata
TOKEN=$(curl -s 'http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01&resource='$APP_ID \
  -H Metadata:true \
  | jq -r '.access_token')

# Exchange and publish
REGISTRY_TOKEN=$(curl -s -X POST https://registry.modelcontextprotocol.io/v0/auth/entra-id \
  -H "Content-Type: application/json" -d "{\"access_token\": \"$TOKEN\"}" | jq -r '.registry_token')

curl -X POST https://registry.modelcontextprotocol.io/v0/publish \
  -H "Authorization: Bearer $REGISTRY_TOKEN" -d @server.json
```

## Namespace Mapping

Your namespace may change depending on configuration:

| Old Method | Old Namespace | New Namespace (Example) |
|-----------|--------------|------------------------|
| `github-oidc` | `io.github.username/*` | `com.yourcompany.*` |
| `dns` | `com.yourcompany/*` | `com.yourcompany.*` |
| `http` | `com.yourcompany/*` | `com.yourcompany.*` |

Configure namespace pattern in registry:
```bash
# Match your company domain
ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*
```

## Rollback Plan

If you need to rollback to mcp-publisher:

1. Keep mcp-publisher installed temporarily
2. Test new approach in a separate pipeline
3. Run both in parallel during transition
4. Remove mcp-publisher after successful migration

```yaml
# Run both during transition
- name: Publish (Old Method - Backup)
  if: failure()
  run: mcp-publisher publish

- name: Publish (New Method)
  run: |
    # Direct API call
```

## Troubleshooting Migration

### Issue: "Invalid token"
**Solution**: Verify APP_ID matches your App Registration:
```bash
az ad app list --display-name "MCP Registry Authentication" --query "[0].appId"
```

### Issue: "Wrong namespace"
**Solution**: Check your email domain:
```bash
az ad signed-in-user show --query userPrincipalName
```
Then verify namespace pattern allows your domain.

### Issue: "Cannot get Azure token"
**Solution**: Ensure you're logged in:
```bash
az login
az account show
```

### Issue: "Pipeline fails with 401"
**Solution**: Verify service connection has correct permissions:
- Check service principal exists
- Verify it has access to the App Registration
- Ensure it's not expired

## Cost Comparison

| Item | mcp-publisher | Direct API |
|------|--------------|-----------|
| Binary downloads | ~50 KB/build | 0 KB |
| CI/CD time | +10-15s (download) | 0s overhead |
| Secrets to manage | 1-2 | 0 (with managed identity) |
| Maintenance | CLI updates needed | API versioned |

## Migration Checklist

- [ ] Create Azure App Registration
- [ ] Note APP_ID and TENANT_ID
- [ ] Configure registry with Entra ID settings
- [ ] Update publishing script/pipeline
- [ ] Test authentication locally
- [ ] Test in CI/CD environment
- [ ] Update documentation
- [ ] Remove mcp-publisher installation steps
- [ ] Remove old secrets/keys
- [ ] Monitor first production publish
- [ ] Document rollback procedure

## Support

If you encounter issues during migration:

1. Check [Entra ID documentation](../docs/reference/authentication/entra-id.md)
2. Review [troubleshooting guide](../docs/reference/authentication/entra-id.md#troubleshooting)
3. Test locally before updating CI/CD
4. Verify registry configuration
5. Check Azure AD token claims

## Conclusion

The migration to direct API + Entra ID provides:
- ✅ Simpler architecture
- ✅ Better Azure integration
- ✅ Fewer dependencies
- ✅ Enhanced security (managed identities)
- ✅ Easier debugging

Most migrations can be completed in under 30 minutes!
