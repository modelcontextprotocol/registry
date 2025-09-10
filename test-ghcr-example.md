# Testing GHCR Support

This document explains how to test the GitHub Container Registry (GHCR) support implementation.

## Quick Tests

### 1. Run Unit Tests
```bash
# Test the new GHCR integration
go test -v ./internal/validators/registries/... -run TestValidateOCI_GHCR_Integration

# Test registry support validation  
go test -v ./internal/validators/registries/... -run TestValidateOCI_SupportedRegistries
```

### 2. Manual Validation Test

Create a test server.json file:
```bash
cat > test-ghcr-server.json << EOF
{
  "\$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
  "name": "io.github.yourname/test-server",
  "description": "Test GHCR server",
  "version": "1.0.0",
  "packages": [
    {
      "registry_type": "oci",
      "registry_base_url": "https://ghcr.io",
      "identifier": "github/github-mcp-server", 
      "version": "main"
    }
  ]
}
EOF
```

### 3. Test with Publisher CLI
```bash
# Build the publisher
make publisher

# Test validation (will show GHCR is supported)
./bin/mcp-publisher validate test-ghcr-server.json
```

## Creating Your Own GHCR MCP Server

To create a testable GHCR MCP server:

### 1. Create Dockerfile with MCP Label
```dockerfile
FROM node:18-alpine
LABEL io.modelcontextprotocol.server.name="io.github.yourusername/your-server-name"
# ... rest of your MCP server setup
```

### 2. Build and Push to GHCR
```bash
# Build and tag
docker build -t ghcr.io/yourusername/your-mcp-server:latest .

# Login to GHCR  
echo $GITHUB_TOKEN | docker login ghcr.io -u yourusername --password-stdin

# Push
docker push ghcr.io/yourusername/your-mcp-server:latest

# Make the package public in GitHub UI
# Go to: github.com/users/yourusername/packages/container/your-mcp-server/settings
```

### 3. Test Your Image
```bash
# Test the validation
go run . validate-image ghcr.io yourusername/your-mcp-server latest io.github.yourusername/your-server-name
```

## Expected Results

✅ **GHCR URL accepted**: `https://ghcr.io` should be accepted as valid registry  
✅ **Anonymous access**: No authentication errors when accessing public images  
✅ **Label validation**: MCP server name label validation should work  
✅ **Error handling**: Proper error messages for private/missing images  

## Known Limitations

- **Private repos**: GitHub's `github/github-mcp-server` appears to be private (401 errors)
- **Rate limiting**: Same rate limiting protections as Docker Hub  
- **Authentication**: Currently only supports anonymous access (public images only)

## Cleanup

```bash
rm -f test-ghcr-server.json
```