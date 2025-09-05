# server.json Format Guide

Detailed specification and examples for the server.json format used to describe MCP servers.

## What You'll Learn

By the end of this tutorial, you'll understand:
- What server.json files represent and why they exist
- The core structure and required fields
- How to configure packages and runtime requirements
- Best practices for writing clear, maintainable server.json files

## Prerequisites

- Basic JSON knowledge
- An MCP server you want to describe
- 10-15 minutes

## What is server.json?

A `server.json` file is a **standardized way to describe an MCP server**. It serves multiple purposes:

- **Registry publishing** - Describes your server for the MCP Registry
- **Client discovery** - Helps MCP clients understand how to run your server
- **Package management** - References where your server code is hosted
- **Configuration** - Specifies runtime arguments and environment variables

Think of it as a "package.json" for MCP servers - it's metadata about your server that can be used across the ecosystem.

## Basic Structure

Let's start with a minimal server.json:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
  "name": "io.github.yourname/weather-server",
  "description": "MCP server for weather data access",
  "version_detail": {
    "version": "1.0.0"
  },
  "packages": [
    {
      "registry_type": "npm",
      "identifier": "weather-mcp-server",
      "version": "1.0.0"
    }
  ]
}
```

### Required Fields

- **`name`** - Unique identifier in reverse-DNS format (`io.github.username/server-name`)
- **`description`** - Clear explanation of what your server does
- **`version_detail.version`** - Version of this server.json (usually matches package version)
- **`packages`** or **`remotes`** - Package references or remote service endpoints

## Package Types

Your server can be packaged in different ways:

### NPM Package
```json
{
  "registry_type": "npm",
  "identifier": "@yourname/weather-server",
  "version": "1.0.0"
}
```

### Python Package
```json
{
  "registry_type": "pypi", 
  "identifier": "weather-mcp-server",
  "version": "1.0.0",
  "runtime_hint": "uvx"
}
```

### Docker Image
```json
{
  "registry_type": "oci",
  "identifier": "yourname/weather-server",
  "version": "1.0.0"
}
```

### NuGet Package
```json
{
  "registry_type": "nuget",
  "identifier": "YourName.WeatherServer", 
  "version": "1.0.0",
  "runtime_hint": "dnx"
}
```

## Adding Runtime Configuration

Most servers need configuration - here's how to specify it:

### Environment Variables
```json
{
  "packages": [
    {
      "registry_type": "npm",
      "identifier": "weather-server",
      "version": "1.0.0",
      "environment_variables": [
        {
          "name": "WEATHER_API_KEY",
          "description": "API key for weather service",
          "is_required": true,
          "is_secret": true
        },
        {
          "name": "UNITS",
          "description": "Temperature units (celsius/fahrenheit)",
          "default": "celsius"
        }
      ]
    }
  ]
}
```

### Command Line Arguments
```json
{
  "packages": [
    {
      "registry_type": "npm",
      "identifier": "filesystem-server",
      "version": "1.0.0",
      "package_arguments": [
        {
          "type": "positional",
          "value_hint": "directory_path",
          "description": "Directory to serve",
          "is_required": true
        },
        {
          "type": "named",
          "name": "--log-level",
          "description": "Logging verbosity",
          "default": "info",
          "choices": ["debug", "info", "warn", "error"]
        }
      ]
    }
  ]
}
```

### Docker Runtime Arguments
For Docker/OCI packages, use `runtime_arguments` for container-specific arguments:
```json
{
  "packages": [
    {
      "registry_type": "oci",
      "identifier": "yourname/server",
      "version": "1.0.0",
      "runtime_arguments": [
        {
          "type": "named",
          "name": "--mount",
          "value": "type=bind,src={source_path},dst=/app/data",
          "description": "Mount data directory",
          "variables": {
            "source_path": {
              "description": "Host path to mount",
              "format": "filepath",
              "is_required": true
            }
          }
        }
      ]
    }
  ]
}
```

## Remote Servers

If your server runs as a hosted service rather than a local package:

```json
{
  "name": "com.yourcompany/weather-service",
  "description": "Cloud-hosted weather MCP server",
  "version_detail": {
    "version": "2.0.0"
  },
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://weather-mcp.yourcompany.com/sse",
      "headers": [
        {
          "name": "X-API-Key",
          "description": "Authentication key",
          "is_required": true,
          "is_secret": true
        }
      ]
    }
  ]
}
```

## Best Practices

### 1. Use Clear Names and Descriptions
```json
{
  "name": "io.github.yourname/weather-server",          // ✅ Clear, follows convention
  "description": "Provides weather data and forecasts"  // ✅ Specific and helpful
}
```

Instead of:
```json
{
  "name": "weather",                                     // ❌ Too generic
  "description": "A server"                              // ❌ Not helpful
}
```

### 2. Include Helpful Configuration Hints
```json
{
  "environment_variables": [
    {
      "name": "API_KEY",
      "description": "Get your key from https://api.weather.com/signup",  // ✅ Actionable help
      "is_required": true,
      "is_secret": true
    }
  ]
}
```

### 3. Provide Sensible Defaults
```json
{
  "package_arguments": [
    {
      "type": "named",
      "name": "--timeout",
      "description": "Request timeout in seconds",
      "default": "30"                                     // ✅ Good default
    }
  ]
}
```

### 4. Add Repository Information
```json
{
  "repository": {
    "url": "https://github.com/yourname/weather-server",
    "source": "github"
  }
}
```

## Progressive Example: Building Up Complexity

Let's build a complete server.json step by step:

**Step 1 - Minimal:**
```json
{
  "name": "io.github.alice/database-tools",
  "description": "MCP server for database operations",
  "version_detail": { "version": "1.0.0" },
  "packages": [
    {
      "registry_type": "pypi",
      "identifier": "database-tools-mcp",
      "version": "1.0.0"
    }
  ]
}
```

**Step 2 - Add Configuration:**
```json
{
  "name": "io.github.alice/database-tools",
  "description": "MCP server for database operations",
  "version_detail": { "version": "1.0.0" },
  "packages": [
    {
      "registry_type": "pypi",
      "identifier": "database-tools-mcp",
      "version": "1.0.0",
      "runtime_hint": "uvx",
      "environment_variables": [
        {
          "name": "DB_URL",
          "description": "Database connection URL",
          "is_required": true,
          "is_secret": true
        }
      ]
    }
  ]
}
```

**Step 3 - Add Metadata:**
```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
  "name": "io.github.alice/database-tools",
  "description": "MCP server for database operations with PostgreSQL and MySQL support",
  "status": "active",
  "repository": {
    "url": "https://github.com/alice/database-tools-mcp",
    "source": "github"
  },
  "version_detail": { "version": "1.0.0" },
  "packages": [
    {
      "registry_type": "pypi",
      "identifier": "database-tools-mcp", 
      "version": "1.0.0",
      "runtime_hint": "uvx",
      "environment_variables": [
        {
          "name": "DB_URL",
          "description": "Database connection URL (e.g. postgresql://user:pass@host/db)",
          "is_required": true,
          "is_secret": true
        },
        {
          "name": "POOL_SIZE",
          "description": "Maximum database connections",
          "default": "10"
        }
      ]
    }
  ]
}
```

## Validation

Always validate your server.json files:

```bash
# Using the publisher CLI (recommended)
mcp-publisher init --validate-only

# Using online validators
# Visit https://www.jsonschemavalidator.net/
# Use schema: https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json
```

## What You've Learned

You now understand:
- **Purpose** - server.json describes MCP servers for clients and registries
- **Structure** - Required fields and optional configuration sections
- **Packages** - How to reference different package types (npm, PyPI, Docker, etc.)
- **Configuration** - Environment variables and command-line arguments
- **Best practices** - Clear naming, helpful descriptions, sensible defaults

## Next Steps

- **Publish your server** - Use this knowledge in the [Publishing Tutorial](publish-your-first-server.md)
- **Explore examples** - See [server.json examples](../reference/server-json/examples.md) for more patterns
- **Read the schema** - Check the [complete specification](../reference/server-json/) for all available options