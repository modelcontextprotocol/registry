#!/bin/bash
# Simple takedown script

REGISTRY_URL="${REGISTRY_URL:-https://registry.modelcontextprotocol.io}"

if [ -z "$SERVER_ID" ] || [ -z "$REGISTRY_TOKEN" ]; then
    echo "Usage: REGISTRY_TOKEN=<token> SERVER_ID=<server-uuid> $0"
    exit 1
fi

# Get current server and update status to deleted
curl -s "${REGISTRY_URL}/v0/servers/${SERVER_ID}" | \
jq '.status = "deleted" | {server: .}' | \
curl -X PUT "${REGISTRY_URL}/v0/servers/${SERVER_ID}" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d @-