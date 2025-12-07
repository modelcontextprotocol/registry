# Dual Namespace Support Examples

This document explains how the dual namespace support works for accommodating both reverse-DNS and simple server names.

## The Challenge

MCP server names must follow the pattern: `namespace/server-name` with exactly one slash.

**Examples:**
- ✅ `io.github.user/weather` (reverse-DNS format)
- ✅ `microsoft/azure-devops-mcp` (simple format)
- ❌ `bla/bla/io.github.user/server` (too many slashes - invalid)

However, Azure Entra ID authentication uses reverse-DNS patterns like `com.microsoft.*`, which don't directly match simple names like `microsoft/azure-devops-mcp`.

## The Solution: Dual Namespace Support

The registry JWT token contains **TWO permission patterns**:

1. **Full reverse-DNS pattern** - for servers like `com.microsoft.azure-devops`
2. **Simple namespace pattern** - for servers like `microsoft/azure-devops-mcp`

## Configuration Examples

### Example 1: Microsoft Organization

**User:** `developer@microsoft.com`

**Registry Configuration:**
```bash
ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*
ENTRA_ID_SIMPLE_NAMESPACE={company}/*
```

**Generated Token Permissions:**
```json
{
  "permissions": [
    {
      "action": "publish",
      "resource": "com.microsoft.*"
    },
    {
      "action": "publish",
      "resource": "microsoft/*"
    }
  ]
}
```

**Allowed Server Names:**
- ✅ `com.microsoft.azure-devops` 
- ✅ `com.microsoft.teams-mcp`
- ✅ `microsoft/azure-devops-mcp`
- ✅ `microsoft/graph-api-server`
- ❌ `contoso/my-server` (wrong namespace)
- ❌ `com.contoso.server` (wrong namespace)

### Example 2: Custom Company (Contoso)

**User:** `admin@contoso.com`

**Registry Configuration:**
```bash
ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*
ENTRA_ID_SIMPLE_NAMESPACE={company}/*
```

**Generated Token Permissions:**
```json
{
  "permissions": [
    {
      "action": "publish",
      "resource": "com.contoso.*"
    },
    {
      "action": "publish",
      "resource": "contoso/*"
    }
  ]
}
```

**Allowed Server Names:**
- ✅ `com.contoso.inventory-mcp`
- ✅ `com.contoso.crm-connector`
- ✅ `contoso/inventory-mcp`
- ✅ `contoso/api-server`
- ❌ `microsoft/server` (wrong namespace)

### Example 3: Service Principal with App Name

**Service Principal:** 
- Display Name: `Azure DevOps MCP Publisher`
- Organization: `microsoft.com` tenant

**Registry Configuration:**
```bash
ENTRA_ID_NAMESPACE_PATTERN=com.microsoft.*
ENTRA_ID_SIMPLE_NAMESPACE={company}/{app_name}/*
```

**Generated Token Permissions:**
```json
{
  "permissions": [
    {
      "action": "publish",
      "resource": "com.microsoft.*"
    },
    {
      "action": "publish",
      "resource": "microsoft/azure-devops-mcp-publisher/*"
    }
  ]
}
```

