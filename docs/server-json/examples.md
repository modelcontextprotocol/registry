# Server JSON Examples

_These examples show the PublishRequest format used by the `/v0/publish` API endpoint. Each example includes the `server` specification and optional `x-publisher` extensions for build metadata._

## Basic Server with NPM Package

```json
{
  "server": {
    "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
    "name": "io.modelcontextprotocol/brave-search",
    "description": "MCP server for Brave Search API integration",
    "status": "active",
    "repository": {
      "url": "https://github.com/modelcontextprotocol/servers",
      "source": "github"
    },
    "version_detail": {
      "version": "1.0.2"
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
  },
  "x-publisher": {
    "tool": "npm-publisher",
    "version": "1.0.1",
    "build_info": {
      "timestamp": "2023-12-01T10:30:00Z"
    }
  }
}
```

## Constant (fixed) arguments needed to start the MCP server

Suppose your MCP server application requires a `mcp start` CLI arguments to start in MCP server mode. Express these as positional arguments like this:

```json
{
  "server": {
    "name": "com.github.joelverhagen/knapcode-samplemcpserver",
    "description": "Sample NuGet MCP server for a random number and random weather",
    "version_detail": {
      "version": "0.4.0-beta"
    },
    "packages": [
      {
        "registry_name": "nuget",
        "name": "Knapcode.SampleMcpServer",
        "version": "0.4.0-beta",
        "package_arguments": [
          {
            "type": "positional",
            "value": "mcp"
          },
          {
            "type": "positional",
            "value": "start"
          }
        ]
      }
    ]
  },
  "x-publisher": {
    "tool": "nuget-publisher",
    "version": "2.1.0",
    "build_info": {
      "timestamp": "2023-11-15T14:22:00Z",
      "pipeline_id": "nuget-build-456"
    }
  }
}
```

This will essentially instruct the MCP client to execute `dnx Knapcode.SampleMcpServer@0.4.0-beta -- mcp start` instead of the default `dnx Knapcode.SampleMcpServer@0.4.0-beta` (when no `package_arguments` are provided).

## Filesystem Server with Multiple Packages

```json
{
  "server": {
    "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
    "name": "com.github.modelcontextprotocol/filesystem",
    "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
    "status": "active",
    "repository": {
      "url": "https://github.com/modelcontextprotocol/servers",
      "source": "github",
      "id": "b94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"
    },
    "version_detail": {
      "version": "1.0.2"
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
  },
  "x-publisher": {
    "tool": "ci-publisher",
    "version": "3.2.1",
    "build_info": {
      "commit": "a1b2c3d4e5f6789",
      "timestamp": "2023-12-01T10:30:00Z",
      "pipeline_id": "filesystem-build-789",
      "environment": "production"
    }
  }
}
```

## Remote Server Example

```json
{
  "server": {
    "name": "io.modelcontextprotocol.anonymous/mcp-fs",
    "description": "Cloud-hosted MCP filesystem server",
    "repository": {
      "url": "https://github.com/example/remote-fs",
      "source": "github",
      "id": "xyz789ab-cdef-0123-4567-890ghijklmno"
    },
    "version_detail": {
      "version": "2.0.0"
    },
    "remotes": [
      {
        "transport_type": "sse",
        "url": "http://mcp-fs.anonymous.modelcontextprotocol.io/sse"
      }
    ]
  },
  "x-publisher": {
    "tool": "cloud-deployer",
    "version": "2.4.0",
    "build_info": {
      "commit": "f7e8d9c2b1a0",
      "timestamp": "2023-12-05T08:45:00Z",
      "deployment_id": "remote-fs-deploy-456",
      "region": "us-west-2"
    }
  }
}
```

## Python Package Example

```json
{
  "server": {
    "name": "com.github.example/weather-mcp",
    "description": "Python MCP server for weather data access",
    "repository": {
      "url": "https://github.com/example/weather-mcp",
      "source": "github",
      "id": "def456gh-ijkl-7890-mnop-qrstuvwxyz12"
    },
    "version_detail": {
      "version": "0.5.0"
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
  },
  "x-publisher": {
    "tool": "poetry-publisher",
    "version": "1.8.3",
    "build_info": {
      "python_version": "3.11.5",
      "timestamp": "2023-11-28T16:20:00Z",
      "build_id": "pypi-weather-123",
      "dependencies_hash": "sha256:a9b8c7d6e5f4"
    }
  }
}
```

