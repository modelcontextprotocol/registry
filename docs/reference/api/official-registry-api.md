# Official MCP Registry API

This document describes the API for the official MCP Registry hosted by Anthropic at `registry.modelcontextprotocol.io`.

## Base URLs

- **Production**: `https://registry.modelcontextprotocol.io`
- **Staging**: `https://staging.registry.modelcontextprotocol.io`

## Interactive Documentation

- **[Live API Docs](https://registry.modelcontextprotocol.io/docs)** - Swagger UI with try-it-now functionality
- **[OpenAPI Spec](./openapi.yaml)** - Complete machine-readable specification

## Implementation

The official registry implements the [Generic Registry API](./generic-registry-api.md) with the following specific configurations and extensions:

### Authentication

Publishing requires namespace-based authentication:

- **GitHub OAuth** - For `io.github.*` namespaces
- **GitHub OIDC** - For publishing from GitHub Actions  
- **DNS verification** - For domain-based namespaces (`com.example.*`)
- **HTTP verification** - For domain-based namespaces (`com.example.*`)

See [Publisher Commands](../cli/commands.md) for authentication setup.

### Rate Limits

- **Read operations**: 1000 requests/hour per IP
- **Write operations**: 100 requests/hour per authenticated user
- **ETL/mirroring**: Contact maintainers for higher limits

### Caching

- Server lists cached for 1 hour
- Individual server data cached for 24 hours (immutable once published)
- Supports `If-None-Match` headers for efficient polling

### Registry-Specific Features

#### Enhanced Search
```http
GET /servers?q=weather&tags=api,forecast
```

Additional query parameters:
- `q` - Text search across names and descriptions
- `tags` - Filter by keywords (comma-separated)
- `status` - Filter by server status (`active`, `deprecated`, `experimental`)

#### Namespace Statistics  
```http
GET /namespaces/io.github.username
```

Returns publishing statistics for a namespace (requires authentication for private data).

#### Version History
All server versions are preserved and accessible via the standard versioning endpoints.

### Package Validation

The official registry enforces strict [package validation requirements](../server-json/official-registry-requirements.md):

- NPM packages must include `mcpName` field in `package.json`
- PyPI packages must include `mcp-name:` in README
- NuGet packages must include `mcp-name:` in README  
- Docker images must include `io.modelcontextprotocol.server.name` label
- MCPB URLs must contain "mcp" substring

### Error Responses

Extended error format with registry-specific codes:

```json
{
  "error": {
    "message": "Package validation failed",
    "code": "PACKAGE_VALIDATION_ERROR",
    "details": [
      "NPM package 'my-package' missing required mcpName field",
      "mcpName must match server name exactly"
    ]
  }
}
```

Common error codes:
- `NAMESPACE_FORBIDDEN` - Insufficient permissions for namespace
- `PACKAGE_VALIDATION_ERROR` - Package ownership verification failed
- `VERSION_EXISTS` - Cannot overwrite existing version
- `RATE_LIMIT_EXCEEDED` - Request rate limit exceeded

### Monitoring and Status

- **Status page**: `https://status.modelcontextprotocol.io`
- **Health check**: `GET /health` (returns `200 OK` when operational)

### Data Retention

- Published servers are preserved indefinitely
- Deprecated servers remain accessible for backwards compatibility
- Account deletion removes ownership but preserves published content

### Support

For API-specific support:
- **Issues**: [GitHub Issues](https://github.com/modelcontextprotocol/registry/issues)
- **Discussions**: [GitHub Discussions](https://github.com/modelcontextprotocol/registry/discussions)
- **Community**: [Discord](https://modelcontextprotocol.io/community/communication)

### Terms of Service

Use of the official registry is subject to the [Terms of Service](https://modelcontextprotocol.io/terms).

## Migration from Legacy APIs

If migrating from registry API versions prior to 2025-07-09:

1. Update endpoints to use `/servers` instead of `/packages`
2. Replace `package_name` with `name` in request/response bodies
3. Use new authentication methods for publishing
4. Validate server.json against updated schema

For migration assistance, see [GitHub Discussions](https://github.com/modelcontextprotocol/registry/discussions).