# Registry API Changelog

Changes to the REST API endpoints and responses.

## Unreleased

### Added

#### API Versioning - v0.1 Introduction

Introduced `/v0.1/` as a stable API version while `/v0/` continues as the development version.

**New version paths:**
- All `/v0/` endpoints are now also available at `/v0.1/`
- Both versions currently share identical behavior
- `/v0/` will continue to evolve with additive changes (new optional fields, new endpoints)
- `/v0.1/` will remain stable with only additive, backward-compatible changes
- Both versions will be maintained until a future v1.0 release

**Migration guidance:**
- Production applications should consider using `/v0.1/` for stability
- Development and testing can continue using `/v0/` for latest features
- No immediate action required - `/v0/` remains fully supported

### ⚠️ BREAKING CHANGES

#### Endpoint Simplification

Removed redundant endpoint to simplify API surface and reduce implementation burden for subregistries.

**Removed endpoints:**
- `GET /v0/servers/{serverName}` - Use `GET /v0/servers/{serverName}/versions/latest` instead

## 2025-09-29

### ⚠️ BREAKING CHANGES

#### API Endpoint Restructuring

API endpoints updated to use server names instead of server IDs for better usability.

**Changed endpoints:**
- `GET /v0/servers/{server_id}` → `GET /v0/servers/{serverName}`
- `GET /v0/servers/{server_id}/versions` → `GET /v0/servers/{serverName}/versions`

**New endpoints:**
- `GET /v0/servers/{serverName}/versions/{version}` - Get specific server version
- `PUT /v0/servers/{serverName}/versions/{version}` - Edit server version (admin only)

**Response format changes:**
- Introduced `ServerResponse` schema separating server data from registry metadata
- Moved `status` field from server data to `_meta.io.modelcontextprotocol.registry/official`
- Removed `io.modelcontextprotocol.registry/official` metadata from `ServerDetail` schema

### Changed
- OpenAPI spec version: `2025-09-16` → `2025-09-29`

## 2025-09-16

### ⚠️ BREAKING CHANGES

#### Server ID Endpoints ([#396](https://github.com/modelcontextprotocol/registry/issues/396))

API endpoints updated for consistent server identification across versions.

**Problem:** Each server version had a unique ID, preventing version history tracking and server renaming.

**Solution:** Introduced consistent server identification across versions.

**Changed endpoints:**
- `GET /v0/servers/{id}` → `GET /v0/servers/{server_id}`

**New endpoints:**
- `GET /v0/servers/{server_id}/versions` - List all versions of a server
- `GET /v0/servers/{server_id}?version=1.0.0` - Get specific version

**Changed response metadata:**
- `_meta.*.id` → `_meta.*.serverId`
- Added: `_meta.*.versionId`

#### Migration Examples

**Old Structure:**
```json
{
  "_meta": {
    "io.modelcontextprotocol.registry/official": {
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "published_at": "2024-01-01T00:00:00Z",
      "is_latest": true
    }
  }
}
```

**New Structure:**
```json
{
  "_meta": {
    "io.modelcontextprotocol.registry/official": {
      "serverId": "550e8400-e29b-41d4-a716-446655440000",
      "versionId": "773f9b2e-1a47-4c8d-b5e6-2f8d9c4a7b3e",
      "published_at": "2024-01-01T00:00:00Z",
      "is_latest": true
    }
  }
}
```

#### Migration Checklist for API Consumers

- [ ] Update API endpoint URLs from `/v0/servers/{id}` to `/v0/servers/{server_id}`
- [ ] Update code reading registry metadata from `id` to `serverId`/`versionId`
- [ ] Add support for new `/v0/servers/{server_id}/versions` endpoint if needed
- [ ] Update JSON parsing to expect camelCase field names
- [ ] Test with new API responses

### Changed
- OpenAPI spec version: `2025-07-09` → `2025-09-16`

## 2025-07-09

Initial release of the Registry API.