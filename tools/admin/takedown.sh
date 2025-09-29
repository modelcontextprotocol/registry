#!/bin/bash
# Simple takedown script

REGISTRY_URL="${REGISTRY_URL:-https://registry.modelcontextprotocol.io}"

if [ -z "$SERVER_NAME" ] || [ -z "$REGISTRY_TOKEN" ]; then
    echo "Usage: REGISTRY_TOKEN=<token> SERVER_NAME=<server-name> [VERSION=<version>] $0"
    echo "Example: REGISTRY_TOKEN=token SERVER_NAME=com.example/my-server ./takedown.sh"
    echo "Example: REGISTRY_TOKEN=token SERVER_NAME=com.example/my-server VERSION=1.0.0 ./takedown.sh"
    exit 1
fi

# URL encode the server name (replace / with %2F)
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

# Determine the endpoint based on whether VERSION is provided
if [ -n "$VERSION" ]; then
    # Mark specific version as deleted
    ENDPOINT="${REGISTRY_URL}/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}?status=deleted"
    echo "Marking version $VERSION of server $SERVER_NAME as deleted..."
else
    # Mark entire server as deleted (latest version)
    ENDPOINT="${REGISTRY_URL}/v0/servers/${ENCODED_SERVER_NAME}?status=deleted"
    echo "Marking server $SERVER_NAME as deleted..."
fi

# Update server status to deleted
curl -X PUT "$ENDPOINT" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json"