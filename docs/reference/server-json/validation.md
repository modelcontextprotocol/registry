# Package Validation Requirements

Registry validation rules for package ownership verification.

## Overview

All packages referenced in `server.json` must include ownership verification metadata to prevent misattribution. The registry validates that publishers actually control the packages they reference.

## NPM Packages

### Requirement
Add `mcpName` field to `package.json`:

```json
{
  "name": "your-npm-package",
  "version": "1.0.0", 
  "mcpName": "com.example/server-name"
}
```

### Validation Process
1. Registry fetches `https://registry.npmjs.org/{package-name}`
2. Checks that `mcpName` field matches server name exactly
3. Fails if field is missing or doesn't match

### Supported Registries
- **npmjs.org only** (`https://registry.npmjs.org`)

## PyPI Packages

### Requirement  
Include MCP name in package README:

```markdown
# My Package

This is a great MCP server.

mcp-name: com.example/server-name

## Installation
...
```

### Validation Process
1. Registry fetches `https://pypi.org/pypi/{package}/json`
2. Searches README content for `mcp-name: {server-name}`
3. Passes if exact match found

### Supported Registries
- **pypi.org only** (`https://pypi.org`)

## NuGet Packages

### Requirement
Include MCP name in package README:

```markdown
# My Package

mcp-name: com.example/server-name

This package provides...
```

### Validation Process  
1. Registry fetches `https://api.nuget.org/v3-flatcontainer/{id}/{version}/readme`
2. Searches README content for `mcp-name: {server-name}`
3. Passes if exact match found

### Supported Registries
- **nuget.org only** (`https://api.nuget.org`)

## Docker/OCI Images

### Requirement
Add label to Dockerfile:

```dockerfile
LABEL io.modelcontextprotocol.server.name="com.example/server-name"
```

### Validation Process
1. Registry authenticates with Docker Hub using public token
2. Fetches image manifest via Docker Registry v2 API  
3. Checks that `io.modelcontextprotocol.server.name` annotation matches server name
4. Fails if annotation is missing or doesn't match

### Supported Registries
- **docker.io only** (`https://docker.io`)

## MCPB Packages

### Requirement
URL must contain "mcp" substring (in filename or repository name).

**Valid examples:**
- `https://github.com/user/mcp-server/releases/download/v1.0.0/server.mcpb`
- `https://github.com/user/awesome-server/releases/download/v1.0.0/my-mcp-tool.mcpb`

**Invalid examples:**  
- `https://github.com/user/server/releases/download/v1.0.0/tool.exe`

### Validation Process
1. Registry checks URL contains "mcp" substring
2. Verifies URL is accessible and returns binary content

### Supported Hosts
- **github.com** - GitHub releases
- **gitlab.com** - GitLab releases

## Common Validation Errors

### Mismatched Names
```
Error: Package 'my-package' declares mcpName 'com.other/server' 
but server name is 'com.example/server'
```

### Missing Metadata
```
Error: Package 'my-package' missing required mcpName field
```

### Unsupported Registry
```
Error: Registry 'https://my-private-npm.com' not supported.
Supported: https://registry.npmjs.org
```

### Package Not Found
```  
Error: Package 'my-package@1.0.0' not found at registry
```

## Bypassing Validation

Validation cannot be bypassed for security reasons. If you cannot add verification metadata:

1. **Fork the package** and add metadata
2. **Contact package maintainer** to add metadata
3. **Use a different package** that supports verification
4. **Host your own registry** for private packages