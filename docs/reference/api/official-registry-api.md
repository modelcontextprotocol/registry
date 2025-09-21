# Official MCP Registry API

This document describes the API for the official MCP Registry hosted at `registry.modelcontextprotocol.io`.

This API is based on the [generic registry API](./generic-registry-api.md) with additional endpoints and authentication. For practical examples of consuming the API, see the [API usage guide](../../guides/consuming/use-rest-api.md). For publishing servers using the API, see the [publishing guide](../../guides/publishing/publish-server.md).

## Base URLs

- **Production**: `https://registry.modelcontextprotocol.io`
- **Staging**: `https://staging.registry.modelcontextprotocol.io`

## Interactive Documentation

- **[Live API Docs](https://registry.modelcontextprotocol.io/docs)** - Stoplight elements with try-it-now functionality
- **[OpenAPI Spec](https://registry.modelcontextprotocol.io/openapi.yaml)** - Complete machine-readable specification

## Extensions

The official registry implements the [Generic Registry API](./generic-registry-api.md) with the following specific configurations and extensions:

### Authentication

Publishing requires namespace-based authentication:

- **GitHub OAuth** - For `io.github.*` namespaces
- **GitHub OIDC** - For publishing from GitHub Actions  
- **DNS verification** - For domain-based namespaces (`com.example.*`)
- **HTTP verification** - For domain-based namespaces (`com.example.*`)

See [Publisher Commands](../cli/commands.md) for authentication setup.

### Package Validation

The official registry enforces additional [package validation requirements](../server-json/official-registry-requirements.md) when publishing.

### Server List Filtering

The official registry extends the `GET /v0/servers` endpoint with additional query parameters for improved discovery and synchronization:

- `updated_since` - Filter servers updated after RFC3339 timestamp (e.g., `2025-08-07T13:15:04.280Z`)
- `search` - Case-insensitive substring search on server names (e.g., `filesystem`)  
    - This is intentionally simple. For more advanced searching and filtering, use a subregistry.
- `version` - Filter by version (currently supports `latest` for latest versions only)

These extensions enable efficient incremental synchronization for downstream registries and improved server discovery. Parameters can be combined and work with standard cursor-based pagination.

Example: `GET /v0/servers?search=filesystem&updated_since=2025-08-01T00:00:00Z&version=latest`

### Additional endpoints

#### Auth endpoints
- POST `/v0/auth/dns` - Exchange signed DNS challenge for auth token
- POST `/v0/auth/http` - Exchange signed HTTP challenge for auth token
- POST `/v0/auth/github-at` - Exchange GitHub access token for auth token
- POST `/v0/auth/github-oidc` - Exchange GitHub OIDC token for auth token
- POST `/v0/auth/oidc` - Exchange Google OIDC token for auth token (for admins)

#### Admin endpoints
- GET `/metrics` - Prometheus metrics endpoint
- GET `/v0/health` - Basic health check endpoint
- PUT `/v0/servers/{server_id}?version=X.X.X` - Edit specific server version (version parameter required)
