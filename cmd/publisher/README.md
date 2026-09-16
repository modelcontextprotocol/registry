# MCP Publisher Tool - Development

CLI tool for publishing MCP servers to the registry.

> These docs are for contributors. See the [Publisher User Guide](../../docs/modelcontextprotocol-io/quickstart.mdx) for end-user documentation.

## Quick Development Setup

```bash
# Build the tool
make publisher

# Test locally 
make dev-compose  # Start local registry
./bin/mcp-publisher init
./bin/mcp-publisher login none --registry=http://localhost:8080
./bin/mcp-publisher publish
```

`publish` takes the registry URL from the token saved by `login`, so it accepts no `--registry`
flag — passing one would be read as the `server.json` path.

## Architecture

### Commands
- **`init`** - Generate server.json templates with auto-detection
- **`login`** - Handle authentication (github, dns, http, none)
- **`publish`** - Validate and upload servers to registry
- **`status`** - Update server lifecycle status (active, deprecated, deleted)
- **`validate`** - Validate server.json without publishing
- **`logout`** - Clear stored credentials

### Authentication Providers
- **`github`** - Interactive OAuth flow
- **`github-oidc`** - CI/CD with GitHub Actions
- **`dns`** - Domain verification via DNS TXT records
- **`http`** - Domain verification via HTTPS endpoints
- **`none`** - No auth (testing only)

### Signing Providers
Optional: enables `dns` and `http` methods to sign out-of-process without direct access to the private key.

- **`google-kms`** - Google KMS signing
- **`azure-key-vault`** - Azure Key Vault signing

## Key Files

- **`main.go`** - CLI setup and command routing
- **`commands/`** - Command implementations with auto-detection logic
- **`auth/`** - Authentication provider implementations
