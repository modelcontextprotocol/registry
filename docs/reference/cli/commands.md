# Publisher CLI Commands Reference

Complete command reference for the `mcp-publisher` CLI tool.

See the [publishing guide](../../modelcontextprotocol-io/quickstart.mdx) for a walkthrough of using the CLI to publish a server.

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

Also see [the guide to publishing from GitHub Actions](../../modelcontextprotocol-io/github-actions.mdx).

#### DNS Verification
```bash
mcp-publisher login dns --domain=example.com --private-key=HEX_KEY [--registry=URL]
```
- Verifies domain ownership via DNS TXT record
- Grants access to `com.example.*` namespaces
- Requires Ed25519 private key (64-character hex) or ECDSA P-384 private key (96-character hex)
  - The private key can be stored in a cloud signing provider like Google KMS or Azure Key Vault.

**Setup:** (for Ed25519, recommended)
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

**Setup:** (for Google KMS signing)

This requires the [gcloud CLI](https://cloud.google.com/sdk/docs/install).

```bash
# log in and set default project
gcloud auth login
gcloud config set project myproject

# Create a keyring in your project
gcloud kms keyrings create mykeyring --location global

# Create an Ed25519 signing key
gcloud kms keys create mykey --default-algorithm=ec-sign-ed25519 --purpose=asymmetric-signing --keyring=mykeyring --location=global

# Enable Application Default Credentials (ADC) so the publisher tool can sign
gcloud auth application-default login

# Attempt login to show the public key
mcp-publisher login dns google-kms --domain=example.com --resource=projects/myproject/locations/global/keyRings/mykeyring/cryptoKeys/mykey/cryptoKeyVersions/1

# Copy the "Expected proof record" and add the TXT record
# example.com. IN TXT "v=MCPv1; k=ed25519; p=PUBLIC_KEY"

# Re-run the login command
mcp-publisher login dns google-kms --domain=example.com --resource=projects/myproject/locations/global/keyRings/mykeyring/cryptoKeys/mykey/cryptoKeyVersions/1
```

**Setup:** (for Azure Key Vault signing)

This requires the [Azure CLI](https://learn.microsoft.com/en-us/cli/azure/install-azure-cli?view=azure-cli-latest).

```bash
# log in and set default subscription
az login
az account set --subscription "My Subscription (name or ID)"

# Create a resource group
az group create --location westus --resource-group MyResourceGroup

# Create a Key Vault
az keyvault create --name MyKeyVault --location westus --resource-group MyResourceGroup

# Create an ECDSA P-384 signing key
az keyvault key create --name MyKey --vault-name MyKeyVault --curve P-384

# Attempt login to show the public key
mcp-publisher login dns azure-key-vault --domain=example.com --vault MyKeyVault --key MyKey

# Copy the "Expected proof record" and add the TXT record
# example.com. IN TXT "v=MCPv1; k=ecdsap384; p=PUBLIC_KEY"

# Re-run the login command
mcp-publisher login dns azure-key-vault --domain=example.com --vault MyKeyVault --key MyKey
```

#### HTTP Verification
```bash
mcp-publisher login http --domain=example.com --private-key=HEX_KEY [--registry=URL]
```
- Verifies domain ownership via HTTPS endpoint  
- Grants access to `com.example.*` namespaces
- Requires Ed25519 private key (64-character hex) or ECDSA P-384 private key (96-character hex)
  - The private key can be stored in a cloud signing provider like Google KMS or Azure Key Vault.

**Setup:** (for Ed25519, recommended)
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

Cloud signing is also supported for HTTP authentication, similar to the DNS examples above. Just swap out the `dns` positional argument for `http`.

#### Anonymous (Testing)
```bash
mcp-publisher login none [--registry=URL]
```
- No authentication - for local testing only
- Only works with local registry instances

### `mcp-publisher validate`

Validate a `server.json` file without publishing.

**Usage:**
```bash
mcp-publisher validate [file]
```

**Arguments:**
- `file` - Path to server.json file (default: `./server.json`)

**Behavior:**
- Performs exhaustive validation, reporting all issues at once (not just the first error)
- Validates JSON syntax and schema compliance
- Runs semantic validation (business logic checks)
- Checks for deprecated schema versions and provides migration guidance
- Includes detailed error locations with JSON paths (e.g., `packages[0].transport.url`)
- Shows validation issue type (json, schema, semantic, linter)
- Displays severity level (error, warning, info)
- Provides schema references showing which validation rule triggered each error

**Example output:**
```bash
$ mcp-publisher validate
✅ server.json is valid

$ mcp-publisher validate custom-server.json
❌ Validation failed with 2 issue(s):

1. [error] repository.url (schema)
   '' has invalid format 'uri'
   Reference: #/definitions/Repository/properties/url/format from: [#/definitions/ServerDetail]/properties/repository/[#/definitions/Repository]/properties/url/format

2. [error] name (semantic)
   server name must be in format 'dns-namespace/name'
   Reference: invalid-server-name
```

### `mcp-publisher publish`

Publish server to the registry.

For detailed guidance on the publishing process, see the [publishing guide](../../modelcontextprotocol-io/quickstart.mdx).

**Usage:**
```bash
mcp-publisher publish [PATH]
```

**Options:**
- `PATH` - Path to server.json (default: `./server.json`)

**Process:**
1. Validates `server.json` against schema
2. Publishes the `server.json` to the registry server URL specified in the login token
3. Server: Verifies package ownership (see [Official Registry Requirements](../server-json/official-registry-requirements.md))
4. Server: Checks namespace authentication
5. Server: Publishes to registry

**Example:**
```bash
# Basic publish
mcp-publisher publish

# Custom file location  
mcp-publisher publish ./config/server.json
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
