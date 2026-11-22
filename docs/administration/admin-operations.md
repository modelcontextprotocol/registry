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

## Malware Checks

The registry checks the npm, PyPI, NuGet and Cargo packages that servers point at against the
malware records (`MAL-*`) that [OSV.dev](https://osv.dev) publishes. OCI images and MCPB bundles
are not covered.

A package matches when a record names exactly the version the server points at, or names no
versions at all, which is how OSV records a package that is malware in every release. A record
that lists some versions does not match the others, even where it also carries a range covering
them. Names and versions are compared the way OSV and the package's registry do: on PyPI `2.0`,
`v2.0.0` and `2.0.0.0` are one release, as are `1.0alpha1` and `1.0a1`, and a crate is the same
under either case and with `-` or `_`. An npm dist-tag such as `next` is not a version: it is refused on publish, and
an existing entry that names one cannot be checked. Neither can one that names no version. The
sweep lists both as `REGISTRY_MALWARE_SWEEP_UNCHECKED`.

- **On publish and edit**: what happens to a matching package is set by
  `MCP_REGISTRY_MALWARE_CHECK_MODE`. Every match is logged as `REGISTRY_MALWARE_PUBLISH_MATCH`.
  - `report` (the default) lets the request through, logged with `action=allowed`. It is for
    watching the check before relying on it: a wrong match shows up here without having cost a
    publisher anything, and a missed one shows up in the daily sweep.
  - `enforce` refuses the request with a `400` that links to the OSV record, logged with
    `action=refused`.
  - `off` does not ask OSV.

  The check runs alongside the package-ownership lookup. OSV takes about 0.3 seconds to answer,
  which is longer than the lookup, so a publish takes about that much longer.
  If OSV has not answered within 2 seconds the request goes ahead unchecked; a match OSV did
  confirm in that time is still acted on.
- **Daily**: a sweep re-checks every listed (not deleted) server version, because OSV usually
  learns about a malicious release after it was published here. The sweep only reports. It never
  changes an entry; taking one down is a maintainer's decision.

### Responding to an alert

On the hosted instance the sweep runs as the CronJob `registry-malware-sweep` and alerts by email
(see [Malware sweep](../../deploy/README.md#malware-sweep)). Do not discuss a match in public
channels before the entry is down.

A publish-time match alerts too. Find it in the registry's own log, then follow steps 2 to 4
below. With `action=refused` nothing was published. With `action=allowed` this check let the
request through, and step 3 shows whether the version was published: another check, such as
package ownership, may still have refused it:

```bash
kubectl -n default logs -l app=mcp-registry --tail=-1 | grep ' WARN REGISTRY_MALWARE_PUBLISH_MATCH '
```

1. Read the matches of the newest run. Each line names a server version, the package it points at
   and the OSV record. Earlier runs are kept beside it, and theirs may be out of date:

   ```bash
   JOB=$(kubectl -n default get jobs -l app=registry-malware-sweep \
     --sort-by=.metadata.creationTimestamp -o name | tail -1 | cut -d/ -f2)
   # A run that failed was tried again in a second pod, so read every pod of the Job.
   kubectl -n default logs -l job-name="$JOB" --tail=-1 --prefix | grep 'REGISTRY_MALWARE_SWEEP_'
   ```

   ```
   REGISTRY_MALWARE_SWEEP_HIT server="com.example/my-server" version="1.0.0" registry_type="npm" package="my-package" package_version="1.0.0" osv="MAL-2026-1234"
   ```

2. Confirm the record at `https://osv.dev/vulnerability/<id>` and that it lists the package version.
3. Confirm that the entry still points at that package version. It may have been edited or taken
   down since the run; a version that is already deleted answers `404`:

   ```bash
   curl -s "https://registry.modelcontextprotocol.io/v0/servers/com.example%2Fmy-server/versions/1.0.0" \
     | jq '.server.packages[] | {registryType, identifier, version}'
   ```

4. Take that version down, with the reason (see [Takedown a Specific Version](#takedown-a-specific-version)):

   ```bash
   REGISTRY_TOKEN="$REGISTRY_TOKEN" SERVER_NAME="com.example/my-server" VERSION="1.0.0" \
     STATUS_MESSAGE="Removed per moderation policy: package listed as malware (OSV MAL-2026-1234)" \
     ./tools/admin/takedown.sh
   ```

   Take down the single version, not the whole server, unless every version matches: the registry
   then promotes the highest remaining version to latest.

The sweep reports a match every day until the version is deleted.

### Running a sweep by hand

The sweep needs no credentials and changes nothing, so it can be run from anywhere. It reads the
public listing, which takes about 12 minutes:

```bash
go run ./cmd/registry -malware-sweep
```

It ends with `REGISTRY_MALWARE_SWEEP_SUCCESS` and the number of matches, or with
`REGISTRY_MALWARE_SWEEP_FAILED` and a non-zero exit status when it could not read the whole
registry or get a complete answer from OSV. A failed sweep still reports the matches OSV had
confirmed. Set `MCP_REGISTRY_MALWARE_SWEEP_REGISTRY_URL` to sweep another registry.

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
