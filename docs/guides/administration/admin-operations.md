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
export SERVER_ID="<server-uuid>"    # This is the server ID (consistent across versions)
export VERSION="<version-string>"   # e.g., "1.0.0" (optional, defaults to latest)

# Get specific version
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}?version=${VERSION}" > server.json

# Or get latest version (omit version parameter)
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}" > server.json
```

### Step 2: Make Changes

Open `server.json` and edit the specific version details. You cannot change the server name or version number.

### Step 3: Update Version

```bash
# Update specific version
curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}?version=${VERSION}" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(cat server.json)"

```

## Edit an Entire Server (All Versions)

Use this when you need to apply changes across all versions of a server (e.g., mark entire server as deleted, apply content scrubbing).

### Step 1: List All Versions

```bash
export SERVER_ID="<server-uuid>"    # This is the server ID (consistent across versions)
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}/versions" > all_versions.json
```

### Step 2: Extract Version IDs

```bash
# Extract all version IDs from the server
jq -r '.servers[]._meta."io.modelcontextprotocol.registry/official".version_id' all_versions.json > version_ids.txt
```

### Step 3: Apply Changes to All Versions

```bash
# For each version, download, modify, and update
while read VERSION_ID; do
  echo "Processing version: $VERSION_ID"
  curl -s "https://registry.modelcontextprotocol.io/v0/servers/${VERSION_ID}" > temp_version.json
  
  # Apply your changes here (e.g., set status to "deleted")
  jq '.status = "deleted"' temp_version.json > modified_version.json
  
  curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${VERSION_ID}" \
    -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "{\"server\": $(cat modified_version.json)}"
done < version_ids.txt

# Clean up temporary files
rm temp_version.json modified_version.json version_ids.txt all_versions.json
```

## Quick Operations

### Get Latest Version of a Server

```bash
export SERVER_ID="<server-uuid>"
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}" > latest_version.json
export VERSION_ID=$(jq -r '._meta."io.modelcontextprotocol.registry/official".version_id' latest_version.json)
echo "Latest version ID: $VERSION_ID"
```

### Takedown a Specific Version

```bash
export VERSION_ID="<version-uuid>"
./tools/admin/takedown.sh
```

### Takedown an Entire Server (All Versions)

```bash
export SERVER_ID="<server-uuid>"
# Get all version IDs and takedown each one
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${SERVER_ID}/versions" | \
  jq -r '.servers[]._meta."io.modelcontextprotocol.registry/official".version_id' | \
  while read VERSION_ID; do
    echo "Taking down version: $VERSION_ID"
    ./tools/admin/takedown.sh "$VERSION_ID"
  done
```

## Notes

- **Version-specific changes**: Only affect that particular version
- **Server-wide changes**: Must be applied to each version individually  
- **Content scrubbing**: Use the version-specific edit workflow to scrub sensitive content
- **Server name**: Cannot be changed in any version (it's the immutable identifier)