**Allowed Server Names:**
- ✅ `com.microsoft.anything`
- ✅ `microsoft/azure-devops-mcp-publisher/work-items`
- ✅ `microsoft/azure-devops-mcp-publisher/pipelines`
- ❌ `microsoft/teams/connector` (doesn't match app_name pattern)

### Example 4: GitHub-style Namespaces

**User:** `developer@mycompany.com`

**Registry Configuration:**
```bash
ENTRA_ID_NAMESPACE_PATTERN=io.github.{company}.*
ENTRA_ID_SIMPLE_NAMESPACE=io.github.{company}/*
```

**Generated Token Permissions:**
```json
{
  "permissions": [
    {
      "action": "publish",
      "resource": "io.github.mycompany.*"
    },
    {
      "action": "publish",
      "resource": "io.github.mycompany/*"
    }
  ]
}
```

**Allowed Server Names:**
- ✅ `io.github.mycompany.server`
- ✅ `io.github.mycompany/weather-mcp`
- ✅ `io.github.mycompany/inventory`

### Example 5: Auto-extraction (No Simple Namespace Configured)

**User:** `user@fabrikam.com`

**Registry Configuration:**
```bash
ENTRA_ID_NAMESPACE_PATTERN=com.{reversed_domain}.*
# ENTRA_ID_SIMPLE_NAMESPACE not set - will auto-extract
```

**Auto-extraction Logic:**
- `com.fabrikam.*` → extracts → `fabrikam/*`

**Generated Token Permissions:**
```json
{
  "permissions": [
    {
      "action": "publish",
      "resource": "com.fabrikam.*"
    },
    {
      "action": "publish",
      "resource": "fabrikam/*"
    }
  ]
}
```

**Allowed Server Names:**
- ✅ `com.fabrikam.my-server`
- ✅ `fabrikam/my-server` (auto-extracted)

## Real-World Use Case: Azure DevOps MCP

**Scenario:** Microsoft wants to publish the official Azure DevOps MCP server.

**Setup:**
```bash
# Registry configuration
ENTRA_ID_ENABLED=true
ENTRA_ID_TENANT_ID=<microsoft-tenant-id>
ENTRA_ID_CLIENT_ID=<app-registration-id>
ENTRA_ID_NAMESPACE_PATTERN=com.microsoft.*
ENTRA_ID_SIMPLE_NAMESPACE=microsoft/*
```

**Authentication:**
```bash
# Get Azure token as Microsoft employee
TOKEN=$(az account get-access-token --resource <APP_ID> --query accessToken -o tsv)

# Exchange for registry token
REGISTRY_TOKEN=$(curl -s -X POST \
  https://registry.modelcontextprotocol.io/v0/auth/entra-id \
  -H "Content-Type: application/json" \
  -d "{\"access_token\": \"$TOKEN\"}" \
  | jq -r '.registry_token')
```

**server.json (Simple Format):**
```json
{
  "name": "microsoft/azure-devops-mcp",
  "description": "MCP server for Azure DevOps integration",
  "version": "1.0.0",
  "packages": [...]
}
```

**Publishing:**
```bash
curl -X POST https://registry.modelcontextprotocol.io/v0/publish \
  -H "Authorization: Bearer $REGISTRY_TOKEN" \
  -d @server.json
```

✅ **Success!** The server name `microsoft/azure-devops-mcp` matches the permission `microsoft/*`.

## Migration Path

If you currently have servers with reverse-DNS names and want to add simple names:

**Step 1: Enable dual namespace**
```bash
ENTRA_ID_SIMPLE_NAMESPACE={company}/*
```

**Step 2: Publish new versions with simple names**
```json
{
  "name": "contoso/my-server",  // New simple format
  "version": "2.0.0",
  ...
}
```

**Step 3: Keep old versions available**
```json
{
  "name": "com.contoso.my-server",  // Old reverse-DNS format
  "version": "1.9.0",
  ...
}
```

Both will be accessible, allowing gradual migration.

## Validation Logic

The permission check in the registry:

```go
// Checks if server name matches ANY permission pattern
HasPermission("microsoft/azure-devops", "publish", permissions)

// Checks against each permission:
// 1. "microsoft/azure-devops" vs "com.microsoft.*" → ❌ (no match)
// 2. "microsoft/azure-devops" vs "microsoft/*" → ✅ (matches!)

// Result: ALLOWED
```

## Summary

**Key Benefits:**
1. ✅ **Schema Compliance** - All server names have exactly one slash
2. ✅ **Flexible Naming** - Support both reverse-DNS and simple formats
3. ✅ **Backward Compatible** - Existing servers continue to work
4. ✅ **Enterprise Ready** - Works with Microsoft, AWS, Google namespaces
5. ✅ **Auto-extraction** - Intelligent defaults if not explicitly configured

**Configuration Required:**
- `ENTRA_ID_NAMESPACE_PATTERN` - Full reverse-DNS pattern (required)
- `ENTRA_ID_SIMPLE_NAMESPACE` - Simple format pattern (optional, auto-extracts if not set)

This approach allows the registry to support diverse naming conventions while maintaining schema compliance and security through namespace-based access control.
