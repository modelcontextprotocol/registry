#!/bin/bash
# Set the lifecycle status of an MCP server, either for one version or for every version.

set -euo pipefail

REGISTRY_URL="${REGISTRY_URL:-https://registry.modelcontextprotocol.io}"
STATUS="${STATUS:-deleted}"
SERVER_NAME="${SERVER_NAME:-}"
VERSION="${VERSION:-}"
ALL_VERSIONS="${ALL_VERSIONS:-}"
STATUS_MESSAGE="${STATUS_MESSAGE:-}"

usage() {
    cat <<'EOF'
Usage:
  REGISTRY_TOKEN=<token> SERVER_NAME=<server-name> VERSION=<version> ./takedown.sh
  REGISTRY_TOKEN=<token> SERVER_NAME=<server-name> ALL_VERSIONS=true ./takedown.sh

Examples:
  REGISTRY_TOKEN=token SERVER_NAME=com.example/my-server VERSION=1.0.0 ./takedown.sh
  REGISTRY_TOKEN=token SERVER_NAME=com.example/my-server ALL_VERSIONS=true ./takedown.sh

Optional environment variables:
  STATUS          active | deprecated | deleted (default: deleted)
  STATUS_MESSAGE  reason recorded alongside the status change (max 500 characters)
  REGISTRY_URL    registry base URL (default: https://registry.modelcontextprotocol.io)
EOF
}

if [ -z "$SERVER_NAME" ] || [ -z "${REGISTRY_TOKEN:-}" ]; then
    usage
    exit 1
fi

# Require an explicit choice, so that a forgotten VERSION cannot silently take
# down every version of a server.
if [ -n "$VERSION" ] && [ -n "$ALL_VERSIONS" ]; then
    echo "Error: set either VERSION or ALL_VERSIONS, not both." >&2
    exit 1
fi
if [ -z "$VERSION" ] && [ -z "$ALL_VERSIONS" ]; then
    echo "Error: set VERSION=<version> for a single version, or ALL_VERSIONS=true for every version." >&2
    echo >&2
    usage
    exit 1
fi

# URL encode the server name and version (replace / with %2F)
ENCODED_SERVER_NAME="${SERVER_NAME//\//%2F}"

if [ -n "$VERSION" ]; then
    ENCODED_VERSION="${VERSION//\//%2F}"
    ENDPOINT="${REGISTRY_URL}/v0/servers/${ENCODED_SERVER_NAME}/versions/${ENCODED_VERSION}/status"
    echo "Setting version ${VERSION} of ${SERVER_NAME} to '${STATUS}'..."
else
    ENDPOINT="${REGISTRY_URL}/v0/servers/${ENCODED_SERVER_NAME}/status"
    echo "Setting ALL versions of ${SERVER_NAME} to '${STATUS}'..."
fi

if [ -n "$STATUS_MESSAGE" ]; then
    BODY=$(jq -n --arg status "$STATUS" --arg message "$STATUS_MESSAGE" \
        '{status: $status, statusMessage: $message}')
else
    BODY=$(jq -n --arg status "$STATUS" '{status: $status}')
fi

RESPONSE_BODY=$(mktemp)
trap 'rm -f "$RESPONSE_BODY"' EXIT

HTTP_CODE=$(curl -sS -X PATCH "$ENDPOINT" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$BODY" \
  -o "$RESPONSE_BODY" \
  -w '%{http_code}')

cat "$RESPONSE_BODY"
echo

if [ "$HTTP_CODE" -lt 200 ] || [ "$HTTP_CODE" -ge 300 ]; then
    echo "Request failed with HTTP ${HTTP_CODE}" >&2
    exit 1
fi
