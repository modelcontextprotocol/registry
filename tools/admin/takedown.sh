#!/bin/bash
# Simple takedown script using the dedicated status endpoint

REGISTRY_URL="${REGISTRY_URL:-https://registry.modelcontextprotocol.io}"

if [ -z "$SERVER_ID" ] || [ -z "$REGISTRY_TOKEN" ]; then
    echo "Usage: REGISTRY_TOKEN=<token> SERVER_ID=<server-uuid> $0"
    echo "Example: REGISTRY_TOKEN=xyz SERVER_ID=abc123 $0"
    exit 1
fi

echo "Taking down server ${SERVER_ID}..."

# Update server status to deleted using the dedicated status endpoint
response=$(curl -s -w "\n%{http_code}" \
  -X PATCH "${REGISTRY_URL}/v0/servers/${SERVER_ID}/status" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"status": "deleted"}')

# Parse response and status code
http_code=$(echo "$response" | tail -n1)
body=$(echo "$response" | head -n -1)

if [ "$http_code" = "200" ]; then
    echo "✓ Server successfully marked as deleted"
    echo "Server ID: ${SERVER_ID}"
    echo ""
    echo "Response details:"
    echo "$body" | jq '.'
else
    echo "✗ Failed to take down server (HTTP $http_code)"
    echo "Response:"
    echo "$body"
    exit 1
fi