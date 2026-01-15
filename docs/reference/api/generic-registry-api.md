# Generic Registry API Specification

A standardized RESTful HTTP API for MCP registries to provide consistent endpoints for discovering and retrieving MCP servers.

Also see:
- For authentication and authorization, see the [registry authorization specification](./registry-authorization.md).

## Browse the Complete API Specification

**📋 View the full API specification interactively**: Open [openapi.yaml](./openapi.yaml) in an OpenAPI viewer like [Stoplight Elements](https://elements-demo.stoplight.io/?spec=https://raw.githubusercontent.com/modelcontextprotocol/registry/refs/heads/main/docs/reference/api/openapi.yaml).

The official registry has some more endpoints and restrictions on top of this. See the [official registry API spec](./official-registry-api.md) for details.

## Quick Reference

### Core Endpoints
- **`GET /v0.1/servers`** - List all servers with pagination
- **`GET /v0.1/servers/{serverName}/versions`** - List all versions of a server
- **`GET /v0.1/servers/{serverName}/versions/{version}`** - Get specific version of server. Use the special version `latest` to get the latest version.
- **`POST /v0.1/publish`** - Publish new server (optional, registry-specific authentication)
- **`PUT /v0.1/servers/{serverName}/versions/{version}`** - Update specific server version (optional, not implemented by official registry)
- **`DELETE /v0.1/servers/{serverName}/versions/{version}`** - Delete specific server version (optional, not implemented by official registry)

Server names and version strings should be URL-encoded in paths.

### Authentication

No authentication required by default. Subregistries may optionally require authentication following the [registry authorization specification](./registry-authorization.md).

### Content Type
All requests and responses use `application/json`

### Pagination
List endpoints use cursor-based pagination for efficient, stable results.

#### Usage
1. **Initial request**: Omit the `cursor` parameter
2. **Subsequent requests**: Use the `nextCursor` value from the previous response
3. **End of results**: When `nextCursor` is null or empty, there are no more results

**Important**: Always treat cursors as opaque strings. Never manually construct or modify cursor values.

### Basic Example: List Servers

```bash
curl https://registry.example.com/v0.1/servers?limit=10
```

```json
{
  "servers": [
    {
      "server": {
        "name": "io.modelcontextprotocol/filesystem",
        "description": "Filesystem operations server",
        "version": "1.0.2"
      },
      "_meta": {
        "io.modelcontextprotocol.registry/official": {
          "status": "active",
          "publishedAt": "2025-01-01T10:30:00Z",
          "isLatest": true
        }
      }
    }
  ],
  "metadata": {
    "count": 10,
    "nextCursor": "com.example/my-server:1.0.0"
  }
}
```

For complete endpoint documentation, view the OpenAPI specification in a schema viewer.
