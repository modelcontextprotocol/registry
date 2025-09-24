#!/bin/bash

set -e

cleanup() {
    echo "========== registry logs =========="
    docker logs registry || true
    echo "=========== cleaning up ==========="
    docker compose down -v
}

go build -o ./bin/publisher ./cmd/publisher

docker build -t registry .

trap cleanup EXIT

docker compose -f docker-compose.yml -f tests/integration/docker-compose.integration-test.yml up --wait --wait-timeout 60

go run tests/integration/main.go
