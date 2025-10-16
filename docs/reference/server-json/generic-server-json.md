# server.json Format Specification

A `server.json` file is a standardized way to describe MCP servers for registry publishing, client discovery, and package management.

Also see:
- For step-by-step instructions on creating and using server.json files, see the [publishing guide](../../guides/publishing/publish-server.md).
- For understanding the validation requirements when publishing to the official registry, see [official registry requirements](./official-registry-requirements.md).

## Browse the Complete Schema

**📋 View the full specification interactively**: Open [server.schema.json](./server.schema.json) in a schema viewer like [json-schema.app](https://json-schema.app/view/%23?url=https%3A%2F%2Fstatic.modelcontextprotocol.io%2Fschemas%2F2025-09-29%2Fserver.schema.json).

The schema contains all field definitions, validation rules, examples, and detailed descriptions.

The official registry has some more restrictions on top of this. See the [official registry requirements](./official-registry-requirements.md) for details.

## Examples

<!-- As a heads up, these are used as part of tests/integration/main.go -->

### Basic Server with NPM Package

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.modelcontextprotocol.anonymous/brave-search",
  "description": "MCP server for Brave Search API integration",
  "title": "Brave Search",
  "websiteUrl": "https://anonymous.modelcontextprotocol.io/examples",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github"
  },
  "version": "1.0.2",
  "packages": [
    {
      "registryType": "npm",
      "registryBaseUrl": "https://registry.npmjs.org",
      "identifier": "@modelcontextprotocol/server-brave-search",
      "version": "1.0.2",
      "transport": {
        "type": "stdio"
      },
      "environmentVariables": [
        {
          "name": "BRAVE_API_KEY",
          "description": "Brave Search API Key",
          "isRequired": true,
          "isSecret": true
        }
      ]
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "tool": "npm-publisher",
      "version": "1.0.1",
      "build_info": {
        "timestamp": "2023-12-01T10:30:00Z"
      }
    }
  }
}
```

### Server in a Monorepo with Subfolder

For MCP servers located within a subdirectory of a larger repository (monorepo structure), use the `subfolder` field to specify the relative path:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.modelcontextprotocol/everything",
  "description": "MCP server that exercises all the features of the MCP protocol",
  "title": "Everything",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github",
    "subfolder": "src/everything"
  },
  "version": "0.6.2",
  "packages": [
    {
      "registryType": "npm",
      "registryBaseUrl": "https://registry.npmjs.org",
      "identifier": "@modelcontextprotocol/everything",
      "version": "0.6.2",
      "transport": {
        "type": "stdio"
      }
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "tool": "npm-publisher",
      "version": "1.0.1",
      "build_info": {
        "timestamp": "2023-12-01T10:30:00Z"
      }
    }
  }
}
```

### Constant (fixed) arguments needed to start the MCP server

Suppose your MCP server application requires a `mcp start` CLI arguments to start in MCP server mode. Express these as positional arguments like this:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.joelverhagen/knapcode-samplemcpserver",
  "description": "Sample NuGet MCP server for a random number and random weather",
  "version": "0.4.0-beta",
  "packages": [
    {
      "registryType": "nuget",
      "registryBaseUrl": "https://api.nuget.org",
      "identifier": "Knapcode.SampleMcpServer",
      "version": "0.4.0-beta",
      "transport": {
        "type": "stdio"
      },
      "packageArguments": [
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
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "tool": "nuget-publisher",
      "version": "2.1.0",
      "build_info": {
        "timestamp": "2023-11-15T14:22:00Z",
        "pipeline_id": "nuget-build-456"
      }
    }
  }
}
```

This will essentially instruct the MCP client to execute `dnx Knapcode.SampleMcpServer@0.4.0-beta -- mcp start` instead of the default `dnx Knapcode.SampleMcpServer@0.4.0-beta` (when no `packageArguments` are provided).

### Filesystem Server with Multiple Packages

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.modelcontextprotocol/filesystem",
  "description": "Node.js server implementing Model Context Protocol (MCP) for filesystem operations.",
  "title": "Filesystem",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/servers",
    "source": "github",
    "id": "b94b5f7e-c7c6-d760-2c78-a5e9b8a5b8c9"
  },
  "version": "1.0.2",
  "packages": [
    {
      "registryType": "npm",
      "registryBaseUrl": "https://registry.npmjs.org",
      "identifier": "@modelcontextprotocol/server-filesystem",
      "version": "1.0.2",
      "transport": {
        "type": "stdio"
      },
      "packageArguments": [
        {
          "type": "positional",
          "valueHint": "target_dir",
          "description": "Path to access",
          "default": "/Users/username/Desktop",
          "isRequired": true,
          "isRepeated": true
        }
      ],
      "environmentVariables": [
        {
          "name": "LOG_LEVEL",
          "description": "Logging level (debug, info, warn, error)",
          "default": "info"
        }
      ]
    },
    {
      "registryType": "oci",
      "identifier": "docker.io/mcp/filesystem:1.0.2",
      "transport": {
        "type": "stdio"
      },
      "runtimeArguments": [
        {
          "type": "named",
          "description": "Mount a volume into the container",
          "name": "--mount",
          "value": "type=bind,src={source_path},dst={target_path}",
          "isRequired": true,
          "isRepeated": true,
          "variables": {
            "source_path": {
              "description": "Source path on host",
              "format": "filepath",
              "isRequired": true
            },
            "target_path": {
              "description": "Path to mount in the container. It should be rooted in `/project` directory.",
              "isRequired": true,
              "default": "/project"
            }
          }
        }
      ],
      "packageArguments": [
        {
          "type": "positional",
          "valueHint": "target_dir",
          "value": "/project"
        }
      ],
      "environmentVariables": [
        {
          "name": "LOG_LEVEL",
          "description": "Logging level (debug, info, warn, error)",
          "default": "info"
        }
      ]
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
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
}
```

