#!/bin/bash
# Validate example server.json files in the publishing guide

set -e

cd "$(dirname "$0")/.."
exec go run tools/validate-guide-examples/main.go "$@"