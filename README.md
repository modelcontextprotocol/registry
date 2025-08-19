# MCP Registry

A community driven registry service for Model Context Protocol (MCP) servers.

## Development Status

This project is being built in the open and is currently in the early stages of development. Please see the [overview discussion](https://github.com/modelcontextprotocol/registry/discussions/11) for the project scope and goals.

### Contributing

Use [Discussions](https://github.com/modelcontextprotocol/registry/discussions) to propose and discuss product and/or technical **requirements**.

Use [Issues](https://github.com/modelcontextprotocol/registry/issues) to track **well-scoped technical work** that the community agrees should be done at some point.

Open [Pull Requests](https://github.com/modelcontextprotocol/registry/pulls) when you want to **contribute work towards an Issue**, or you feel confident that your contribution is desireable and small enough to forego community discussion at the requirements and planning levels.

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
- Seed data export/import composability with HTTP support
- Registry instance data sharing via HTTP endpoints

## Getting Started

### Prerequisites

- Go 1.24.x (required - check with `go version`)
- MongoDB
- Docker (optional, but recommended for development)

For development:
- golangci-lint v2.4.0 - Install with:
  ```bash
  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v2.4.0
  ```

## Running

The easiest way to get the registry running is uses docker compose. This will setup the MCP Registry service, import the seed data and run MongoDB in a local Docker environment.

```bash
# Run the registry and MongoDB with docker compose
make dev-compose
```

This will start the MCP Registry service and MongoDB with Docker, running at [`localhost:8080`](http://localhost:8080).

## Building

If you prefer to run the service locally without Docker, you can build and run it directly:

```bash
# Build a registry executable
make build
```
This will create the `registry` binary in the current directory. You'll need to have MongoDB running locally or with Docker.

To run the service locally:
```bash
# Run registry locally (requires MongoDB)
make dev-local
```

By default, the service will run on [`localhost:8080`](http://localhost:8080).

To build the CLI tool for publishing MCP servers to the registry:

```bash
# Build the publisher tool
make publisher
```

## Development

### Available Make Targets

To see all available make targets:

```bash
make help
```

Key development commands:

```bash
# Development
make dev-compose   # Start development environment with Docker Compose
make dev-local     # Run registry locally (requires MongoDB)

# Build targets
make build          # Build the registry application
make publisher      # Build the publisher tool

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

### Project Structure

```
├── api/           # OpenApi specification
├── cmd/           # Application entry points
├── config/        # Configuration files
├── internal/      # Private application code
│   ├── api/       # HTTP server and request handlers (routing)
│   ├── auth/      # GitHub OAuth integration
│   ├── config/    # Configuration management
│   ├── database/  # Data persistence abstraction (MongoDB and in-memory)
│   ├── model/     # Data models and domain structures
│   └── service/   # Business logic implementation
├── pkg/           # Public libraries
├── scripts/       # Utility scripts
└── tools/         # Command line tools
    └── publisher/ # Tool to publish MCP servers to the registry
```

### Architecture Overview

### Request Flow
1. HTTP requests enter through router (`internal/api/router/`)
2. Handlers in `internal/api/handlers/v0/` validate and process requests
3. Service layer executes business logic
4. Database interface handles persistence
5. JSON responses returned to clients

### Key Interfaces
- **Database Interface** (`internal/database/database.go`) - Abstracts data persistence with MongoDB and memory implementations
- **RegistryService** (`internal/service/service.go`) - Business logic abstraction over database
- **Auth Service** (`internal/auth/auth.go`) - GitHub OAuth token validation

### Authentication Flow
Publishing requires GitHub OAuth validation:
1. Extract bearer token from Authorization header
2. Validate token with GitHub API
3. Verify repository ownership matches token owner
4. Check organization membership if applicable

### Design Patterns
- **Factory Pattern** for service creation with dependency injection
- **Repository Pattern** for database abstraction
- **Context Pattern** for timeout management (5-second DB operations)
- **Cursor-based Pagination** using UUIDs for stateless pagination

## API Endpoints

### API Documentation

```
GET /docs
```

The API is documented using Swagger/OpenAPI. This page provides a complete reference of all endpoints with request/response schemas and examples, and allows you to test the API directly from your browser.

## Configuration

The service can be configured using environment variables. See [.env.example](./.env.example) for details.

## Pre-built Docker Images

Pre-built Docker images are automatically published to GitHub Container Registry on each release and main branch commit:

```bash
# Run latest from main branch
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:latest

# Run specific commit build
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:main-20250806-a1b2c3d
```

**Available image tags:**
- `latest` - Latest commit from main branch
- `main-<date>-<sha>` - Specific commit builds

**Configuration:** The Docker images support all environment variables listed in the [Configuration](#configuration) section. For production deployments, you'll need to configure the database connection and other settings via environment variables.

### Import Seed Data

Registry instances can import data from:

**Local files:**
```bash
MCP_REGISTRY_SEED_FROM=data/seed.json ./registry
```

**HTTP endpoints:**
```bash
MCP_REGISTRY_SEED_FROM=http://other-registry:8080 ./registry
```

## Testing

Run the test script to validate API endpoints:

```bash
./scripts/test_endpoints.sh
```

You can specify specific endpoints to test:

```bash
./scripts/test_endpoints.sh --endpoint health
./scripts/test_endpoints.sh --endpoint servers
```

## License

See the [LICENSE](LICENSE) file for details.
