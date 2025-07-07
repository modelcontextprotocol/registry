# Server JSON Examples

## Basic Server with NPM Package

```json
{
  "name": "io.modelcontextprotocol/brave-search",
  "description": "MCP server for Brave Search API integration",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github",
    "id": "abc123de-f456-7890-ghij-klmnopqrstuv"
  },
  "version_detail": {
    "version": "1.0.2",
    "release_date": "2023-06-15T10:30:00Z"
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

## Filesystem Server with Multiple Packages

```json
{
  "name": "io.modelcontextprotocol/filesystem",
  "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github",
    "id": "b94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"
  },
  "version_detail": {
    "version": "1.0.2",
    "release_date": "2023-06-15T10:30:00Z"
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
              "default": "/project"
            }
          }
        }
      ],
      "package_arguments": [
        {
          "type": "positional",
          "value_hint": "target_dir",
          "value": "/project"
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
  ]
}
```

## Remote Server Example

```json
{
  "name": "Remote Filesystem Server",
  "description": "Cloud-hosted MCP filesystem server",
  "repository": {
    "url": "https://github.com/example/remote-fs",
    "source": "github",
    "id": "xyz789ab-cdef-0123-4567-890ghijklmno"
  },
  "version_detail": {
    "version": "2.0.0",
    "release_date": "2024-01-20T14:30:00Z"
  },
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://mcp-fs.example.com/sse"
    }
  ]
}
```

## Python Package Example

```json
{
  "name": "weather-mcp-server",
  "description": "Python MCP server for weather data access",
  "repository": {
    "url": "https://github.com/example/weather-mcp",
    "source": "github",
    "id": "def456gh-ijkl-7890-mnop-qrstuvwxyz12"
  },
  "version_detail": {
    "version": "0.5.0",
    "release_date": "2024-02-10T09:15:00Z"
  },
  "packages": [
    {
      "registry_name": "pypi",
      "name": "weather-mcp-server",
      "version": "0.5.0",
      "runtime_hint": "uvx",
      "environment_variables": [
        {
          "name": "WEATHER_API_KEY",
          "description": "API key for weather service",
          "is_required": true,
          "is_secret": true
        },
        {
          "name": "WEATHER_UNITS",
          "description": "Temperature units (celsius, fahrenheit)",
          "default": "celsius"
        }
      ]
    }
  ]
}
```

## NuGet (.NET) Package Example

The `dnx` tool ships with the .NET 10 SDK, starting with Preview 6.

```json
{
  "name": "Knapcode.SampleMcpServer",
  "description": "Sample NuGet MCP server for a random number and random weather",
  "repository": {
    "url": "https://github.com/joelverhagen/Knapcode.SampleMcpServer",
    "source": "github"
  },
  "version_detail": {
    "version": "0.3.0",
    "release_date": "2025-07-02T18:54:28.00Z"
  },
  "packages": [
    {
      "registry_name": "nuget",
      "name": "Knapcode.SampleMcpServer",
      "version": "0.3.0-beta",
      "runtime_hint": "dnx",
      "environment_variables": [
        {
          "name": "WEATHER_CHOICES",
          "description": "Comma separated list of weather descriptions to randomly select.",
          "is_required": true,
          "is_secret": false
        }
      ]
    }
  ]
}
```

## Complex Docker Server with Multiple Arguments

```json
{
  "name": "mcp-database-manager",
  "description": "MCP server for database operations with support for multiple database types",
  "repository": {
    "url": "https://github.com/example/mcp-database",
    "source": "gitlab",
    "id": "ghi789jk-lmno-1234-pqrs-tuvwxyz56789"
  },
  "version_detail": {
    "version": "3.1.0",
    "release_date": "2024-03-05T16:45:00Z"
  },
  "packages": [
    {
      "registry_name": "docker",
      "name": "mcp/database-manager",
      "version": "3.1.0",
      "runtime_arguments": [
        {
          "type": "named",
          "name": "--network",
          "value": "host",
          "description": "Use host network mode"
        },
        {
          "type": "named",
          "name": "-e",
          "value": "DB_TYPE={db_type}",
          "description": "Database type to connect to",
          "is_repeated": true,
          "variables": {
            "db_type": {
              "description": "Type of database",
              "choices": ["postgres", "mysql", "mongodb", "redis"],
              "is_required": true
            }
          }
        }
      ],
      "package_arguments": [
        {
          "type": "named",
          "name": "--host",
          "description": "Database host",
          "default": "localhost",
          "is_required": true
        },
        {
          "type": "named",
          "name": "--port",
          "description": "Database port",
          "format": "number"
        },
        {
          "type": "positional",
          "value_hint": "database_name",
          "description": "Name of the database to connect to",
          "is_required": true
        }
      ],
      "environment_variables": [
        {
          "name": "DB_USERNAME",
          "description": "Database username",
          "is_required": true
        },
        {
          "name": "DB_PASSWORD",
          "description": "Database password",
          "is_required": true,
          "is_secret": true
        },
        {
          "name": "SSL_MODE",
          "description": "SSL connection mode",
          "default": "prefer",
          "choices": ["disable", "prefer", "require"]
        }
      ]
    }
  ]
}
```

## Server with Remote and Package Options

```json
{
  "name": "hybrid-mcp-server",
  "description": "MCP server available as both local package and remote service",
  "repository": {
    "url": "https://github.com/example/hybrid-mcp",
    "source": "github",
    "id": "klm012no-pqrs-3456-tuvw-xyz789abcdef"
  },
  "version_detail": {
    "version": "1.5.0",
    "release_date": "2024-04-01T12:00:00Z"
  },
  "packages": [
    {
      "registry_name": "npm",
      "name": "@example/hybrid-mcp-server",
      "version": "1.5.0",
      "runtime_hint": "npx",
      "package_arguments": [
        {
          "type": "named",
          "name": "--mode",
          "description": "Operation mode",
          "default": "local",
          "choices": ["local", "cached", "proxy"]
        }
      ]
    }
  ],
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://hybrid-mcp.example.com/sse",
      "headers": [
        {
          "name": "X-API-Key",
          "description": "API key for authentication",
          "is_required": true,
          "is_secret": true
        },
        {
          "name": "X-Region",
          "description": "Service region",
          "default": "us-east-1",
          "choices": ["us-east-1", "eu-west-1", "ap-southeast-1"]
        }
      ]
    },
    {
      "transport_type": "streamable",
      "url": "https://hybrid-mcp.example.com/stream"
    }
  ]
}
```