## NuGet (.NET) Package Example

The `dnx` tool ships with the .NET 10 SDK, starting with Preview 6.

```json
{
  "server": {
    "name": "com.github.joelverhagen/knapcode-samplemcpserver",
    "description": "Sample NuGet MCP server for a random number and random weather",
    "repository": {
      "url": "https://github.com/joelverhagen/Knapcode.SampleMcpServer",
      "source": "github",
      "id": "example-nuget-id-0000-1111-222222222222"
    },
    "version_detail": {
      "version": "0.5.0"
    },
    "packages": [
      {
        "registry_name": "nuget",
        "name": "Knapcode.SampleMcpServer",
        "version": "0.5.0",
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
  },
  "x-publisher": {
    "tool": "dotnet-publisher",
    "version": "8.0.100",
    "build_info": {
      "dotnet_version": "8.0.0",
      "timestamp": "2023-12-10T12:15:00Z",
      "configuration": "Release",
      "target_framework": "net8.0",
      "build_number": "20231210.1"
    }
  }
}
```

## Complex Docker Server with Multiple Arguments

```json
{
  "server": {
    "name": "com.github.example/database-manager",
    "description": "MCP server for database operations with support for multiple database types",
    "repository": {
      "url": "https://github.com/example/database-manager-mcp",
      "source": "github",
      "id": "ghi789jk-lmno-1234-pqrs-tuvwxyz56789"
    },
    "version_detail": {
      "version": "3.1.0"
    },
    "packages": [
      {
        "registry_name": "docker",
        "name": "example/database-manager-mcp",
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
  },
  "x-publisher": {
    "tool": "docker-buildx",
    "version": "0.12.1",
    "build_info": {
      "docker_version": "24.0.7",
      "timestamp": "2023-12-08T14:30:00Z",
      "platform": "linux/amd64,linux/arm64",
      "registry": "docker.io",
      "image_digest": "sha256:1a2b3c4d5e6f7890"
    }
  }
}
```

## Server with Remote and Package Options

```json
{
  "server": {
    "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
    "name": "io.modelcontextprotocol.anonymous/hybrid-mcp",
    "description": "MCP server available as both local package and remote service",
    "repository": {
      "url": "https://github.com/example/hybrid-mcp",
      "source": "github",
      "id": "klm012no-pqrs-3456-tuvw-xyz789abcdef"
    },
    "version_detail": {
      "version": "1.5.0"
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
        "url": "https://mcp.anonymous.modelcontextprotocol.io/sse",
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
        "url": "https://mcp.anonymous.modelcontextprotocol.io/http"
      }
    ]
  },
  "x-publisher": {
    "tool": "hybrid-deployer",
    "version": "1.7.2",
    "build_info": {
      "timestamp": "2023-12-03T11:00:00Z",
      "deployment_strategy": "blue-green",
      "npm_version": "10.2.4",
      "node_version": "20.10.0",
      "service_endpoints": {
        "sse": "deployed",
        "streamable": "deployed"
      }
    }
  }
}
```

## Deprecated Server Example

```json
{
  "server": {
    "name": "com.github.example/old-weather",
    "description": "Legacy weather server - DEPRECATED: Use weather-v2 instead for new projects",
    "status": "deprecated",
    "repository": {
      "url": "https://github.com/example/old-weather",
      "source": "github",
      "id": "legacy-abc123-def456-789012-345678-901234567890"
    },
    "version_detail": {
      "version": "0.9.5"
    },
    "packages": [
      {
        "registry_name": "npm",
        "name": "@legacy/old-weather-server",
        "version": "0.9.5",
        "environment_variables": [
          {
            "name": "WEATHER_API_KEY",
            "description": "Weather API key",
            "is_required": true,
            "is_secret": true
          }
        ]
      }
    ]
  },
  "x-publisher": {
    "tool": "legacy-publisher",
    "version": "0.8.1",
    "build_info": {
      "timestamp": "2023-06-15T09:30:00Z",
      "deprecation_notice": "This publisher is deprecated. Use npm-publisher v2.0+ for new projects.",
      "maintenance_mode": true,
      "final_version": true
    }
  }
}
```