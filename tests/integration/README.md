# Integration Test

This directory contains an end-to-end test for publishing to the registry.

## What the Test Covers

1. **Publisher Tool**: Tests the `publisher` CLI that publishes metadata to the registry
2. **Registry API**: Validates the `/v0/publish` and `/v0/servers/{server_id}` endpoints work correctly
3. **Example Validation**: Ensures all example JSON in `docs/reference/server-json/generic-server-json.md` is valid and can be published
4. **Data Consistency**: Verifies published data matches what's retrieved from the registry

## Test Flow

1. **Build**: Build `publisher` and `registry`
2. **Start Services**: Launch registry and MongoDB using Docker Compose with test configuration
3. **Publish Examples**: Extract JSON examples from documentation and run `publisher` to publish each one
4. **Validate Responses**: GET each published server from the registry and compare it to the example JSON
5. **Cleanup**: Stop Docker containers and remove temporary files

## How to Run

### Prerequisites

- Docker and Docker Compose
- Go 1.24
- Make sure you're in the repository root directory

### Run the Tests

```sh
./tests/integration/run.sh
```
