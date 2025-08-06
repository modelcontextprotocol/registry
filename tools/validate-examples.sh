#!/bin/bash
# Validate examples in docs/server-json/examples.md
# For more information, see docs/server-json/README.md

set -e

cd "$(dirname "$0")/.."
exec go run tools/validate-examples/main.go "$@"
