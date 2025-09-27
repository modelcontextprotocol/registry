#!/bin/bash

set -e

# Use a unique project name to avoid conflicts with dev environment
export COMPOSE_PROJECT_NAME=registry-integration-test

cleanup() {
    echo "========== registry logs =========="
    docker logs registry-integration-test || true
    echo "=========== cleaning up ==========="

    # Clean up integration test containers and volumes
    docker compose -f docker-compose.yml -f tests/integration/docker-compose.integration-test.yml down -v || true

    # Force remove containers by name as backup (in case compose context is confused)
    docker rm -f registry-integration-test postgres-integration-test 2>/dev/null || true

    # Clean up any leftover volumes that might have been created
    docker volume prune -f || true

    echo "Cleanup completed"
}

go build -o ./bin/publisher ./cmd/publisher

docker build -t registry .

trap cleanup EXIT

# Clean up any leftover containers from previous runs
echo "Pre-cleanup: removing any leftover containers..."
cleanup

docker compose -f docker-compose.yml -f tests/integration/docker-compose.integration-test.yml up --wait --wait-timeout 60

go run tests/integration/main.go
