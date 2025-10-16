# Server JSON Schema Changelog

Changes to the server.json schema and format.

## 2025-10-11

### Changed

#### Package Format Enhancements ([#634](https://github.com/modelcontextprotocol/registry/pull/634))

The `Package` schema has been refactored to better support different package types with dedicated handling per registry type.

**Key Changes:**

- **`version` field is now optional** - Previously required for all packages, now only used by npm, pypi, and nuget. OCI packages include version in the identifier (e.g., `ghcr.io/owner/repo:v1.0.0`), and MCPB packages use direct download URLs.

- **Enhanced documentation** - Added detailed comments explaining which fields are relevant for each `registryType`:
  - **NPM/PyPI/NuGet**: Use `registryType`, `identifier` (package name), `version`, optional `registryBaseUrl`
  - **OCI**: Use `registryType`, `identifier` (full image reference with tag)
  - **MCPB**: Use `registryType`, `identifier` (download URL), `fileSha256` (required)

- **Field clarifications**:
  - `identifier`: Now clearly documented as package name for registries, full image reference for OCI, or download URL for MCPB
  - `fileSha256`: Clarified as required for MCPB packages and optional for other types
  - `registryBaseUrl`: Clarified as used by npm/pypi/nuget but not by oci/mcpb

**Migration:**

Publishers using OCI or MCPB packages can now omit the `version` field, as it's either embedded in the identifier (OCI) or not applicable (MCPB direct downloads). Publishers using npm, pypi, or nuget should continue to provide the `version` field as before.

**Example - OCI Package (version in identifier):**
```json
{
  "packages": [{
    "registryType": "oci",
    "identifier": "ghcr.io/modelcontextprotocol/server-example:v1.2.3",
    "transport": {
      "type": "stdio"
    }
  }]
}
```

**Example - MCPB Package (no version field):**
```json
{
  "packages": [{
    "registryType": "mcpb",
    "identifier": "https://github.com/example/releases/download/v1.0.0/package.mcpb",
    "fileSha256": "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
    "transport": {
      "type": "stdio"
    }
  }]
}
```

### Schema Version
- Schema version: `2025-09-29` → `2025-10-11`

## 2025-09-29

### ⚠️ BREAKING CHANGES

#### Schema Simplification

Removed registry-managed fields from publisher-controlled server.json schema.

**Removed fields:**
- `status` field from Server object (now managed by registry in API responses)
- `io.modelcontextprotocol.registry/official` from `_meta` (read-only, added by registry)

**Migration:**
Publishers should remove these fields from their `server.json` files. The registry will manage server status and official metadata separately.

### Changed
- Schema version: `2025-09-16` → `2025-09-29`

## 2025-09-16

### ⚠️ BREAKING CHANGES

#### Field Names: snake_case → camelCase ([#428](https://github.com/modelcontextprotocol/registry/issues/428))

All JSON field names standardized to camelCase. **All existing `server.json` files must be updated.**

**Changed fields:**
- `registry_type` → `registryType`
- `registry_base_url` → `registryBaseUrl`
- `file_sha256` → `fileSha256`
- `runtime_hint` → `runtimeHint`
- `runtime_arguments` → `runtimeArguments`
- `package_arguments` → `packageArguments`
- `environment_variables` → `environmentVariables`
- `is_required` → `isRequired`
- `is_secret` → `isSecret`
- `value_hint` → `valueHint`
- `is_repeated` → `isRepeated`
- `website_url` → `websiteUrl`

#### Migration Examples

**Package Configuration:**
```json
// OLD - Will be rejected
{
  "packages": [{
    "registry_type": "npm",
    "registry_base_url": "https://registry.npmjs.org",
    "file_sha256": "abc123...",
    "runtime_hint": "node",
    "runtime_arguments": [...],
    "package_arguments": [...],
    "environment_variables": [...]
  }]
}

// NEW - Required format
{
  "packages": [{
    "registryType": "npm",
    "registryBaseUrl": "https://registry.npmjs.org",
    "fileSha256": "abc123...",
    "runtimeHint": "node",
    "runtimeArguments": [...],
    "packageArguments": [...],
    "environmentVariables": [...]
  }]
}
```

**Arguments Configuration:**
```json
// OLD - Will be rejected
{
  "runtime_arguments": [
    {
      "name": "port",
      "is_required": true,
      "is_repeated": false,
      "value_hint": "8080"
    }
  ]
}

// NEW - Required format
{
  "runtimeArguments": [
    {
      "name": "port",
      "isRequired": true,
      "isRepeated": false,
      "valueHint": "8080"
    }
  ]
}
```

**Environment Variables:**
```json
// OLD - Will be rejected
{
  "environment_variables": [
    {
      "name": "API_KEY",
      "is_required": true,
      "is_secret": true
    }
  ]
}

// NEW - Required format
{
  "environmentVariables": [
    {
      "name": "API_KEY",
      "isRequired": true,
      "isSecret": true
    }
  ]
}
```

#### Migration Checklist for Publishers

- [ ] Update your `server.json` files to use camelCase field names
- [ ] Test server publishing with new CLI version
- [ ] Update any automation scripts that reference old field names
- [ ] Update documentation referencing old field names

#### Updated Schema Reference

🔗 **Current schema**: https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json

### Changed
- Schema version: `2025-07-09` → `2025-09-16`

## 2025-07-09

Initial release of the server.json schema.