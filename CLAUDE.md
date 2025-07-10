# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Overview
MCP Registry is a community-driven registry service for Model Context Protocol (MCP) servers. It provides a centralized repository for discovering and managing MCP implementations.

## Common Development Commands

### Build
```bash
# Build the registry application
go build ./cmd/registry

# Build with Docker
docker build -t registry .

# Build the publisher tool
cd tools/publisher && ./build.sh
```

### Run
```bash
# Development with Docker Compose (recommended)
docker compose up

# Direct execution
go run cmd/registry/main.go
```

### Test
```bash
# Unit tests
go test -v -race -coverprofile=coverage.out -covermode=atomic ./internal/...

# Integration tests
./integrationtests/run_tests.sh

# Test API endpoints
./scripts/test_endpoints.sh

# Test publish endpoint (requires GitHub token)
export BEARER_TOKEN=your_github_token_here
./scripts/test_publish.sh
```

### Lint
```bash
# Install golangci-lint if needed
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.61.0

# Run linting
golangci-lint run --timeout=5m

# Check formatting
gofmt -s -l .
```

## Architecture Overview

The codebase follows a clean architecture pattern:

### Core Layers
- **API Layer** (`internal/api/`) - HTTP handlers and routing
- **Service Layer** (`internal/service/`) - Business logic implementation  
- **Database Layer** (`internal/database/`) - Data persistence abstraction with MongoDB and in-memory implementations
- **Domain Models** (`internal/model/`) - Core data structures
- **Authentication** (`internal/auth/`) - GitHub OAuth integration

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

## Environment Variables
Key configuration for development:
- `MCP_REGISTRY_DATABASE_URL` (default: `mongodb://localhost:27017`)
- `MCP_REGISTRY_GITHUB_CLIENT_ID` 
- `MCP_REGISTRY_GITHUB_CLIENT_SECRET`
- `MCP_REGISTRY_LOG_LEVEL` (default: `info`)
- `MCP_REGISTRY_SERVER_ADDRESS` (default: `:8080`)
- `MCP_REGISTRY_SEED_IMPORT` (default: `true`)
- `MCP_REGISTRY_SEED_FILE_PATH` (default: `data/seed.json`)