### Remote Server Example

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.modelcontextprotocol.anonymous/mcp-fs",
  "description": "Cloud-hosted MCP filesystem server",
  "repository": {
    "url": "https://github.com/example/remote-fs",
    "source": "github",
    "id": "xyz789ab-cdef-0123-4567-890ghijklmno"
  },
  "version": "2.0.0",
  "remotes": [
    {
      "type": "streamable-http",
      "url": "http://mcp-fs.anonymous.modelcontextprotocol.io/http"
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
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
}
```

### Python Package Example

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.example/weather-mcp",
  "description": "Python MCP server for weather data access",
  "title": "Weather",
  "repository": {
    "url": "https://github.com/example/weather-mcp",
    "source": "github",
    "id": "def456gh-ijkl-7890-mnop-qrstuvwxyz12"
  },
  "version": "0.5.0",
  "packages": [
    {
      "registryType": "pypi",
      "registryBaseUrl": "https://pypi.org",
      "identifier": "weather-mcp-server",
      "version": "0.5.0",
      "runtimeHint": "uvx",
      "transport": {
        "type": "stdio"
      },
      "environmentVariables": [
        {
          "name": "WEATHER_API_KEY",
          "description": "API key for weather service",
          "isRequired": true,
          "isSecret": true
        },
        {
          "name": "WEATHER_UNITS",
          "description": "Temperature units (celsius, fahrenheit)",
          "default": "celsius"
        }
      ]
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
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
}
```

### NuGet (.NET) Package Example

The `dnx` tool ships with the .NET 10 SDK, starting with Preview 6.

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.joelverhagen/knapcode-samplemcpserver",
  "description": "Sample NuGet MCP server for a random number and random weather",
  "repository": {
    "url": "https://github.com/joelverhagen/Knapcode.SampleMcpServer",
    "source": "github",
    "id": "example-nuget-id-0000-1111-222222222222"
  },
  "version": "0.5.0",
  "packages": [
    {
      "registryType": "nuget",
      "registryBaseUrl": "https://api.nuget.org",
      "identifier": "Knapcode.SampleMcpServer",
      "version": "0.5.0",
      "runtimeHint": "dnx",
      "transport": {
        "type": "stdio"
      },
      "environmentVariables": [
        {
          "name": "WEATHER_CHOICES",
          "description": "Comma separated list of weather descriptions to randomly select.",
          "isRequired": true,
          "isSecret": false
        }
      ]
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
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
}
```

### Complex Docker Server with Multiple Arguments

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.example/database-manager",
  "description": "MCP server for database operations with support for multiple database types",
  "repository": {
    "url": "https://github.com/example/database-manager-mcp",
    "source": "github",
    "id": "ghi789jk-lmno-1234-pqrs-tuvwxyz56789"
  },
  "version": "3.1.0",
  "packages": [
    {
      "registryType": "oci",
      "identifier": "docker.io/example/database-manager-mcp:3.1.0",
      "transport": {
        "type": "stdio"
      },
      "runtimeArguments": [
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
          "isRepeated": true,
          "variables": {
            "db_type": {
              "description": "Type of database",
              "choices": [
                "postgres",
                "mysql",
                "mongodb",
                "redis"
              ],
              "isRequired": true
            }
          }
        }
      ],
      "packageArguments": [
        {
          "type": "named",
          "name": "--host",
          "description": "Database host",
          "default": "localhost",
          "isRequired": true
        },
        {
          "type": "named",
          "name": "--port",
          "description": "Database port",
          "format": "number"
        },
        {
          "type": "positional",
          "valueHint": "database_name",
          "description": "Name of the database to connect to",
          "isRequired": true
        }
      ],
      "environmentVariables": [
        {
          "name": "DB_USERNAME",
          "description": "Database username",
          "isRequired": true
        },
        {
          "name": "DB_PASSWORD",
          "description": "Database password",
          "isRequired": true,
          "isSecret": true
        },
        {
          "name": "SSL_MODE",
          "description": "SSL connection mode",
          "default": "prefer",
          "choices": [
            "disable",
            "prefer",
            "require"
          ]
        }
      ]
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
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
}
```

### Server with Remote and Package Options

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.modelcontextprotocol.anonymous/hybrid-mcp",
  "description": "MCP server available as both local package and remote service",
  "title": "Hybrid",
  "repository": {
    "url": "https://github.com/example/hybrid-mcp",
    "source": "github",
    "id": "klm012no-pqrs-3456-tuvw-xyz789abcdef"
  },
  "version": "1.5.0",
  "packages": [
    {
      "registryType": "npm",
      "registryBaseUrl": "https://registry.npmjs.org",
      "identifier": "@example/hybrid-mcp-server",
      "version": "1.5.0",
      "runtimeHint": "npx",
      "transport": {
        "type": "stdio"
      },
      "packageArguments": [
        {
          "type": "named",
          "name": "--mode",
          "description": "Operation mode",
          "default": "local",
          "choices": [
            "local",
            "cached",
            "proxy"
          ]
        }
      ]
    }
  ],
  "remotes": [
    {
      "type": "streamable-http",
      "url": "https://mcp.anonymous.modelcontextprotocol.io/http",
      "headers": [
        {
          "name": "X-API-Key",
          "description": "API key for authentication",
          "isRequired": true,
          "isSecret": true
        },
        {
          "name": "X-Region",
          "description": "Service region",
          "default": "us-east-1",
          "choices": [
            "us-east-1",
            "eu-west-1",
            "ap-southeast-1"
          ]
        }
      ]
    },
    {
      "type": "sse",
      "url": "https://mcp.anonymous.modelcontextprotocol.io/sse"
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
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
}
```

### MCP Bundle (MCPB) Package Example

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.modelcontextprotocol/text-editor",
  "description": "MCP Bundle server for advanced text editing capabilities",
  "title": "Text Editor",
  "repository": {
    "url": "https://github.com/modelcontextprotocol/text-editor-mcpb",
    "source": "github"
  },
  "version": "1.0.2",
  "packages": [
    {
      "registryType": "mcpb",
      "identifier": "https://github.com/modelcontextprotocol/text-editor-mcpb/releases/download/v1.0.2/text-editor.mcpb",
      "fileSha256": "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
      "transport": {
        "type": "stdio"
      }
    }
  ],
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "tool": "mcpb-publisher",
      "version": "1.0.0",
      "build_info": {
        "timestamp": "2023-12-02T09:15:00Z",
        "bundle_format": "mcpb-v1"
      }
    }
  }
}
```

This example shows an MCPB (MCP Bundle) package that:
- Is hosted on GitHub Releases (an allowlisted provider)
- Includes a SHA-256 hash for integrity verification
- Can be downloaded and executed directly by MCP clients that support MCPB

### Embedded MCP inside a CLI tool

Some CLI tools bundle an MCP server, without a standalone MCP package or a public repository. In these cases, reuse the existing `packages` shape by pointing at the host CLI package and supplying the `packageArguments` and `runtimeHint` if needed to start the MCP server.

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.snyk/cli-mcp",
  "description": "MCP server provided by the Snyk CLI",
  "title": "Snyk",
  "version": "1.1298.0",
  "packages": [
    {
      "registryType": "npm",
      "registryBaseUrl": "https://registry.npmjs.org",
      "identifier": "snyk",
      "version": "1.1298.0",
      "transport": {
        "type": "stdio"
      },
      "packageArguments": [
        { "type": "positional", "value": "mcp" },
        {
          "type": "named",
          "name": "-t",
          "description": "Transport type for MCP server",
          "default": "stdio",
          "choices": ["stdio", "sse"]
        }
      ]
    }
  ]
}
```

### Server with Custom Installation Path

For MCP servers that follow a custom installation path or are embedded in applications without standalone packages, use the `websiteUrl` field to direct users to setup documentation:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.modelcontextprotocol.anonymous/embedded-mcp",
  "description": "MCP server embedded in a Desktop app",
  "websiteUrl": "https://anonymous.modelcontextprotocol.io/embedded-mcp-guide",
  "version": "0.1.0"
}
```

