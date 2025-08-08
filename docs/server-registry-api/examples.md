# Examples

## Health Check

### Request

```http
GET /v0/health
```

### Response

```json
{
  "status": "ok",
  "github_client_id": "your_github_client_id"
}
```

## Ping Endpoint

### Request

```http
GET /v0/ping
```

### Response

```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

## List Registry Server Entries

### Request

```http
GET /v0/servers
```

Lists MCP registry server entries with pagination support.

Query parameters:
- `limit`: Maximum number of entries to return (default: 30, max: 100)
- `cursor`: Pagination cursor for retrieving next set of results

### Response

```json
{
  "servers": [
    {
      "id": "123e4567-e89b-12d3-a456-426614174000",
      "name": "io.github.modelcontextprotocol/filesystem",
      "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
      "status": "active",
      "repository": {
        "url": "https://github.com/modelcontextprotocol/servers",
        "source": "github",
        "id": "b94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"
      },
      "version_detail": {
        "version": "1.0.2",
        "release_date": "2023-06-15T10:30:00Z",
        "is_latest": true
      },
      "created_at": "2025-05-17T17:34:22.912Z",
      "updated_at": "2025-05-17T17:34:22.912Z"
    }
  ],
  "metadata": {
    "next_cursor": "123e4567-e89b-12d3-a456-426614174000",
    "count": 30
  }
}
```

## Get Server Details

### Request

```http
GET /v0/servers/{id}
```

Retrieves detailed information about a specific MCP server entry.

Path parameters:
- `id`: Unique identifier of the server entry (UUID format)

### Response

```json
{
  "id": "01129bff-3d65-4e3d-8e82-6f2f269f818c",
  "name": "io.github.gongrzhe/redis-mcp-server",
  "description": "A Redis MCP server implementation for interacting with Redis databases. This server enables LLMs to interact with Redis key-value stores through a set of standardized tools.",
  "status": "active",
  "repository": {
    "url": "https://github.com/GongRzhe/REDIS-MCP-Server",
    "source": "github",
    "id": "907849235"
  },
  "version_detail": {
    "version": "0.0.1-seed",
    "release_date": "2025-05-16T19:13:21Z",
    "is_latest": true
  },
  "created_at": "2025-05-17T17:34:22.912Z",
  "updated_at": "2025-05-17T17:34:22.912Z",
  "packages": [
    {
      "registry_name": "docker",
      "name": "@gongrzhe/server-redis-mcp",
      "version": "1.0.0",
      "package_arguments": [
        {
          "description": "Docker image to run",
          "is_required": true,
          "format": "string",
          "value": "mcp/redis",
          "default": "mcp/redis",
          "type": "positional",
          "value_hint": "mcp/redis"
        },
        {
          "description": "Redis server connection string",
          "is_required": true,
          "format": "string",
          "value": "redis://host.docker.internal:6379",
          "default": "redis://host.docker.internal:6379",
          "type": "positional",
          "value_hint": "host.docker.internal:6379"
        }
      ]
    }
  ]
}
```

## Publish a Server Entry

### Request

```http
POST /v0/publish
```

Publishes a new MCP server entry to the registry. Authentication is required via Bearer token in the Authorization header.

Headers:
- `Authorization`: Bearer token for authentication (e.g., `Bearer your_token_here`)
- `Content-Type`: application/json

Request body:
```json
{
  "server_detail": {
    "description": "<your description here>",
    "name": "io.github.<owner>/<server-name>",
    "packages": [
      {
        "registry_name": "npm",
        "name": "@<owner>/<server-name>",
        "version": "0.2.23",
        "package_arguments": [
          {
            "description": "Specify services and permissions.",
            "is_required": true,
            "format": "string",
            "value": "-s",
            "default": "-s",
            "type": "positional",
            "value_hint": "-s"
          }
        ],
        "environment_variables": [
          {
            "description": "API Key to access the server",
            "name": "API_KEY"
          }
        ]
      },
      {
        "registry_name": "docker",
        "name": "@<owner>/<server-name>-cli",
        "version": "0.123.223",
        "runtime_hint": "docker",
        "runtime_arguments": [
          {
            "description": "Specify services and permissions.",
            "is_required": true,
            "format": "string",
            "value": "--mount",
            "default": "--mount",
            "type": "positional",
            "value_hint": "--mount"
          }
        ],
        "environment_variables": [
          {
            "description": "API Key to access the server",
            "name": "API_KEY"
          }
        ]
      }
    ],
    "repository": {
      "url": "https://github.com/<owner>/<server-name>",
      "source": "github"
    },
    "version_detail": {
      "version": "0.0.1-<publisher_version>"
    }
  },
  "repo_ref": "optional-repository-reference"
}
```

### Response

```json
{
  "message": "Server publication successful",
  "id": "1234567890abcdef12345678"
}
```

### Server Configuration Examples

#### Local Server with npx

API Response:
```json
{
  "id": "brave-search-12345",
  "name": "io.modelcontextprotocol/brave-search",
  "description": "MCP server for Brave Search API integration",
  "status": "active",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github",
    "id": "abc123de-f456-7890-ghij-klmnopqrstuv"
  },
  "version_detail": {
    "version": "1.0.2",
    "release_date": "2023-06-15T10:30:00Z",
    "is_latest": true
  },
  "packages": [
    {
      "registry_name": "npm",
      "name": "@modelcontextprotocol/server-brave-search",
      "version": "1.0.2",
      "environment_variables": [
        {
          "name": "BRAVE_API_KEY",
          "description": "Brave Search API Key",
          "is_required": true,
          "is_secret": true
        }
      ]
    }
  ]
}
```

claude_desktop_config.json:
```json
{
  "brave-search": {
    "command": "npx",
    "args": [
      "-y",
      "@modelcontextprotocol/server-brave-search"
    ],
    "env": {
      "BRAVE_API_KEY": "YOUR_API_KEY_HERE"
    }
  }
}
```

#### Local Server with Docker

API Response:
```json
{
  "id": "filesystem-67890",
  "name": "io.modelcontextprotocol/filesystem",
  "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations",
  "status": "active",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github",
    "id": "d94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"
  },
  "version_detail": {
    "version": "1.0.2",
    "release_date": "2023-06-15T10:30:00Z",
    "is_latest": true
  },
  "packages": [
    {
      "registry_name": "docker",
      "name": "mcp/filesystem",
      "version": "1.0.2",
      "runtime_arguments": [
        {
          "type": "named",
          "description": "Mount a volume into the container",
          "name": "--mount",
          "value": "type=bind,src={source_path},dst={target_path}",
          "is_required": true,
          "is_repeated": true,
          "variables": {
            "source_path": {
              "description": "Source path on host",
              "format": "filepath",
              "is_required": true
            },
            "target_path": {
              "description": "Path to mount in the container. It should be rooted in `/project` directory.",
              "is_required": true,
              "default": "/project",
            }
          }
        }
      ],
      "package_arguments": [
        {
          "type": "positional",
          "value_hint": "target_dir",
          "value": "/project",
        }
      ]
    }
  ]
}
```

claude_desktop_config.json:
```json
{
  "filesystem": {
    "server": "@modelcontextprotocol/servers/src/filesystem@1.0.2",
    "package": "docker",
    "settings": {
      "--mount": [
        { "source_path": "/Users/username/Desktop", "target_path": "/project/desktop" },
        { "source_path": "/path/to/other/allowed/dir", "target_path": "/project/other/allowed/dir,ro" },
      ]
    }
  }
}
```

#### Remote Server

API Response:
```json
{
  "id": "remote-fs-54321",
  "name": "Remote Brave Search Server",
  "description": "Cloud-hosted MCP Brave Search server",
  "status": "active",
  "repository": {
    "url": "https://github.com/example/remote-fs",
    "source": "github",
    "id": "xyz789ab-cdef-0123-4567-890ghijklmno"
  },
  "version_detail": {
    "version": "1.0.2",
    "release_date": "2023-06-15T10:30:00Z",
    "is_latest": true
  },
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://mcp-fs.example.com/sse"
    }
  ]
}
```
