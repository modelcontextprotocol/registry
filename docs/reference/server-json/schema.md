# server.json Schema Reference

Complete field specification for the `server.json` format used to describe MCP servers.

## Schema Files

- **[server.schema.json](./server.schema.json)** - Complete JSON Schema for all server.json use cases
- **[registry-schema.json](./registry-schema.json)** - Constrained schema specific to registry requirements

## Core Structure

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
  "name": "com.example/my-server",
  "description": "Brief description of what the server does",
  "status": "active",
  "repository": { ... },
  "version_detail": { ... },
  "packages": [ ... ]
}
```

## Required Fields

### `name` (string)
Server identifier in reverse-DNS format.
- **Format**: `domain.tld/server-name` or `domain.tld.subdomain/server-name`
- **Examples**: `io.github.user/weather`, `com.company.team/database`
- **Registry requirement**: Must match authenticated namespace (see [CLI Commands](../cli/commands.md#authentication-methods))

### `description` (string)  
Clear explanation of server functionality.
- **Length**: 10-500 characters
- **Should**: Focus on capabilities, not implementation details

### `status` (string)
Server lifecycle status.
- **Values**: `active`, `deprecated`, `experimental`
- **Default**: `active`

### `version_detail` (object)
Version information for this server.
- **`version`** (string, required) - Version string (max 255 chars)
- **Should**: Use semantic versioning (e.g., "1.0.2", "2.1.0-alpha")

### `packages` (array)
Runtime packages for this server. At least one required.

## Package Types

### NPM Package
```json
{
  "registry_type": "npm",
  "identifier": "package-name",
  "version": "1.0.0",
  "registry_base_url": "https://registry.npmjs.org"
}
```

### PyPI Package  
```json
{
  "registry_type": "pypi", 
  "identifier": "package-name",
  "version": "1.0.0",
  "registry_base_url": "https://pypi.org"
}
```

### NuGet Package
```json
{
  "registry_type": "nuget",
  "identifier": "Package.Name", 
  "version": "1.0.0",
  "registry_base_url": "https://api.nuget.org"
}
```

### Docker/OCI Image
```json
{
  "registry_type": "oci",
  "identifier": "username/image-name",
  "version": "1.0.0",
  "registry_base_url": "https://docker.io"
}
```

### MCPB Binary
```json
{
  "registry_type": "mcpb",
  "identifier": "https://github.com/user/repo/releases/download/v1.0.0/server.mcpb"
}
```

## Optional Fields

### `repository` (object)
Source code repository information.
```json
{
  "url": "https://github.com/user/repo",
  "source": "github",
  "id": "optional-internal-id"
}
```

### `license` (string)
SPDX license identifier (e.g., "MIT", "Apache-2.0").

### `contact` (array)
Contact information for maintainers.
```json
[
  {
    "name": "Maintainer Name",
    "email": "maintainer@example.com",
    "url": "https://example.com"
  }
]
```

### `keywords` (array)
Searchable tags describing server functionality.
```json
["weather", "api", "forecast"]
```

## Package Configuration

### Environment Variables
```json
{
  "environment_variables": [
    {
      "name": "API_KEY",
      "description": "Service API key",
      "is_required": true,
      "is_secret": true,
      "default_value": "optional-default"
    }
  ]
}
```

### Runtime Arguments
```json
{
  "args": ["--verbose", "--config=/path/to/config"]
}
```

### Working Directory
```json
{
  "cwd": "/app/server"
}
```

## Extensions

Use `x-` prefix for custom metadata:
```json
{
  "x-publisher": {
    "build_date": "2024-01-01",
    "internal_id": "abc123"
  }
}
```

## Validation

All server.json files must:
1. Be valid JSON
2. Pass JSON Schema validation  
3. Meet package verification requirements (see [Validation Reference](./validation.md))
4. Use authenticated namespace for registry publishing