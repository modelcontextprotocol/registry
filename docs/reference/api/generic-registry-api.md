# Generic Registry API Specification

This document describes the standardized API format for MCP registries. Any registry implementation can use this specification to provide consistent endpoints and data formats.

## Overview

MCP registries provide a RESTful HTTP API for discovering and retrieving MCP servers. This specification defines the minimum required endpoints and data structures that any compliant registry must implement.

## Base Requirements

### Content Type
All requests and responses must use `application/json` content type.

### Authentication
- **Read operations** - No authentication required
- **Write operations** - Registry-specific authentication (OAuth, API keys, etc.)

## Core Endpoints

### List Servers
```http
GET /servers
```

Returns a paginated list of available servers.

**Query Parameters:**
- `limit` (optional) - Maximum results per page (default: registry-defined)
- `offset` (optional) - Results to skip for pagination (default: 0)

**Response Format:**
```json
{
  "data": [
    {
      "name": "com.example/server-name",
      "description": "Brief server description",
      "status": "active",
      "latest_version": "1.2.0",
      "created_at": "2024-01-15T10:30:00Z",
      "updated_at": "2024-01-20T14:45:00Z"
    }
  ],
  "meta": {
    "total": 42,
    "limit": 20,
    "offset": 0,
    "has_more": true
  }
}
```

### Get Server Details
```http
GET /servers/{name}
```

Returns complete metadata for a specific server including all versions.

**Path Parameters:**
- `name` - Server identifier in reverse-DNS format (URL-encoded)

**Response Format:**
```json
{
  "data": {
    "name": "com.example/server-name",
    "description": "Detailed server description",
    "status": "active",
    "repository": {
      "url": "https://github.com/user/repo",
      "source": "github"
    },
    "license": "MIT",
    "keywords": ["tag1", "tag2"],
    "contact": [
      {
        "name": "Maintainer Name",
        "email": "maintainer@example.com"
      }
    ],
    "created_at": "2024-01-15T10:30:00Z",
    "updated_at": "2024-01-20T14:45:00Z",
    "versions": [
      {
        "version": "1.2.0",
        "published_at": "2024-01-20T14:45:00Z",
        "packages": [...],
        "remotes": [...]
      }
    ]
  }
}
```

### Get Server Version
```http
GET /servers/{name}/versions/{version}
```

Returns metadata for a specific server version.

**Path Parameters:**
- `name` - Server identifier (URL-encoded)
- `version` - Version string (URL-encoded)

**Response Format:**
```json
{
  "data": {
    "name": "com.example/server-name",
    "version": "1.2.0",
    "description": "Server description",
    "packages": [
      {
        "registry_type": "npm",
        "identifier": "package-name",
        "version": "1.2.0",
        "registry_base_url": "https://registry.npmjs.org",
        "environment_variables": [...],
        "package_arguments": [...],
        "runtime_arguments": [...]
      }
    ],
    "remotes": [
      {
        "transport_type": "sse",
        "url": "https://api.example.com/mcp",
        "headers": [...]
      }
    ],
    "published_at": "2024-01-20T14:45:00Z"
  }
}
```

### Publish Server (Optional)
```http
POST /servers
```

Publishes a new server or server version. This endpoint is optional for read-only registries.

**Request Body:**
Complete `server.json` format as specified in the [server-json specification](../server-json/generic-server-json.md).

**Response Format:**
```json
{
  "data": {
    "name": "com.example/server-name",
    "version": "1.2.0",
    "published_at": "2024-01-20T14:45:00Z"
  }
}
```

## Data Types

### Server Summary
Minimal server information for list endpoints:
```json
{
  "name": "com.example/server",
  "description": "Brief description",
  "status": "active",
  "latest_version": "1.0.0",
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z"
}
```

### Server Detail
Complete server information:
```json
{
  "name": "com.example/server",
  "description": "Detailed description",
  "status": "active",
  "repository": {
    "url": "https://github.com/user/repo",
    "source": "github"
  },
  "license": "MIT",
  "keywords": ["keyword1", "keyword2"],
  "contact": [
    {
      "name": "Name",
      "email": "email@example.com",
      "url": "https://example.com"
    }
  ],
  "created_at": "2024-01-01T00:00:00Z",
  "updated_at": "2024-01-01T00:00:00Z",
  "versions": [...]
}
```

### Package Reference
Package installation information:
```json
{
  "registry_type": "npm|pypi|nuget|oci|mcpb",
  "identifier": "package-identifier",
  "version": "1.0.0",
  "registry_base_url": "https://registry.url",
  "runtime_hint": "optional-runtime",
  "environment_variables": [...],
  "package_arguments": [...],
  "runtime_arguments": [...]
}
```

### Remote Reference
Remote service connection information:
```json
{
  "transport_type": "sse|websocket",
  "url": "https://service.url",
  "headers": [
    {
      "name": "Header-Name",
      "description": "Header purpose",
      "is_required": true,
      "is_secret": false
    }
  ]
}
```

## HTTP Status Codes

### Success Codes
- `200 OK` - Request successful
- `201 Created` - Resource created (publish operations)

### Client Error Codes
- `400 Bad Request` - Invalid request format
- `401 Unauthorized` - Authentication required
- `403 Forbidden` - Insufficient permissions
- `404 Not Found` - Resource not found
- `409 Conflict` - Resource already exists (publishing duplicate version)
- `422 Unprocessable Entity` - Valid request format but business logic errors

### Server Error Codes
- `500 Internal Server Error` - Unexpected server error
- `502 Bad Gateway` - Upstream service error
- `503 Service Unavailable` - Registry temporarily unavailable

## Error Response Format

All error responses follow this structure:
```json
{
  "error": {
    "message": "Human-readable error description",
    "code": "MACHINE_READABLE_ERROR_CODE",
    "details": [
      "Additional error detail 1",
      "Additional error detail 2"
    ]
  }
}
```

## Implementation Notes

### URL Encoding
Server names containing special characters must be URL-encoded in path parameters:
- `com.example/my-server` → `com.example%2Fmy-server`

### Date Format
All timestamps use ISO 8601 format with UTC timezone:
- `2024-01-20T14:45:00Z`

### Pagination
Registries should implement consistent pagination:
- Use `limit` and `offset` parameters
- Include `meta` object with pagination information
- Provide `has_more` boolean for efficient client iteration

### Caching
Registries may implement HTTP caching:
- Use appropriate `Cache-Control` headers
- Support `If-None-Match` for ETags
- Consider immutability of published versions

## OpenAPI Schema

For implementation reference, see the complete OpenAPI specification:
- **[openapi.yaml](./openapi.yaml)** - Machine-readable API specification

This schema can be used to generate client libraries and validate implementations.