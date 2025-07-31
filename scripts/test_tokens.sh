#!/bin/bash
# Test script for verification token endpoints

BASE_URL="http://localhost:8080"
AUTH_TOKEN="your_github_token_here"
SERVER_ID="io.github.example/test-server"

echo "Testing Token API Endpoints"
echo "============================"

# First, ensure the server exists (you may need to publish it first)
echo "1. Publishing a test server..."
curl -X POST "${BASE_URL}/v0/publish" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "id": "'${SERVER_ID}'",
    "name": "'${SERVER_ID}'",
    "description": "Test server for token verification",
    "repository": {
      "url": "https://github.com/example/test-server",
      "source": "github",
      "id": "example/test-server"
    },
    "version_detail": {
      "version": "1.0.0",
      "release_date": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",
      "is_latest": true
    }
  }' | jq .

echo -e "\n2. Generating verification token..."
TOKEN_RESPONSE=$(curl -s -X POST "${BASE_URL}/v0/verification/generate" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" \
  -d '{
    "server_id": "'${SERVER_ID}'"
  }')

echo $TOKEN_RESPONSE | jq .

echo -e "\n3. Retrieving verification token..."
curl -s -X GET "${BASE_URL}/v0/verification/${SERVER_ID}" \
  -H "Authorization: Bearer ${AUTH_TOKEN}" | jq .

echo -e "\n4. Testing unauthorized access (should fail)..."
curl -s -X GET "${BASE_URL}/v0/verification/${SERVER_ID}" | jq .

echo -e "\nToken API testing complete!"
