# Package Canonical Reference Format

## Overview

This document describes the canonical reference format for each package type in the MCP registry. The goal is to align with industry standards while using a single `Package` struct that can represent all types.

## Design Principles

1. **Industry alignment**: Follow each ecosystem's standard conventions
2. **Single struct**: Use one `Package` type with a `type` discriminator field
3. **Validation-based**: Rely on schema validation + Go validation rather than compile-time type safety
4. **Database-friendly**: Design for future migration from JSONB to individual columns

## Package Types and Their Canonical Formats

### NPM Packages (`type: "npm"`)

**Industry Standard**: Separate name + version
- CLI: `npm install @scope/package@1.0.0`
- package.json: `"@scope/package": "1.0.0"`

**Our Format**:
```json
{
  "type": "npm",
  "identifier": "@modelcontextprotocol/server-filesystem",
  "version": "1.0.2",
  "registryBaseUrl": "https://registry.npmjs.org",
  "transport": { "type": "stdio" }
}
```

**Fields**:
- `identifier`: NPM package name (with or without @scope)
- `version`: Specific version (no ranges)
- `registryBaseUrl`: Optional, defaults to https://registry.npmjs.org

---

### PyPI Packages (`type: "pypi"`)

**Industry Standard**: Combined name==version
- CLI: `pip install package==1.0.0`
- requirements.txt: `package==1.0.0`

**Our Format**:
```json
{
  "type": "pypi",
  "identifier": "mcp-server-time",
  "version": "1.0.2",
  "registryBaseUrl": "https://pypi.org",
  "transport": { "type": "stdio" }
}
```

**Fields**:
- `identifier`: PyPI package name
- `version`: Specific version
- `registryBaseUrl`: Optional, defaults to https://pypi.org

---

### NuGet Packages (`type: "nuget"`)

**Industry Standard**: Separate ID + version
- CLI: `dotnet add package PackageId --version 1.0.0`
- .csproj: `<PackageReference Include="PackageId" Version="1.0.0" />`

**Our Format**:
```json
{
  "type": "nuget",
  "identifier": "ModelContextProtocol.Server",
  "version": "1.0.2",
  "registryBaseUrl": "https://api.nuget.org/v3/index.json",
  "transport": { "type": "stdio" }
}
```

**Fields**:
- `identifier`: NuGet package ID
- `version`: Specific version
- `registryBaseUrl`: Optional, defaults to https://api.nuget.org/v3/index.json

---

### OCI Container Images (`type: "oci"`)

**Industry Standard**: Single canonical reference
- CLI: `docker pull ghcr.io/owner/repo:v1.0.0`
- Also supports: `ghcr.io/owner/repo@sha256:abc...`
- Format: `[registry/]namespace/image[:tag][@digest]`

**Our Format**:
```json
{
  "type": "oci",
  "identifier": "ghcr.io/modelcontextprotocol/server-example:v1.0.0",
  "transport": { "type": "stdio" }
}
```

**Fields**:
- `identifier`: Full OCI image reference including registry, namespace, image, and tag/digest
- `version`: NOT USED (version info is in the identifier tag/digest)
- `registryBaseUrl`: NOT USED (registry is part of the identifier)

**Valid identifier formats**:
- `ghcr.io/owner/repo:v1.0.0` (registry + tag)
- `ghcr.io/owner/repo@sha256:abc...` (registry + digest)
- `ghcr.io/owner/repo:v1.0.0@sha256:abc...` (registry + tag + digest)
- `owner/repo:latest` (defaults to docker.io registry)
- `library/postgres:16` (defaults to docker.io registry, official image)

---

### MCPB Binary Packages (`type: "mcpb"`)

**Industry Standard**: Direct download URL
- Just a URL to the binary file

**Our Format**:
```json
{
  "type": "mcpb",
  "identifier": "https://github.com/owner/repo/releases/download/v1.0.0/server-macos-arm64.mcpb",
  "fileSha256": "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
  "transport": { "type": "stdio" }
}
```

**Fields**:
- `identifier`: Full HTTPS URL to the binary file (must be GitHub or GitLab release)
- `version`: NOT USED (version info is in the URL)
- `registryBaseUrl`: NOT USED (inferred from URL hostname)
- `fileSha256`: REQUIRED for integrity verification

---

## Single Package Struct

```go
type Package struct {
    // Discriminator
    Type string `json:"type"` // "npm", "pypi", "nuget", "oci", "mcpb"

    // Universal identifier (used by all types)
    Identifier string `json:"identifier"`

    // Optional version (used by npm, pypi, nuget; NOT used by oci, mcpb)
    Version string `json:"version,omitempty"`

    // Optional registry URL (used by npm, pypi, nuget; NOT used by oci, mcpb)
    RegistryBaseURL string `json:"registryBaseUrl,omitempty"`

    // Optional file hash (REQUIRED for mcpb, optional for others)
    FileSHA256 string `json:"fileSha256,omitempty"`

    // Common fields (all types)
    Transport            Transport       `json:"transport"`
    RuntimeHint          string          `json:"runtimeHint,omitempty"`
    RuntimeArguments     []Argument      `json:"runtimeArguments,omitempty"`
    PackageArguments     []Argument      `json:"packageArguments,omitempty"`
    EnvironmentVariables []KeyValueInput `json:"environmentVariables,omitempty"`
}
```

## Validation Rules

### Type-specific field requirements:

**NPM, PyPI, NuGet**:
- REQUIRED: `type`, `identifier`, `version`, `transport`
- OPTIONAL: `registryBaseUrl` (has defaults)

**OCI**:
- REQUIRED: `type`, `identifier`, `transport`
- UNUSED: `version`, `registryBaseUrl` (must be empty or omitted)

**MCPB**:
- REQUIRED: `type`, `identifier`, `fileSha256`, `transport`
- UNUSED: `version`, `registryBaseUrl` (must be empty or omitted, though registryBaseUrl can be inferred)

## Migration Path

1. **Phase 1**: Update OpenAPI schema with discriminated union using `oneOf`
2. **Phase 2**: Keep existing `Package` struct, ensure it matches schema
3. **Phase 3**: Update validators to handle canonical references
4. **Phase 4**: Create database migration to transform existing JSONB data
5. **Phase 5**: Update API layer to use new format
6. **Phase 6**: Update publisher CLI to generate new format
7. **Phase 7**: Update seed data and tests
8. **Phase 8**: Update documentation

## Examples

See the OpenAPI schema examples for complete working examples of each package type.