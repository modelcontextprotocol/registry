# Examples

## /v0/servers

### Request

```http
GET /v0/servers?limit=5000&offset=0
```

### Response

```json
{
  "servers": [
    {
      "id": "a5e8a7f0-d4e4-4a1d-b12f-2896a23fd4f1",
      "name": "io.modelcontextprotocol/filesystem",
      "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
      "repository": {
        "url": "https://github.com/modelcontextprotocol/servers",
        "source": "github",
        "id": "b94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"
      },
      "version_detail": {
        "version": "1.0.2",
        "release_date": "2023-06-15T10:30:00Z",
        "is_latest": true
      }
    }
  ],
  "next": "https://registry.modelcontextprotocol.io/servers?offset=50",
  "total_count": 1
}
```

## /v0/servers/:id

### Request

```http
GET /v0/servers/a5e8a7f0-d4e4-4a1d-b12f-2896a23fd4f1?version=0.0.3
```

### Response

```json
{
  "id": "a5e8a7f0-d4e4-4a1d-b12f-2896a23fd4f1",
  "name": "io.modelcontextprotocol/filesystem",
  "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
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
  "packages": [
    {
      "registry_name": "npm",
      "name": "@modelcontextprotocol/server-filesystem",
      "version": "1.0.2",
      "package_arguments": [
        {
          "type": "positional",
          "value_hint": "target_dir",
          "description": "Path to access",
          "default": "/Users/username/Desktop",
          "is_required": true,
          "is_repeated": true
        }
      ],
      "environment_variables": [
        {
          "name": "LOG_LEVEL",
          "description": "Logging level (debug, info, warn, error)",
          "default": "info"
        }
      ]
    },
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
      ],
      "environment_variables": [
        {
          "name": "LOG_LEVEL",
          "description": "Logging level (debug, info, warn, error)",
          "default": "info"
        }
      ]
    }
  ],
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://mcp-fs.example.com/sse"
    }
  ]
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
