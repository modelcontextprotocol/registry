# Authentication Methods

The MCP Registry supports multiple authentication methods to accommodate different use cases and deployment scenarios.

## Available Methods

### GitHub-based Authentication
- **[GitHub Access Token](./github-at.md)** - Authenticate using GitHub personal access tokens
- **[GitHub OIDC](./github-oidc.md)** - Authenticate from GitHub Actions using OIDC tokens

### Domain-based Authentication
- **[DNS Verification](./dns.md)** - Prove domain ownership via DNS TXT records
- **[HTTP Verification](./http.md)** - Prove domain ownership via HTTPS endpoints

### Cloud Provider Authentication
- **[Azure Entra ID](./entra-id.md)** - Authenticate using Azure AD tokens (service principals, users, managed identities)
- **[Generic OIDC](./oidc.md)** - Authenticate using any OIDC provider

### Development
- **[Anonymous (None)](./none.md)** - No authentication (local development only)

## Quick Comparison

| Method | Best For | Namespace Access | Setup Complexity |
|--------|----------|------------------|------------------|
| GitHub Access Token | Individual developers | `io.github.username/*` | Low |
| GitHub OIDC | CI/CD workflows | `io.github.org/*` | Low |
| DNS Verification | Custom domains | `com.yourdomain/*` | Medium |
| HTTP Verification | Custom domains | `com.yourdomain/*` | Medium |
| **Azure Entra ID** | **Azure users/pipelines** | **Configurable** | **Medium** |
| Generic OIDC | Other OIDC providers | Configurable | High |
| Anonymous | Local testing | All (if enabled) | None |

## Azure Entra ID Authentication

The newest addition to the registry's authentication methods, Azure Entra ID (formerly Azure Active Directory) provides enterprise-grade authentication for:

- **Azure DevOps Pipelines** with service connections
- **GitHub Actions** with Azure login
- **Managed Identities** in Azure VMs and containers
- **Service Principals** for automation
- **User Accounts** with Azure AD

### Quick Start

1. **[Quick Start Guide](./entra-id-quickstart.md)** - Get started in 5 minutes
2. **[Full Documentation](./entra-id.md)** - Complete setup and configuration guide

### Key Features

✅ **Service Principal Support** - Perfect for CI/CD automation  
✅ **Managed Identity Support** - Secure authentication without secrets  
✅ **User Authentication** - Interactive login via Azure CLI  
✅ **Flexible Namespace Patterns** - Control what can be published  
✅ **Multi-tenant Support** - Works across Azure AD tenants  

### Example: Publish from Azure Pipeline

```yaml
- task: AzureCLI@2
  inputs:
    azureSubscription: 'your-service-connection'
    scriptType: 'bash'
    scriptLocation: 'inlineScript'
    inlineScript: |
      TOKEN=$(az account get-access-token --resource <APP_ID> --query accessToken -o tsv)
      REGISTRY_TOKEN=$(curl -s -X POST \
        https://registry.modelcontextprotocol.io/v0/auth/entra-id \
        -H "Content-Type: application/json" \
        -d "{\"access_token\": \"$TOKEN\"}" \
        | jq -r '.registry_token')
      curl -X POST https://registry.modelcontextprotocol.io/v0/publish \
        -H "Authorization: Bearer $REGISTRY_TOKEN" \
        -d @server.json
```

## Choosing the Right Method

### For Azure Users
👉 **Use Azure Entra ID** if you:
- Deploy on Azure infrastructure
- Use Azure DevOps or GitHub Actions with Azure
- Need managed identity support
- Want enterprise SSO integration

### For GitHub Users
👉 **Use GitHub OIDC** if you:
- Publish from GitHub Actions
- Want seamless GitHub integration
- Prefer `io.github.org/*` namespaces

### For Custom Domains
👉 **Use DNS/HTTP Verification** if you:
- Own a custom domain
- Want `com.yourdomain/*` namespaces
- Need cryptographic proof of ownership

### For Development
👉 **Use Anonymous** if you:
- Are testing locally
- Don't need authentication
- Registry has `ENABLE_ANONYMOUS_AUTH=true`

## Configuration

Each authentication method can be enabled/disabled via environment variables:

```bash
# GitHub
MCP_REGISTRY_GITHUB_CLIENT_ID=<client-id>
MCP_REGISTRY_GITHUB_CLIENT_SECRET=<secret>

# Azure Entra ID
MCP_REGISTRY_ENTRA_ID_ENABLED=true
MCP_REGISTRY_ENTRA_ID_TENANT_ID=<tenant-id>
MCP_REGISTRY_ENTRA_ID_CLIENT_ID=<client-id>

# Generic OIDC
MCP_REGISTRY_OIDC_ENABLED=true
MCP_REGISTRY_OIDC_ISSUER=<issuer-url>
MCP_REGISTRY_OIDC_CLIENT_ID=<client-id>

# Anonymous (testing only)
MCP_REGISTRY_ENABLE_ANONYMOUS_AUTH=true
```

## Security Best Practices

1. **Use Short-lived Tokens**: All tokens should expire quickly
2. **Rotate Secrets**: Regularly rotate client secrets and private keys
3. **Principle of Least Privilege**: Configure namespace patterns to limit access
4. **Enable MFA**: Use multi-factor authentication for user accounts
5. **Monitor Activity**: Log and audit authentication attempts
6. **Secure Storage**: Never commit secrets to version control

## API Endpoints

All authentication endpoints follow the pattern:

```
POST /v0/auth/{method}
```

Available endpoints:
- `/v0/auth/github-at` - GitHub access token
- `/v0/auth/github-oidc` - GitHub OIDC
- `/v0/auth/entra-id` - **Azure Entra ID** ⭐ NEW
- `/v0/auth/dns` - DNS verification
- `/v0/auth/http` - HTTP verification
- `/v0/auth/oidc/{provider}` - Generic OIDC
- `/v0/auth/none` - Anonymous

Each returns a registry JWT token:

```json
{
  "registry_token": "eyJhbGciOiJFZERTQSIsInR5cCI6IkpXVCJ9..."
}
```

## Further Reading

- [API Reference](../api/)
- [Publisher CLI](../cli/)
- [Server JSON Schema](../server-json/)
