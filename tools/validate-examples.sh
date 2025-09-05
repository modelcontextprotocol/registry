#!/bin/bash
# Validate example server.json files

set -e

cd "$(dirname "$0")/.."
exec go run tools/validate-examples/main.go "$@"
