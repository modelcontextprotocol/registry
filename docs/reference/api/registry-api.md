# Registry API Reference

Complete REST API specification for the MCP Registry.

## Base URL

- **Production**: `https://registry.modelcontextprotocol.io`
- **Staging**: `https://staging.registry.modelcontextprotocol.io`

## Interactive Documentation

- **[Live API Docs](https://registry.modelcontextprotocol.io/docs)** - Swagger UI with try-it-now functionality
- **[OpenAPI Spec](./openapi.yaml)** - Download the complete OpenAPI specification

## Authentication

Most endpoints are read-only and require no authentication. Publishing requires authentication via:

- **GitHub OAuth** - For `io.github.*` namespaces
- **DNS verification** - For domain-based namespaces (`com.example.*`)
- **HTTP verification** - For domain-based namespaces (`com.example.*`)

See [Publisher Commands](../cli/commands.md) for authentication setup.

## Core Endpoints

### List Servers
```http
GET /servers
```
Returns all published servers with basic metadata.

**Query Parameters:**
- `limit` - Maximum number of results (default: 100, max: 1000)
- `offset` - Pagination offset

### Get Server Details
```http
GET /servers/{name}
```
Returns complete metadata for a specific server.

**Path Parameters:**
- `name` - Server name in reverse-DNS format (e.g., `io.github.user/server`)

### Get Server Version
```http  
GET /servers/{name}/versions/{version}
```
Returns metadata for a specific server version.

### Publish Server
```http
POST /servers
```
Publishes a new server or version. Requires authentication and valid `server.json` payload.

## Response Format

All responses follow this structure:

```json
{
  "data": {}, 
  "meta": {}
}
```

- `data` - The requested resource(s)
- `meta` - Pagination and metadata information

## Error Handling

HTTP status codes follow REST conventions:

- `200` - Success
- `400` - Invalid request (validation errors, malformed JSON)
- `401` - Authentication required  
- `403` - Insufficient permissions (namespace mismatch)
- `404` - Resource not found
- `409` - Conflict (version already exists, immutable data)
- `422` - Package validation failed
- `429` - Rate limit exceeded
- `500` - Server error

Error responses include detailed messages:

```json
{
  "error": {
    "message": "Validation failed",
    "details": ["Field 'name' is required", "Invalid namespace format"]
  }
}
```

## Rate Limits

- **Read operations**: 1000 requests/hour per IP
- **Write operations**: 100 requests/hour per authenticated user
- **ETL/mirroring**: Contact maintainers for higher limits

## Caching

- Server lists cached for 1 hour
- Individual server data cached for 24 hours (immutable once published)
- Use `If-None-Match` headers for efficient polling