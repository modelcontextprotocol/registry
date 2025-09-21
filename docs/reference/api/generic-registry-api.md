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
- **`GET /v0/servers/{server_id}`** - Get latest version of server by server ID
- **`GET /v0/servers/{server_id}?version=X.X.X`** - Get specific version of server
- **`GET /v0/servers/{server_id}/versions`** - List all versions of a server
- **`POST /v0/publish`** - Publish new server (optional, registry-specific authentication)

### Authentication
- **Read operations**: No authentication required
- **Write operations**: Registry-specific authentication (if supported)

### Content Type
All requests and responses use `application/json`

### Basic Example: List Servers

```bash
curl https://registry.example.com/v0/servers?limit=10
```

```json
{
  "servers": [
    {
      "name": "io.modelcontextprotocol/filesystem",
      "description": "Filesystem operations server", 
      "status": "active",
      "version": "1.0.2"
    }
  ],
  "metadata": {
    "count": 10,
    "next_cursor": "eyJ..."
  }
}
```

For complete endpoint documentation, view the OpenAPI specification in a schema viewer.
