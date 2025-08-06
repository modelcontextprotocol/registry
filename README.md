# MCP Registry

A community driven registry service for Model Context Protocol (MCP) servers.

## Development Status

This project is being built in the open and is currently in the early stages of development. Please see the [overview discussion](https://github.com/modelcontextprotocol/registry/discussions/11) for the project scope and goals. If you would like to contribute, please check out the [contributing guidelines](CONTRIBUTING.md).

## Overview

The MCP Registry service provides a centralized repository for MCP server entries. It allows discovery and management of various MCP implementations with their associated metadata, configurations, and capabilities.

## Features

- RESTful API for managing MCP registry entries (list, get, create, update, delete)
- Health check endpoint for service monitoring
- Support for various environment configurations
- Graceful shutdown handling
- MongoDB and in-memory database support
- Comprehensive API documentation
- Pagination support for listing registry entries

## Getting Started

### Prerequisites

- Go 1.23.x (required - check with `go version`)
- MongoDB
- Docker (optional, but recommended for development)

For development:
- golangci-lint v2.3.1 - Install with:
  ```bash
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.3.1
  ```

## Running

The easiest way to get the registry running is uses docker compose. This will setup the MCP Registry service, import the seed data and run MongoDB in a local Docker environment.

```bash
# Run the registry and MongoDB with docker compose
make dev-compose
```

This will start the MCP Registry service and MongoDB with Docker, exposing it on port 8080.

## Building

If you prefer to run the service locally without Docker, you can build and run it directly using Go.

```bash
# Build a registry executable
make build
```
This will create the `registry` binary in the current directory. You'll need to have MongoDB running locally or with Docker.

By default, the service will run on `http://localhost:8080`.

## Development

### Available Make Targets

To see all available make targets:

```bash
make help
```

Key development commands:

```bash
# Build targets
make build          # Build the registry application
make publisher      # Build the publisher tool

# Development
make dev-compose   # Start development environment with Docker Compose
make dev-local     # Run registry locally (requires MongoDB)

# Testing
make test-unit        # Run unit tests with coverage report
make test-integration # Run integration tests
make test-endpoints   # Test API endpoints (requires running server)
make test-publish     # Test publish endpoint (requires BEARER_TOKEN)
make test-all         # Run all tests

# Code quality
make lint          # Run linter (same as CI)
make lint-fix      # Run linter with auto-fix

# Validation
make validate-schemas   # Validate JSON schemas
make validate-examples  # Validate examples against schemas
make validate          # Run all validation checks

# Combined workflows
make check         # Run all checks (lint, validate, unit tests)

# Utilities
make clean         # Clean build artifacts and coverage files
```

### Linting

The project uses golangci-lint with extensive checks. Always run linting before pushing:

```bash
# Run all linters (same as CI)
make lint

# Run linter with auto-fix
make lint-fix
```

### Git Hooks (Optional)

To automatically run linting before commits:

```bash
git config core.hooksPath .githooks
```

This will prevent commits that fail linting or have formatting issues.

## Project Structure

```
├── api/           # OpenApi specification
├── cmd/           # Application entry points
├── config/        # Configuration files
├── internal/      # Private application code
│   ├── api/       # HTTP server and request handlers
│   ├── config/    # Configuration management
│   ├── model/     # Data models
│   └── service/   # Business logic
├── pkg/           # Public libraries
├── scripts/       # Utility scripts
└── tools/         # Command line tools
    └── publisher/ # Tool to publish MCP servers to the registry
```

## API Documentation

The API is documented using Swagger/OpenAPI. You can access the interactive Swagger UI at:

```
/v0/swagger/index.html
```

This provides a complete reference of all endpoints with request/response schemas and allows you to test the API directly from your browser.

## API Endpoints

### Health Check

```
GET /v0/health
```

Returns the health status of the service:
```json
{
  "status": "ok"
}
```

### Registry Endpoints

#### List Registry Server Entries

```
GET /v0/servers
```

Lists MCP registry server entries with pagination support.

Query parameters:
- `limit`: Maximum number of entries to return (default: 30, max: 100)
- `cursor`: Pagination cursor for retrieving next set of results

Response example:
```json
{
  "servers": [
    {
      "id": "123e4567-e89b-12d3-a456-426614174000",
      "name": "Example MCP Server",
      "url": "https://example.com/mcp",
      "description": "An example MCP server",
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

#### Get Server Details

```
GET /v0/servers/{id}
```

Retrieves detailed information about a specific MCP server entry.

Path parameters:
- `id`: Unique identifier of the server entry

Response example:
```json
{
  "id": "01129bff-3d65-4e3d-8e82-6f2f269f818c",
  "name": "io.github.gongrzhe/redis-mcp-server",
  "description": "A Redis MCP server (pushed to https://github.com/modelcontextprotocol/servers/tree/main/src/redis) implementation for interacting with Redis databases. This server enables LLMs to interact with Redis key-value stores through a set of standardized tools.",
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

#### Publish a Server Entry

```
POST /v0/publish
```

Publishes a new MCP server entry to the registry. Authentication is required via Bearer token in the Authorization header.

Headers:
- `Authorization`: Bearer token for authentication (e.g., `Bearer your_token_here`)
- `Content-Type`: application/json

Request body example:
```json
{
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
        },{
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
}
```

Response example:
```json
{
  "message": "Server publication successful",
  "id": "1234567890abcdef12345678"
}
```

### Ping Endpoint

```
GET /v0/ping
```

Simple ping endpoint that returns environment configuration information:
```json
{
  "environment": "dev",
  "version": "registry-<sha>"
}
```

## Configuration

The service can be configured using environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| `MCP_REGISTRY_APP_VERSION`           | Application version | `dev` |
| `MCP_REGISTRY_DATABASE_TYPE`         | Database type | `mongodb` |
| `MCP_REGISTRY_COLLECTION_NAME`       | MongoDB collection name | `servers_v2` |
| `MCP_REGISTRY_DATABASE_NAME`         | MongoDB database name | `mcp-registry` |
| `MCP_REGISTRY_DATABASE_URL`          | MongoDB connection string | `mongodb://localhost:27017` |
| `MCP_REGISTRY_GITHUB_CLIENT_ID`      | GitHub App Client ID |  |
| `MCP_REGISTRY_GITHUB_CLIENT_SECRET`  | GitHub App Client Secret |  |
| `MCP_REGISTRY_LOG_LEVEL`             | Log level | `info` |
| `MCP_REGISTRY_SEED_FILE_PATH`        | Path to import seed file | `data/seed.json` |
| `MCP_REGISTRY_SEED_IMPORT`           | Import `seed.json` on first run | `true` |
| `MCP_REGISTRY_SERVER_ADDRESS`        | Listen address for the server | `:8080` |


## Testing

### Unit Tests

```bash
# Run unit tests with coverage
make test

# Generate coverage report
make coverage
```

### Integration Tests

```bash
# Run integration tests
make integration-test
```

### API Endpoint Testing

```bash
# Test API endpoints (requires running server)
make test-endpoints
```

You can also run the script directly with specific endpoints:

```bash
./scripts/test_endpoints.sh --endpoint health
./scripts/test_endpoints.sh --endpoint servers
```

### Publish Endpoint Testing

```bash
# Test publish endpoint (requires BEARER_TOKEN env var)
make test-publish
```

### Validation

```bash
# Validate JSON schemas
make validate-schemas

# Validate examples against schemas
make validate-examples

# Run all validation checks
make validate
```

### Comprehensive Testing

```bash
# Run all checks (lint, validate, test)
make check
```

## License

See the [LICENSE](LICENSE) file for details.

## Contributing

See the [CONTRIBUTING](CONTRIBUTING.md) file for details.
