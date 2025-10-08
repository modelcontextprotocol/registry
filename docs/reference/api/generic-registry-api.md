# Generic Registry API Specification

A standardized RESTful HTTP API for MCP registries to provide consistent endpoints for discovering and retrieving MCP servers.

Also see:
- For guidance consuming the API, see the [consuming guide](../../guides/consuming/use-rest-api.md).

## Browse the Complete API Specification

**📋 View the full API specification interactively**: Open [openapi.yaml](./openapi.yaml) in an OpenAPI viewer like [Stoplight Elements](https://elements-demo.stoplight.io/?spec=https://raw.githubusercontent.com/modelcontextprotocol/registry/refs/heads/main/docs/reference/api/openapi.yaml).

The official registry has some more endpoints and restrictions on top of this. See the [official registry API spec](./official-registry-api.md) for details.

## Quick Reference

### Core Endpoints
- **`GET /v0/servers`** - List all servers with pagination
- **`GET /v0/servers/{serverName}/versions`** - List all versions of a server
- **`GET /v0/servers/{serverName}/versions/{version}`** - Get specific version of server. Use the special version `latest` to get the latest version.
- **`POST /v0/publish`** - Publish new server (optional, registry-specific authentication)

Server names and version strings should be URL-encoded in paths.

### Authentication
- **Read operations**: No authentication required
- **Write operations**: Registry-specific authentication (if supported)

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
curl https://registry.example.com/v0/servers?limit=10
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
