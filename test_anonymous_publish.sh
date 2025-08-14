#!/bin/bash

# Exit on error
set -e

# Base URL for the registry
BASE_URL="${BASE_URL:-http://localhost:8080}"

echo "Testing anonymous auth and publish flow..."
echo "Using registry at: $BASE_URL"

# Step 1: Get anonymous token
echo ""
echo "1. Getting anonymous token..."
TOKEN_RESPONSE=$(curl -s -X POST "$BASE_URL/v0/auth/none")
echo "Response: $TOKEN_RESPONSE"

# Extract the registry_token from the response
REGISTRY_TOKEN=$(echo "$TOKEN_RESPONSE" | grep -o '"registry_token":"[^"]*' | cut -d'"' -f4)

if [ -z "$REGISTRY_TOKEN" ]; then
    echo "Failed to get registry token"
    exit 1
fi

echo "Got token: ${REGISTRY_TOKEN:0:50}..."

# Step 2: Use token to publish a test server
echo ""
echo "2. Publishing test server..."

# Create a minimal server JSON with the ID in the payload
SERVER_JSON='{
  "id": "io.modelcontextprotocol.anonymous/test-server",
  "name": "io.modelcontextprotocol.anonymous/test-server",
  "description": "A test server published via anonymous auth",
  "packages": [
    {
      "environment_variables": [
        {
          "choices": [
            "string"
          ],
          "default": "string",
          "description": "string",
          "format": "string",
          "is_required": true,
          "is_secret": true,
          "name": "string",
          "value": "string",
          "variables": {
            "property1": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            },
            "property2": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            }
          }
        }
      ],
      "name": "string",
      "package_arguments": [
        {
          "choices": [
            "string"
          ],
          "default": "string",
          "description": "string",
          "format": "string",
          "is_repeated": true,
          "is_required": true,
          "is_secret": true,
          "name": "string",
          "type": "string",
          "value": "string",
          "value_hint": "string",
          "variables": {
            "property1": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            },
            "property2": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            }
          }
        }
      ],
      "registry_name": "string",
      "runtime_arguments": [
        {
          "choices": [
            "string"
          ],
          "default": "string",
          "description": "string",
          "format": "string",
          "is_repeated": true,
          "is_required": true,
          "is_secret": true,
          "name": "string",
          "type": "string",
          "value": "string",
          "value_hint": "string",
          "variables": {
            "property1": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            },
            "property2": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            }
          }
        }
      ],
      "runtime_hint": "string",
      "version": "string"
    }
  ],
  "remotes": [
    {
      "headers": [
        {
          "choices": [
            "string"
          ],
          "default": "string",
          "description": "string",
          "format": "string",
          "is_required": true,
          "is_secret": true,
          "name": "string",
          "value": "string",
          "variables": {
            "property1": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            },
            "property2": {
              "choices": [
                "string"
              ],
              "default": "string",
              "description": "string",
              "format": "string",
              "is_required": true,
              "is_secret": true,
              "value": "string"
            }
          }
        }
      ],
      "transport_type": "string",
      "url": "string"
    }
  ],
  "repository": {
    "id": "string",
    "source": "string",
    "url": "string"
  },
  "status": "string",
  "version_detail": {
    "is_latest": true,
    "release_date": "string",
    "version": "string"
  }
}'

# Publish the server
PUBLISH_RESPONSE=$(curl -s -X POST "$BASE_URL/v0/publish" \
  -H "Authorization: Bearer $REGISTRY_TOKEN" \
  -H "Content-Type: application/json" \
  -d "$SERVER_JSON")

echo "Publish response: $PUBLISH_RESPONSE"

# Check if publish was successful (look for an ID in the response)
if echo "$PUBLISH_RESPONSE" | grep -q '"id"'; then
    echo ""
    echo "✅ Success! Server published with anonymous auth"
    SERVER_ID=$(echo "$PUBLISH_RESPONSE" | grep -o '"id":"[^"]*' | cut -d'"' -f4)
    echo "Server ID: $SERVER_ID"
else
    echo ""
    echo "❌ Failed to publish server"
    exit 1
fi

# Step 3: Verify the server exists
echo ""
echo "3. Verifying server exists..."
VERIFY_RESPONSE=$(curl -s "$BASE_URL/v0/servers/io.modelcontextprotocol.anonymous/test-server")

if echo "$VERIFY_RESPONSE" | grep -q '"id"'; then
    echo "✅ Server verified successfully"
else
    echo "❌ Failed to verify server"
    exit 1
fi

echo ""
echo "All tests passed!"