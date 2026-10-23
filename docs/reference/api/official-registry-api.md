# Official MCP Registry API

This document describes the API for the official MCP Registry hosted at `registry.modelcontextprotocol.io`.

This API is based on the [generic registry API](./generic-registry-api.md) with additional endpoints and authentication. For publishing servers using the API, see the [publishing guide](../../modelcontextprotocol-io/quickstart.mdx).

## Base URLs

- **Production**: `https://registry.modelcontextprotocol.io`
- **Staging**: `https://staging.registry.modelcontextprotocol.io`

## Interactive Documentation

- **[Live API Docs](https://registry.modelcontextprotocol.io/docs)** - Stoplight elements with try-it-now functionality
- **[OpenAPI Spec](https://registry.modelcontextprotocol.io/openapi.yaml)** - Complete machine-readable specification

## Extensions

The official registry implements the [Generic Registry API](./generic-registry-api.md) with the following specific configurations and extensions:

### Authentication

Publishing requires namespace-based authentication:

- **GitHub OAuth** - For `io.github.*` namespaces
- **GitHub OIDC** - For publishing from GitHub Actions  
- **DNS verification** - For domain-based namespaces (`com.example.*`)
- **HTTP verification** - For domain-based namespaces (`com.example.*`)

See [Publisher Commands](../cli/commands.md) for authentication setup.

### Package Validation

The official registry enforces additional [package validation requirements](../server-json/official-registry-requirements.md) when publishing.

### Server List Filtering

The official registry extends the `GET /v0.1/servers` endpoint with additional query parameters for improved discovery and synchronization:

- `updated_since` - Filter servers updated after RFC3339 timestamp (e.g., `2025-08-07T13:15:04.280Z`)
- `search` - Case-insensitive substring search on server names (e.g., `filesystem`)
    - This is intentionally simple. For more advanced searching and filtering, use a subregistry.
- `version` - Filter by version (currently supports `latest` for latest versions only)
- `include_deleted` - Include deleted servers in results (default: `false`, but automatically `true` when `updated_since` is provided for incremental sync)

These extensions enable efficient incremental synchronization for downstream registries and improved server discovery. Parameters can be combined and work with standard cursor-based pagination.

Example: `GET /v0.1/servers?search=filesystem&updated_since=2025-08-01T00:00:00Z&version=latest`

### Server Detail

The `GET /v0.1/servers/{serverName}/versions/{version}` endpoint returns detailed information about a specific server version.

**Path parameters:**
- `serverName` - URL-encoded server name (e.g., `io.github.user%2Fmy-server`)
- `version` - Server version or `latest` for the most recent version

**Query parameters:**
- `include_deleted` - Include deleted servers in results (default: `false`)

### Server Version History

The `GET /v0.1/servers/{serverName}/versions` endpoint returns all versions of a server.

**Path parameters:**
- `serverName` - URL-encoded server name (e.g., `io.github.user%2Fmy-server`)

**Query parameters:**
- `include_deleted` - Include deleted servers in results (default: `false`)

### Additional endpoints

#### Auth endpoints
- POST `/v0.1/auth/dns` - Exchange signed DNS challenge for auth token
- POST `/v0.1/auth/http` - Exchange signed HTTP challenge for auth token
- POST `/v0.1/auth/github-at` - Exchange GitHub access token for auth token
- POST `/v0.1/auth/github-oidc` - Exchange GitHub OIDC token for auth token
- POST `/v0.1/auth/oidc` - Exchange Google OIDC token for auth token (for admins)

##### Publishing without `mcp-publisher`

[Publisher Commands](../cli/commands.md) covers the CLI, which is the recommended path. If you
publish from a script, a CI step, or a language without the Go binary, the request and response
shapes for the domain-proof flow are below.

Exchange a signed timestamp for a token. The signature is over the RFC3339 timestamp string,
and is **hex-encoded, not base64**:

```bash
curl -s -X POST https://registry.modelcontextprotocol.io/v0.1/auth/http \
  -H 'content-type: application/json' \
  -d '{
        "domain": "example.com",
        "timestamp": "2026-08-16T15:53:00Z",
        "signed_timestamp": "<hex signature over the timestamp string>"
      }'
```

The response field is **`registry_token`**:

```json
{ "registry_token": "<jwt>", "expires_at": 1786893600 }
```

The token is short-lived, so authenticate and publish in the same run.

Then publish. The body is the [server.json](../server-json/generic-server-json.md) object
**flat at the top level** — not wrapped in `{"server": {...}}` — and `$schema` is required:

```bash
curl -s -X POST https://registry.modelcontextprotocol.io/v0.1/publish \
  -H "authorization: Bearer ${REGISTRY_TOKEN}" \
  -H 'content-type: application/json' \
  -d '{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "com.example/my-server",
        "description": "Does a useful thing.",
        "version": "1.0.0",
        "repository": { "url": "https://github.com/example/my-server", "source": "github" },
        "remotes": [{ "type": "streamable-http", "url": "https://example.com/mcp" }]
      }'
```

Three responses worth recognising:

- Wrapping the body in `{"server": {...}}` returns `422` with
  `expected required property $schema to be present`. The validator is describing the outer
  object, so the message names fields that are plainly present one level down.
- Reading the auth response's `token` instead of `registry_token` sends `Bearer undefined`,
  which returns `401` with `token is malformed: token contains an invalid number of segments`
  — an error that reads like a signing fault and is not one.
- Re-publishing an existing `version` returns `400` with
  `invalid version: cannot publish duplicate version`. There is no upsert; bump the version.

If you are unsure whether a server was already published, query the full name rather than
searching for part of it — `?search=` matches loosely and can rank other servers above yours.

#### Status endpoints

##### Update Single Version Status

`PATCH /v0.1/servers/{serverName}/versions/{version}/status` - Update status of a specific server version.

**Path parameters:**
- `serverName` - URL-encoded server name (e.g., `io.github.user%2Fmy-server`)
- `version` - Server version to update

**Request body:**
- `status` (required) - New status: `active`, `deprecated`, or `deleted`
- `statusMessage` (optional) - Message explaining the status change (max 500 characters, not allowed when status is `active`)

##### Update All Versions Status

`PATCH /v0.1/servers/{serverName}/status` - Update status of all versions of a server in a single transaction.

**Path parameters:**
- `serverName` - URL-encoded server name (e.g., `io.github.user%2Fmy-server`)

**Request body:**
- `status` (required) - New status: `active`, `deprecated`, or `deleted`
- `statusMessage` (optional) - Message explaining the status change (max 500 characters, not allowed when status is `active`)

**Status values:**
- `active` - Server is active and visible in default listings
- `deprecated` - Server is deprecated but still visible with a warning message
- `deleted` - Server is hidden from default listings (use `include_deleted=true` to show)

**Authentication:** Requires `publish` or `edit` permission for the server namespace.

#### Validation endpoint
- POST `/v0.1/validate` - Validate a `server.json` without publishing it. Used by `mcp-publisher validate`.

#### Service endpoints
- GET `/v0.1/ping` - Liveness check
- GET `/v0.1/version` - Registry version information

#### Admin endpoints
- GET `/metrics` - Prometheus metrics endpoint
- GET `/v0.1/health` - Basic health check endpoint
- PUT `/v0.1/servers/{serverName}/versions/{version}` - Edit specific server version

Note that `/v0.1/validate`, `/v0.1/ping`, `/v0.1/version` and `/v0.1/health` are not described in
[openapi.yaml](./openapi.yaml), which covers the server and publish endpoints only.
