# Server Versioning Guide

This document describes the versioning approach for MCP servers published to the registry.

## Overview

The MCP Registry supports flexible versioning while encouraging semantic versioning best practices. The registry attempts to parse versions as semantic versions for ordering and comparison, but falls back gracefully for non-semantic versions.

## Version Requirements

1. **Version String**: `version` MUST be a string up to 255 characters
2. **Uniqueness**: Each version for a given server name must be unique
3. **Immutability**: Once published, version metadata cannot be changed

## Best Practices

### 1. Use Semantic Versioning
Server authors SHOULD use [semantic versions](https://semver.org/) following the `MAJOR.MINOR.PATCH` format:

```json
{
  "version": "1.2.3"
}
```

### 2. Align with Package Versions
Server authors SHOULD use versions aligned with their underlying packages to reduce confusion:

```json
{
  "version": "1.2.3",
  "packages": [{
    "registry_name": "npm",
    "name": "@myorg/my-server",
    "version": "1.2.3"
  }]
}
```

### 3. Multiple Registry Versions
If server authors expect to have multiple registry versions for the same package version, they SHOULD follow the semantic version spec using the prerelease label:

```json
{
  "version": "1.2.3-1",
  "packages": [{
    "registry_name": "npm", 
    "name": "@myorg/my-server",
    "version": "1.2.3"
  }]
}
```

**Note**: According to semantic versioning, `1.2.3-1` is considered lower than `1.2.3`, so if you expect to need a `1.2.3-1`, you should publish that before `1.2.3`.

## Version Ordering and "Latest" Determination

### For Semantic Versions
The registry attempts to parse versions as semantic versions. If successful, it uses semantic version comparison rules to determine:
- Version ordering in lists
- Which version is marked as `isLatest`

### For Non-Semantic Versions
If version parsing as semantic version fails:
- The registry will always mark the version as latest (overriding any previous version)
- Clients should fall back to using publish timestamp for ordering

**Important Note**: This behavior means that for servers with mixed semantic and non-semantic versions, the `isLatest` flag may not align with the total ordering. A non-semantic version published after semantic versions will be marked as latest, even if semantic versions are considered "higher" in the ordering.

## Implementation Details

### Registry Behavior
1. **Validation**: Versions are validated for uniqueness within a server name
2. **Parsing**: The registry attempts to parse each version as semantic version
3. **Comparison**: Uses semantic version rules when possible, falls back to timestamp
4. **Latest Flag**: The `isLatest` field is set based on the comparison results

### Client Recommendations  
Registry clients SHOULD:
1. Attempt to interpret versions as semantic versions when possible
2. Use the following ordering rules:
   - If one version is marked as isLatest: it is later
   - If both versions are valid semver: use semver comparison
   - If neither are valid semver: use publish timestamp  
   - If one is semver and one is not: semver version is considered higher

## Examples

### Valid Semantic Versions
```javascript
"1.0.0"          // Basic semantic version
"2.1.3-alpha"    // Prerelease version  
"1.0.0-beta.1"   // Prerelease with numeric suffix
"3.0.0-rc.2"     // Release candidate
```

### Non-Semantic Versions (Allowed)
```javascript
"v1.0"           // Version with prefix
"2021.03.15"     // Date-based versioning
```

### Not Allowed: Version Ranges
The registry requires specific versions for both the top-level `version` and any `packages[].version`. Version ranges or wildcard versions are rejected during publish, including but not limited to:

```javascript
"^1.2.3"
"~1.2.3"
">=1.2.3"
"<=1.2.3"
">1.2.3"
"<1.2.3"
"1.x"
"1.2.*"
"1 - 2"
"1.2 || 1.3"
```

### Alignment Examples
```json
{
  "version": "1.2.3-1",
  "packages": [{
    "registry_name": "npm",
    "name": "@myorg/k8s-server", 
    "version": "1.2.3"
  }]
}
```

## Migration Path

Existing servers with non-semantic versions will continue to work without changes. However, to benefit from proper version ordering, server maintainers are encouraged to:

1. Adopt semantic versioning for new releases
2. Consider the ordering implications when transitioning from non-semantic to semantic versions
3. Use prerelease labels for registry-specific versioning needs

## Common Scenarios

### Standard Releases
**Package and server versions aligned**:
```json
{
  "version": "1.2.3",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "@myorg/server",
      "version": "1.2.3"
    }
  ]
}
```

### Registry-Only Updates
**Server metadata changes without package updates**:
```json
{
  "version": "1.2.3-1",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "@myorg/server", 
      "version": "1.2.3"
    }
  ]
}
```

Note: `1.2.3-1` is considered lower than `1.2.3` in semver ordering. Publish prerelease versions first.

### Remote Server Versioning
**Remote servers without package dependencies**:
```json
{
  "version": "2.1.0",
  "remotes": [
    {
      "transport_type": "sse",
      "url": "https://api.myservice.com/mcp/v2.1"
    }
  ]
}
```

Version strategy options:
- **API versioning**: Match your service API version
- **Semantic versioning**: Standard semver for feature changes  
- **Date-based**: `2024.03.15` for regular releases

### Multi-Package Versioning
**Different package versions**:
```json
{
  "version": "1.3.0",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "@myorg/server",
      "version": "1.3.0"
    },
    {
      "registryType": "oci",
      "identifier": "myorg/server",
      "version": "1.2.5"
    }
  ]
}
```

Use server version to indicate the overall release.

## Decision Guide

**Which version strategy should I use?**

```
Do you have underlying packages?
├─ Yes: Align with package version
│  ├─ Same package version → Use package version
│  └─ Different package versions → Use highest + indicate relationship
└─ No (remote server): Choose versioning scheme
   ├─ API versioning → Match service API version
   ├─ Feature-based → Use semantic versioning
   └─ Regular releases → Consider date-based versioning
```

## Future Considerations

This versioning approach is designed to be compatible with potential future changes to the MCP specification's `Implementation.version` field. Any SHOULD requirements introduced here may be proposed as updates to the specification through the SEP (Specification Enhancement Proposal) process.