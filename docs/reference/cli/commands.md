# Publisher CLI Commands Reference

Complete command reference for the `mcp-publisher` CLI tool.

See the [publishing guide](../../guides/publishing/publish-server.md) for a walkthrough of using the CLI to publish a server.

## Installation

Install via Homebrew (macOS/Linux):

```bash
$ brew install mcp-publisher
```

## Global Options

All commands support:
- `--help`, `-h` - Show command help
- `--registry` - Registry URL (default: `https://registry.modelcontextprotocol.io`)

## Commands

### `mcp-publisher init`

Generate a `server.json` template with automatic detection.

**Usage:**
```bash
mcp-publisher init [options]
```

**Behavior:**
- Creates `server.json` in current directory
- Auto-detects package managers (`package.json`, `setup.py`, etc.)
- Pre-fills fields where possible
- Prompts for missing required fields

**Example output:**
```json
{
  "name": "io.github.username/server-name",
  "description": "TODO: Add server description",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "detected-package-name",
      "version": "1.0.0"
    }
  ]
}
```

### `mcp-publisher login <method>`

Authenticate with the registry.

**Authentication Methods:**

#### GitHub Interactive
```bash
mcp-publisher login github [--registry=URL]
```
- Opens browser for GitHub OAuth flow
- Grants access to `io.github.{username}/*` and `io.github.{org}/*` namespaces

#### GitHub OIDC (CI/CD)  
```bash
mcp-publisher login github-oidc [--registry=URL]
```
- Uses GitHub Actions OIDC tokens automatically
- Requires `id-token: write` permission in workflow
- No browser interaction needed

Also see [the guide to publishing from GitHub Actions](../../guides/publishing/github-actions.md).

#### DNS Verification
```bash
mcp-publisher login dns --domain=example.com --private-key=HEX_KEY [--registry=URL]
```
- Verifies domain ownership via DNS TXT record
- Grants access to `com.example.*` namespaces
- Requires Ed25519 private key (64-character hex) or ECDSA P-384 private key (96-character hex)

**Setup:** (for Ed25519)
```bash
# Generate keypair
openssl genpkey -algorithm Ed25519 -out key.pem

# Get public key for DNS record
openssl pkey -in key.pem -pubout -outform DER | tail -c 32 | base64

# Add DNS TXT record:
# example.com. IN TXT "v=MCPv1; k=ed25519; p=PUBLIC_KEY"

# Extract private key for login
openssl pkey -in key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n'
```

**Setup:** (for ECDSA P-384)
```bash
# Generate keypair
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:secp384r1 -out key.pem

# Get public key for DNS record
openssl ec -in key.pem -text -noout -conv_form compressed | grep -A4 "pub:" | tail -n +2 | tr -d ' :\n' | xxd -r -p | base64

# Add DNS TXT record:
# example.com. IN TXT "v=MCPv1; k=ecdsap384; p=PUBLIC_KEY"

# Extract private key for login
openssl ec -in <pem path> -noout -text | grep -A4 "priv:" | tail -n +2 | tr -d ' :\n'
```

#### HTTP Verification
```bash
mcp-publisher login http --domain=example.com --private-key=HEX_KEY [--registry=URL]
```
- Verifies domain ownership via HTTPS endpoint  
- Grants access to `com.example.*` namespaces
- Requires Ed25519 private key (64-character hex) or ECDSA P-384 private key (96-character hex)

**Setup:** (for Ed25519)
```bash
# Generate keypair (same as DNS)
openssl genpkey -algorithm Ed25519 -out key.pem

# Host public key at:
# https://example.com/.well-known/mcp-registry-auth
# Content: v=MCPv1; k=ed25519; p=PUBLIC_KEY
```

**Setup:** (for ECDSA P-384)
```bash
# Generate keypair (same as DNS)
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:secp384r1 -out key.pem

# Host public key at:
# https://example.com/.well-known/mcp-registry-auth
# Content: v=MCPv1; k=ecdsap384; p=PUBLIC_KEY
```

#### Anonymous (Testing)
```bash
mcp-publisher login none [--registry=URL]
```
- No authentication - for local testing only
- Only works with local registry instances

### `mcp-publisher publish`

Publish server to the registry.

For detailed guidance on the publishing process, see the [publishing guide](../../guides/publishing/publish-server.md).

**Usage:**
```bash
mcp-publisher publish [options]
```

**Options:**
- `--file=PATH` - Path to server.json (default: `./server.json`)
- `--registry=URL` - Registry URL override
- `--dry-run` - Validate without publishing

**Process:**
1. Validates `server.json` against schema
2. Verifies package ownership (see [Official Registry Requirements](../server-json/official-registry-requirements.md))
3. Checks namespace authentication
4. Publishes to registry

**Example:**
```bash
# Basic publish
mcp-publisher publish

# Dry run validation
mcp-publisher publish --dry-run

# Custom file location  
mcp-publisher publish --file=./config/server.json
```

### `mcp-publisher logout`

Clear stored authentication credentials.

**Usage:**
```bash
mcp-publisher logout
```

**Behavior:**
- Removes `~/.mcp_publisher_token`
- Does not revoke tokens on server side

## Configuration

### Token Storage
Authentication tokens stored in `~/.mcp_publisher_token` as JSON:
```json
{
  "token": "jwt-token-here",
  "registry_url": "https://registry.modelcontextprotocol.io",
  "expires_at": "2024-12-31T23:59:59Z"
}
```
