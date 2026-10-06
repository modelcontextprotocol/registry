#!/bin/bash
# Simple OIDC authentication helper using gcloud

set -euo pipefail

REGISTRY_URL="${REGISTRY_URL:-https://registry.modelcontextprotocol.io}"

if ! gcloud projects list &> /dev/null; then
    gcloud auth login >&2
fi

# Get Google Cloud identity token
OIDC_TOKEN=$(gcloud auth print-identity-token)

# Exchange for registry token
RESPONSE=$(curl -sS -X POST "${REGISTRY_URL}/v0/auth/oidc" \
  -H "Content-Type: application/json" \
  -d "{\"oidc_token\": \"${OIDC_TOKEN}\"}")

# Check if successful
if ! REGISTRY_TOKEN=$(printf '%s\n' "$RESPONSE" | jq -r '.registry_token // empty' 2>/dev/null); then
    REGISTRY_TOKEN=""
fi

if [ -z "$REGISTRY_TOKEN" ]; then
    echo "Error: Authentication failed" >&2
    if ! printf '%s\n' "$RESPONSE" | jq '.' >&2 2>/dev/null; then
        printf '%s\n' "$RESPONSE" >&2
    fi
    exit 1
fi

# Output the export command
echo "# Successfully authenticated! Now run this to use your token:" >&2
echo "export REGISTRY_TOKEN='${REGISTRY_TOKEN}'"
