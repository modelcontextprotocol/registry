# Admin Operations

This is a brief guide for admins and moderators managing content on the registry. All actions should be taken in line with the [moderation policy](../modelcontextprotocol-io/moderation-policy.mdx).

## Prerequisites

- Admin account with @modelcontextprotocol.io email
  - If you are a maintainer and would like an account, ask in the Discord
- `gcloud` CLI installed and configured
- `curl` and `jq` installed
- `kubectl` installed with `gke-gcloud-auth-plugin` (for database access)

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

# Or get the latest version (use the special version "latest")
curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/latest" > server.json
```

### Step 2: Make Changes

Open `server.json` and edit the specific version details. You cannot change the server name or version number.

### Step 3: Update Version

```bash
# Update specific version (requires the full server.json body)
curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d "$(cat server.json)"
```

To change **only** the status of a version, use the status endpoint instead — it does not require
the full server configuration:

```bash
curl -X PATCH "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}/status" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"status": "deprecated", "statusMessage": "Superseded by v2"}'
```

## Edit an Entire Server (All Versions)

### Status Changes Across All Versions

A status change applies to every version of a server in a single request. The response reports
how many versions were updated in `updatedCount`.

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

curl -X PATCH "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/status" \
  -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
  -H "Content-Type: application/json" \
  -d '{"status": "deleted", "statusMessage": "Removed per moderation policy"}'
```

### Content Changes Across All Versions

Content edits (e.g. scrubbing sensitive text from descriptions) have no bulk endpoint and must be
applied per version using the edit endpoint, which takes the full server configuration.

#### Step 1: List All Versions

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions" > all_versions.json
```

#### Step 2: Extract Versions

```bash
# Extract all versions from the server
jq -r '.servers[].server.version' all_versions.json > versions.txt
```

#### Step 3: Apply Changes to Each Version

```bash
while read VERSION; do
  echo "Processing version: $VERSION"

  # Download the version, edit it, then send the full body back
  curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}" > version.json

  # Apply your changes to version.json here, then:
  curl -X PUT "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/${VERSION}" \
    -H "Authorization: Bearer ${REGISTRY_TOKEN}" \
    -H "Content-Type: application/json" \
    -d "$(cat version.json)"

done < versions.txt

# Clean up temporary files
rm -f versions.txt all_versions.json version.json
```

## Quick Operations

### Get Latest Version of a Server

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/latest" > latest_version.json
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

### Takedown All Versions of a Server

`ALL_VERSIONS=true` marks every version as deleted in a single request. The script requires either
`VERSION` or `ALL_VERSIONS` to be set explicitly, so a forgotten `VERSION` cannot take down a whole
server by accident.

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
export REGISTRY_TOKEN="<your-token>"

REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="$SERVER_NAME" ALL_VERSIONS=true ./tools/admin/takedown.sh
```

### Takedown the Latest Version Only

```bash
export SERVER_NAME="<server-name>"    # e.g., "com.example/my-server"
export REGISTRY_TOKEN="<your-token>"
ENCODED_SERVER_NAME=$(echo "$SERVER_NAME" | sed 's|/|%2F|g')

# Resolve the latest version, then take down that specific version
VERSION=$(curl -s "https://registry.modelcontextprotocol.io/v0/servers/${ENCODED_SERVER_NAME}/versions/latest" | jq -r '.server.version')

REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="$SERVER_NAME" VERSION="$VERSION" ./tools/admin/takedown.sh
```

### Record a Reason with a Takedown

```bash
REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="$SERVER_NAME" ALL_VERSIONS=true \
  STATUS_MESSAGE="Removed per moderation policy" ./tools/admin/takedown.sh
```

## Connecting to the Production Database

For debugging or data analysis, you can connect directly to the production PostgreSQL database. Use caution and prefer read-only access.

### Prerequisites

Install the GKE auth plugin if you haven't already:

```bash
gcloud components install gke-gcloud-auth-plugin
```

### Connect

```bash
# Get cluster credentials
gcloud container clusters get-credentials mcp-registry-prod --zone us-central1-b --project mcp-registry-prod

# Get the database password
kubectl get secret registry-pg-app -o jsonpath='{.data.password}' | base64 -d

# Port-forward and connect (enter the password from above)
kubectl port-forward svc/registry-pg-rw 15432:5432 &
sleep 2
psql -h localhost -p 15432 -U app -d app
```

### Read-Only Access

To prevent accidental writes, set your session to read-only after connecting:

```sql
SET default_transaction_read_only = on;
```

Any write attempts will fail with an error until you disconnect.

## Notes

- **Version-specific changes**: Only affect that particular version
- **Server-wide status changes**: `PATCH /v0/servers/{serverName}/status` updates every version in one request
- **Server-wide content changes**: Have no bulk endpoint and must be applied to each version individually
- **Status vs. edit**: Use `PATCH .../status` to change status alone; the `PUT` edit endpoint requires the full server configuration
- **Content scrubbing**: Use the version-specific edit workflow to scrub sensitive content
- **Server name**: Cannot be changed in any version (it's the immutable identifier)
