#!/bin/bash

set -e

cleanup() {
    echo "========== registry logs =========="
    docker logs registry || true
    echo "=========== cleaning up ==========="
    docker compose down -v
}

go build -o ./bin/publisher ./tools/publisher

docker build -t registry --build-arg GO_BUILD_TAGS=noauth .

trap cleanup EXIT

export MCP_REGISTRY_GITHUB_CLIENT_ID=fake
export MCP_REGISTRY_GITHUB_CLIENT_SECRET=fake
docker compose -f docker-compose.yml -f tests/integration/docker-compose.integration-test.yml up --wait --wait-timeout 60

go run tests/integration/main.go
