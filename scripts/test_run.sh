#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

echo "Starting MCP Registry test run..."

# Change to project root
cd "$PROJECT_ROOT"

# Build the binary
echo "Building registry binary..."
if ! go build ./cmd/registry; then
    echo "Error: Failed to build registry binary" >&2
    exit 1
fi

# Copy seed data if it doesn't exist
if [ ! -f data/seed.json ]; then
    echo "Copying seed data..."
    if ls data/seed*.json 1> /dev/null 2>&1; then
        cp data/seed*.json data/seed.json
        echo "Seed data copied successfully"
    else
        echo "Warning: No seed files found matching data/seed*.json" >&2
    fi
else
    echo "Seed data already exists, skipping copy"
fi

# Copy environment configuration
echo "Setting up environment configuration..."
if [ ! -f .env ]; then
    if [ -f .env.example ]; then
        cp .env.example .env
        echo "Environment configuration copied from .env.example"
    else
        echo "Warning: .env.example not found" >&2
    fi
else
    echo ".env already exists, skipping copy"
fi

# Run the binary
echo "Starting registry server..."
echo "Press Ctrl+C to stop the server"
./registry
