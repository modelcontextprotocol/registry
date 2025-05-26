#!/bin/bash

# Integration Test Runner for MCP Registry
# This script runs the integration tests for the publish functionality

echo "Running MCP Registry Integration Tests..."
echo "========================================"

# Change to the project directory (parent of integrationtests)
cd "$(dirname "$0")/.."

# Run integration tests with verbose output
echo "Running publish integration tests..."
go test -v ./integrationtests/...

# Check exit code
if [ $? -eq 0 ]; then
    echo ""
    echo "✅ All integration tests passed!"
else
    echo ""
    echo "❌ Some integration tests failed!"
    exit 1
fi
