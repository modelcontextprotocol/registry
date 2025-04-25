# MCP Registry

A reference implementation of a registry service for Model Context Protocol (MCP) servers.

> **Note:** This project is currently a work in progress.

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

## Project Structure

```
├── cmd/           # Application entry points
├── config/        # Configuration files
├── internal/      # Private application code
│   ├── api/       # HTTP server and request handlers
│   ├── config/    # Configuration management
│   ├── model/     # Data models
│   └── service/   # Business logic
├── pkg/           # Public libraries
├── scripts/       # Utility scripts
├── tools/         # Command line tools
│   └── importer/  # MongoDB data importer tool
└── build/         # Build artifacts
```

## Getting Started

### Prerequisites

- Go 1.18 or later
- MongoDB (optional, for production use)
- Docker (optional, for containerized deployment)

### Building

```bash
make build
```

This will create the `registry` binary in the `build/` directory.

### Running

```bash
./build/mcp-registry
```

Alternatively, run
```bash
MCP_REGISTRY_DATABASE_URL="mongodb://localhost:27017" ./build/registry
```

By default, the service will run on `http://localhost:8080`.

### Docker Deployment

You can build and run the service using Docker:

```bash
# Build the Docker image
docker build -t registry .

# Run the registry and MongoDB with docker compose
docker compose up
```

This will start the MCP Registry service and MongoDB with Docker, exposing it on port 8080.

### Using the Fake Service

For development and testing purposes, the application includes a fake service with pre-populated registry entries. To use the fake service:

1. Set the environment to "test":

```bash
export MCP_REGISTRY_ENVIRONMENT=test
./build/registry
```

Alternatively, run

```bash
make run-test
```

The fake service provides three sample MCP registry entries with the following capabilities:

- Registry 1: Code generation and completion capabilities
- Registry 2: Chat and knowledge base capabilities
- Registry 3: Data visualization and analysis capabilities

You can interact with the fake data through the API endpoints:

- List all entries: `GET /servers`


The fake service is useful for:
- Frontend development without a real backend
- Testing API integrations
- Example data structure reference

## Tools

### Data Importer

A command-line tool for importing server data from a JSON file into a MongoDB database:

```bash
cd tools/importer
go run main.go -uri mongodb://localhost:27017 -db mcp_registry -collection servers -seed ../../data/seed.json
```

For more details on the importer tool, see the [importer README](./tools/importer/README.md).

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
      "id": "1",
      "name": "Example MCP Server",
      "description": "An example MCP server implementation",
      "url": "https://example.com/mcp",
      "repository": {
        "url": "https://github.com/example/mcp-server",
        "stars": 120
      },
      "version": "1.0.0",
    }],
   "metadata": {
    "next_cursor": "cursor-value-for-next-page"
  }
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
| `MCP_REGISTRY_ENVIRONMENT`     | Application environment (production, test) | `production` |
| `MCP_REGISTRY_APP_VERSION`     | Application version | `dev` |
| `MCP_REGISTRY_DATABASE_URL`    | MongoDB connection string | `mongodb://localhost:27017` |
| `MCP_REGISTRY_DATABASE_NAME`   | MongoDB database name | `mcp-registry` |
| `MCP_REGISTRY_COLLECTION_NAME` | MongoDB collection name for server registry | `servers_v2` |

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
