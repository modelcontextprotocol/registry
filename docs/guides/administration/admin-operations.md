# Admin Operations

This is a brief guide for admins and moderators managing content on the registry. All actions should be taken in line with the [moderation guidelines](moderation-guidelines.md).

## Prerequisites

- Admin account with @modelcontextprotocol.io email
  - If you are a maintainer and would like an account, ask in the Discord
- `gcloud` CLI installed and configured
- `curl` and `jq` installed

## Authentication

```bash
# Run this, then run the export command it outputs
./tools/admin/auth.sh
```

## Edit a Specific Server Version

Use this when you need to modify details of a specific version (e.g., fix description, update status, modify packages).

### Step 1: Download Specific Version

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
export VERSION="<version-string>"     # e.g., "1.0.0" (optional, defaults to latest)

# URL encode the server name (replace / with %2F)
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

# Get specific version
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}" > server.json

# Or get latest version (omit /versions/VERSION)
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}" > server.json
```

### Step 2: Make Changes

Open `server.json` and edit the specific version details. You cannot change the server name or version number.

### Step 3: Update Version

```bash
# Update specific version
curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(cat server.json)"

```

## Edit an Entire Server (All Versions)

Use this when you need to apply changes across all versions of a server (e.g., mark entire server as deleted, apply content scrubbing).

### Step 1: List All Versions

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions" > all_versions.json
```

### Step 2: Extract Versions

```bash
# Extract all versions from the server
jq -r '.servers[].server.version' all_versions.json > versions.txt
```

### Step 3: Apply Changes to All Versions

```bash
# For each version, apply changes using the edit endpoint
while read VERSION; do
  echo "Processing version: $VERSION"

  # Apply your changes here (e.g., set status to "deleted")
  curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}?status=deleted" \
    -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
    -H "Content-Type: application/json"

done < versions.txt

# Clean up temporary files
rm versions.txt all_versions.json
```

## Quick Operations

### Get Latest Version of a Server

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}" > latest_version.json
export VERSION=$(jq -r '.server.version' latest_version.json)
echo "Latest version: $VERSION"
```

### Takedown a Specific Version

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
export VERSION="<version-string>"     # e.g., "1.0.0"
export REGISTRY_TOKEN="<your-token>"

REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="$SERVER_NAME" VERSION="$VERSION" ./tools/admin/takedown.sh
```

### Takedown Latest Version (Entire Server)

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
export REGISTRY_TOKEN="<your-token>"

# This marks the latest version as deleted
REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="$SERVER_NAME" ./tools/admin/takedown.sh
```

### Takedown All Versions of a Server

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
export REGISTRY_TOKEN="<your-token>"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

# Get all versions and takedown each one
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions" | \
  jq -r '.servers[].server.version' | \
  while read VERSION; do
    echo "Taking down version: $VERSION"
    REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="$SERVER_NAME" VERSION="$VERSION" ./tools/admin/takedown.sh
  done
```

## Notes

- **Version-specific changes**: Only affect that particular version
- **Server-wide changes**: Must be applied to each version individually  
- **Content scrubbing**: Use the version-specific edit workflow to scrub sensitive content
- **Server name**: Cannot be changed in any version (it's the immutable identifier)
