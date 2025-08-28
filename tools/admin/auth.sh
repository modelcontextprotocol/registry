#!/bin/bash
# Simple OIDC authentication helper using gcloud

REGISTRY_URL="${REGISTRY_URL:-https://registry.modelcontextprotocol.io}"

if ! gcloud projects list &> /dev/null; then
    gcloud auth login >&2
fi

# Get Google Cloud identity token
OIDC_TOKEN=$(gcloud auth print-identity-token)

# Exchange for registry token
RESPONSE=$(curl -s -X POST "${REGISTRY_URL}/v0/auth/oidc" \
  -H "Content-Type: application/json" \
  -d "{\"oidc_token\": \"${OIDC_TOKEN}\"}")

# Check if successful
REGISTRY_TOKEN=$(echo "$RESPONSE" | jq -r '.registry_token // empty')

if [ -z "$REGISTRY_TOKEN" ]; then
    echo "Error: Authentication failed" >&2
    echo "$RESPONSE" | jq '.' >&2
    exit 1
fi

# Output the export command
echo "# Successfully authenticated! Now run this to use your token:" >&2
echo "export REGISTRY_TOKEN='${REGISTRY_TOKEN}'"