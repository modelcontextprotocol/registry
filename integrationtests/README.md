# Integration Tests

This directory contains integration tests for the MCP Registry API using the fake service implementation.

## Overview

The integration tests are designed to test the complete flow of the publish endpoint using real service implementations (fake service) rather than mocks. This provides confidence that the entire request/response cycle works correctly.

## Test Structure

### `publish_integration_test.go`

Contains comprehensive integration tests for the publish endpoint:

- **TestPublishIntegration**: Tests various scenarios for publishing servers
  - Successful publish with GitHub authentication
  - Successful publish without authentication (for non-GitHub servers)
  - Error cases: missing name, missing version, missing auth header, invalid JSON, unsupported HTTP methods
  - Duplicate package handling: fails when same name+version, succeeds with different versions

- **TestPublishIntegrationWithComplexPackages**: Tests publishing servers with complex package configurations
  - Multiple runtime arguments (named and positional)
  - Package arguments
  - Environment variables (including secrets)
  - Multiple remotes with different transport types
  - Headers for HTTP remotes

- **TestPublishIntegrationEndToEnd**: Tests the complete end-to-end flow
  - Publishes a server and verifies it can be retrieved
  - Checks that the server appears in the registry list
  - Verifies count consistency

## Mock Services

### MockAuthService

A simple mock implementation of the `auth.Service` interface that:
- Accepts any non-empty token for GitHub authentication
- Always allows authentication for `AuthMethodNone`
- Provides realistic responses for auth flow methods

## Running the Tests

From the project root directory:

```bash
# Run all integration tests
go test ./integrationtests/...

# Run with verbose output
go test -v ./integrationtests/...

# Run a specific test
go test -v ./integrationtests/ -run TestPublishIntegration

# Run tests with race detection
go test -race ./integrationtests/...

# Use the convenient test runner script
./integrationtests/run_tests.sh
```

## Test Data

The tests use the fake service which comes pre-populated with sample data:
- 3 sample MCP servers with different configurations
- Uses in-memory database for isolation between tests
- Each test creates unique server instances with UUIDs

## Benefits of Integration Tests

1. **Real Flow Testing**: Tests the actual HTTP request/response cycle
2. **Service Integration**: Validates that handlers work correctly with service implementations
3. **Data Persistence**: Verifies that published data can be retrieved
4. **Error Handling**: Tests complete error scenarios end-to-end
5. **Complex Scenarios**: Tests realistic server configurations with packages and remotes

## Dependencies

These tests use:
- `testify/assert` and `testify/require` for assertions
- `httptest` for HTTP testing utilities
- The fake service implementation for realistic data operations
- Standard Go testing package

## Test Coverage

The integration tests cover:
- ✅ Successful publish scenarios
- ✅ Authentication validation
- ✅ Input validation
- ✅ Duplicate package handling
- ✅ Complex package configurations
- ✅ Multiple remotes
- ✅ Error handling
- ✅ End-to-end data flow
- ✅ HTTP method validation
- ✅ JSON parsing errors
