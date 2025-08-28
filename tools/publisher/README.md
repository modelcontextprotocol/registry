# MCP Registry Publisher Tool

The MCP Registry Publisher Tool helps you publish Model Context Protocol (MCP) servers to the official registry. It follows a simple, npm-like workflow that's familiar to most developers.

## Installation

```bash
# Build for current platform
./build.sh

# Build for all supported platforms (optional)
./build.sh --all
```

The compiled binary will be placed in the `bin` directory.

## Quick Start

```bash
# 1. Create a template server.json file
mcp-publisher init

# 2. Edit server.json with your server details

# 3. Authenticate with the registry (one-time setup)
mcp-publisher login <method>

# 4. Publish your server
mcp-publisher publish
```

## Commands

### `mcp-publisher init`

Creates a template `server.json` file with smart defaults. This command automatically detects values from your environment when possible.

```bash
mcp-publisher init
```

Auto-detection sources:
- **Git remote origin** for repository URL and server name
- **package.json** for npm packages (name, description)
- **pyproject.toml** for Python packages
- **Current directory** for fallback naming

The generated file includes placeholder values that you should update before publishing.

### `mcp-publisher login <method>`

Authenticates with the MCP registry. You'll only need to do this once - the tool saves your credentials locally.

```bash
# GitHub interactive authentication
mcp-publisher login github

# GitHub Actions OIDC (for CI/CD)
mcp-publisher login github-oidc

# DNS-based authentication
mcp-publisher login dns --domain example.com --private-key abc123...

# HTTP-based authentication  
mcp-publisher login http --domain example.com --private-key abc123...
```

**Authentication Methods:**

- **`github`**: Interactive GitHub OAuth flow. Opens browser for authentication.
- **`github-oidc`**: For GitHub Actions. Uses OIDC tokens automatically.
- **`dns`**: Domain-based auth using DNS TXT records. Requires `--domain` and `--private-key`.
- **`http`**: Domain-based auth using HTTPS endpoint. Requires `--domain` and `--private-key`.

### `mcp-publisher publish`

Publishes your `server.json` file to the registry. You must be logged in first.

```bash
mcp-publisher publish
```

This command:
- Validates your `server.json` file
- Checks your authentication
- Uploads the server configuration to the registry

### `mcp-publisher logout`

Clears your saved authentication credentials.

```bash
mcp-publisher logout
```

## GitHub Actions

For automated publishing from GitHub Actions:

```yaml
name: Publish to MCP Registry
on:
  push:
    tags: ['v*']

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # Required for OIDC
      contents: read
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup MCP Publisher
        run: |
          curl -LO https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher-linux-amd64
          chmod +x mcp-publisher-linux-amd64
          
      - name: Login to Registry
        run: ./mcp-publisher-linux-amd64 login github-oidc
        
      - name: Publish to Registry
        run: ./mcp-publisher-linux-amd64 publish
```

The `login github-oidc` command uses GitHub's OIDC tokens for authentication in CI environments.

## Examples

### Publishing an NPM-based server

```bash
# Create template server.json (auto-detects from package.json)
# Then edit server.json to add your specific details
mcp-publisher init

# Login (first time only)
mcp-publisher login github

# Publish
mcp-publisher publish
```

### Publishing a Python server

```bash
# Create template server.json (auto-detects from pyproject.toml)
# Then edit server.json to add your specific details
mcp-publisher init

# Publish (assuming already logged in)
mcp-publisher publish
```

### Publishing with DNS authentication

```bash
# Setup DNS TXT record first (see DNS Authentication section below)

# Login with DNS
mcp-publisher login dns --domain example.com --private-key YOUR_PRIVATE_KEY

# Publish
mcp-publisher publish
```

## Authentication Details

### DNS Authentication

For domain-based authentication using DNS:

1. Generate an Ed25519 keypair:
   ```bash
   openssl genpkey -algorithm Ed25519 -out /tmp/key.pem && \
   echo "DNS TXT Record:" && \
   echo "  v=MCPv1; k=ed25519; p=$(openssl pkey -in /tmp/key.pem -pubout -outform DER | tail -c 32 | base64)" && \
   echo "" && \
   echo "Private key for login:" && \
   echo "  $(openssl pkey -in /tmp/key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n')" && \
   rm /tmp/key.pem
   ```

2. Add the TXT record to your domain's DNS

3. Login with:
   ```bash
   mcp-publisher login dns --domain example.com --private-key YOUR_PRIVATE_KEY
   ```

This grants publishing rights for `example.com/*` and `*.example.com/*` namespaces.

### HTTP Authentication

For HTTP-based authentication:

1. Generate an Ed25519 keypair (same as DNS)

2. Host the public key at `https://yoursite.com/.well-known/mcp-registry-auth`:
   ```
   v=MCPv1; k=ed25519; p=YOUR_PUBLIC_KEY_BASE64
   ```

3. Login with:
   ```bash
   mcp-publisher login http --domain example.com --private-key YOUR_PRIVATE_KEY
   ```

This grants publishing rights for the `example.com/*` namespace.

## Troubleshooting

**"Not authenticated" error**: Run `mcp-publisher login <method>` to authenticate (e.g., `mcp-publisher login github`).

**"Invalid server.json" error**: Ensure your `server.json` file is valid. You can recreate it with `mcp-publisher init`.

**GitHub Actions failing**: Ensure your workflow has `id-token: write` permission and you're using `mcp-publisher login github-oidc`.
