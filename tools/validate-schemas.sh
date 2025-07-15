#!/bin/bash
# Validate JSON schema files
# For more information, see docs/server-json/README.md

set -e

cd "$(dirname "$0")/.."
exec go run tools/validate-schemas/main.go "$@"
