# Consuming Registry Data via REST API

Integration patterns and best practices for building applications that consume MCP registry data.

## Key details

**Base URL**: `https://registry.modelcontextprotocol.io`  

**Authentication**: Not required for read-only access

- **`GET /v0/servers`** - List all servers with pagination
- **`GET /v0/servers/{server_id}`** - Get latest version of server by server ID
- **`GET /v0/servers/{server_id}?version=X.X.X`** - Get specific version of server
- **`GET /v0/servers/{server_id}/versions`** - List all versions of a server

See the [interactive API documentation](https://registry.modelcontextprotocol.io/docs) for complete request/response schemas.

**Disclaimer**: The official registry provides no uptime or data durability guarantees. You should design your applications to handle service downtime via caching.

## Building a subregistry  
**Create enhanced registries** - ETL official registry data and add your own metadata like ratings, security scans, or compatibility info.

For now we recommend scraping the `GET /v0/servers` endpoint on some regular basis. In the future we might provide a filter for updatedAt ([#291](https://github.com/modelcontextprotocol/registry/issues/291)) to get only recently changed servers.

Servers are generally immutable, except for the `status` field which can be updated to `deleted` (among other states). For these packages, we recommend you also update the status field to `deleted` or remove the package from your registry quickly. This is because this status generally indicates it has violated our permissive [moderation guidelines](../administration/moderation-guidelines.md), suggesting it is illegal, malware or spam.

### Filtering & Enhancement

The official registry has a [permissive moderation policy](../administration/moderation-guidelines.md), so you may want to implement your own filtering on top of registry data.

You can also add custom metadata to servers using the `_meta` field. For example, user ratings, download counts, or security scan results. If you do this, we recommend you put this under a key that is namespaced to your organization, for example:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-16/server.schema.json",
  "name": "io.github.yourname/weather-server",
  "description": "MCP server for weather data access",
  "status": "active",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "weather-mcp-server",
      "version": "1.0.0"
    }
  ],
  "_meta": {
    "com.example.subregistry/custom": {
    "user_rating": 4.5,
      "download_count": 12345,
      "security_scan": {
        "last_scanned": "2024-06-01T12:00:00Z",
        "vulnerabilities_found": 0
      }
    }
  }
}
```

### Providing an API

We recommend that your subregistry provides an API meeting the registry API specification, so it's easy for clients to switch between registries. See the [registry API documentation](../../reference/api/) for details.

## MCP Client Integration
**Convert registry data to client configuration** - Fetch servers and transform package information into your MCP client's config format.

We highly recommend using a subregistry rather than fetching data from the official registry directly. You might want to make this configurable so that users of your client can choose their preferred registry, for example we expect that some enterprise users may have their own registry.

Your client should gracefully handle registries that meet the minimum spec, i.e. avoid hard dependencies on `_meta` fields.

### Filtering

You likely should filter out servers that are not `active` in the `status` field.

### Running servers

You can use the `packages` or `remotes` field to determine how to run a server. More details of these fields are in the [server.json documentation](../../reference/server-json/generic-server-json.md).
