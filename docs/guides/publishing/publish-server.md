# Publish Your MCP Server

Complete guide to publishing an MCP server to the registry.

## What You'll Learn

By the end of this tutorial, you'll have:
- Created a server.json file for your MCP server
- Authenticated with the registry
- Successfully published your server
- Verified your server appears in the registry

## Prerequisites

- An MCP server you've already built ([follow this guide if you don't have one already](https://modelcontextprotocol.io/quickstart/server))
- Your server published to a package registry (npm, PyPI, Docker Hub, etc.)

## Step 1: Install the Publisher CLI

You can either download a pre-built binary or build from source.

### Option A: Download Pre-built Binary (Recommended)

Download the latest release for your platform:

```bash
# macOS Apple Silicon (M1/M2/M3)
curl -L https://github.com/modelcontextprotocol/registry/releases/download/v1.0.0/mcp-publisher_1.0.0_darwin_arm64.tar.gz | tar xz

# macOS Intel
curl -L https://github.com/modelcontextprotocol/registry/releases/download/v1.0.0/mcp-publisher_1.0.0_darwin_amd64.tar.gz | tar xz

# Linux x86_64
curl -L https://github.com/modelcontextprotocol/registry/releases/download/v1.0.0/mcp-publisher_1.0.0_linux_amd64.tar.gz | tar xz

# Linux ARM64
curl -L https://github.com/modelcontextprotocol/registry/releases/download/v1.0.0/mcp-publisher_1.0.0_linux_arm64.tar.gz | tar xz

# Windows x86_64 (PowerShell)
Invoke-WebRequest -Uri "https://github.com/modelcontextprotocol/registry/releases/download/v1.0.0/mcp-publisher_1.0.0_windows_amd64.tar.gz" -OutFile "mcp-publisher.tar.gz"
tar xf mcp-publisher.tar.gz

# Windows ARM64 (PowerShell)
Invoke-WebRequest -Uri "https://github.com/modelcontextprotocol/registry/releases/download/v1.0.0/mcp-publisher_1.0.0_windows_arm64.tar.gz" -OutFile "mcp-publisher.tar.gz"
tar xf mcp-publisher.tar.gz

# Add to PATH (macOS/Linux)
chmod +x mcp-publisher && sudo mv mcp-publisher /usr/local/bin/

# Add to PATH (Windows): Move mcp-publisher.exe to a directory in your PATH
```

### Option B: Build from Source

If you prefer to build from source (requires Go 1.24+):

```bash
# Clone the registry repository
git clone https://github.com/modelcontextprotocol/registry
cd registry
make publisher

# The binary will be at bin/mcp-publisher
export PATH=$PATH:$(pwd)/bin
```

## Step 2: Initialize Your server.json

Navigate to your server's directory and create a template:

```bash
cd /path/to/your/mcp-server
mcp-publisher init
```

This creates a `server.json` with auto-detected values. You'll see something like:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-07-09/server.schema.json",
  "name": "io.github.yourname/your-server",
  "description": "A description of your MCP server",
  "version": "1.0.0",
  "packages": [
    {
      "registry_type": "npm",
      "identifier": "your-package-name",
      "version": "1.0.0"
    }
  ]
}
```

## Step 3: Configure Your Server Details

Edit the generated `server.json`:

### Choose Your Namespace

The `name` field determines authentication requirements:

- **`io.github.yourname/*`** - Requires GitHub authentication
- **`com.yourcompany/*`** - Requires DNS or HTTP domain verification

### Add Package Validation

Your package must include validation metadata to prove ownership.


<details>
<summary><strong>📦 NPM Packages</strong></summary>

### Requirements
Add an `mcpName` field to your `package.json`:

```json
{
  "name": "your-npm-package",
  "version": "1.0.0",
  "mcpName": "io.github.username/server-name"
}
```

### How It Works
- Registry fetches `https://registry.npmjs.org/your-npm-package`
- Checks that `mcpName` field matches your server name
- Fails if field is missing or doesn't match

### Example server.json
```json
{
  "name": "io.github.username/server-name",
  "packages": [
    {
      "registry_type": "npm",
      "identifier": "your-npm-package",
      "version": "1.0.0"
    }
  ]
}
```

The official MCP registry currently only supports the NPM public registry (`https://registry.npmjs.org`).

</details>

<details>
<summary><strong>🐍 PyPI Packages</strong></summary>

### Requirements
Include your server name in your package README file using this format:

**MCP name format**: `mcp-name: io.github.username/server-name`

Add it to your README.md file (which becomes the package description on PyPI).

### How It Works
- Registry fetches `https://pypi.org/pypi/your-package/json`
- Passes if `mcp-name: server-name` is in the README content

### Example server.json
```json
{
  "name": "io.github.username/server-name",
  "packages": [
    {
      "registry_type": "pypi",
      "identifier": "your-pypi-package",
      "version": "1.0.0"
    }
  ]
}
```

The official MCP registry currently only supports the official PyPI registry (`https://pypi.org`).

</details>

<details>
<summary><strong>📋 NuGet Packages</strong></summary>

### Requirements
Include your server name in your package's README using this format:

**MCP name format**: `mcp-name: io.github.username/server-name`

Add a README file to your NuGet package that includes the server name.

### How It Works
- Registry fetches README from `https://api.nuget.org/v3-flatcontainer/{id}/{version}/readme`
- Passes if `mcp-name: server-name` is found in the README content

### Example server.json
```json
{
  "name": "io.github.username/server-name",
  "packages": [
    {
      "registry_type": "nuget",
      "identifier": "Your.NuGet.Package",
      "version": "1.0.0"
    }
  ]
}
```

The official MCP registry currently only supports the official NuGet registry (`https://api.nuget.org`).

</details>

<details>
<summary><strong>🐳 Docker/OCI Images</strong></summary>

### Requirements
Add an annotation to your Docker image:

```dockerfile
LABEL io.modelcontextprotocol.server.name="io.github.username/server-name"
```

### How It Works
- Registry authenticates with Docker Hub using public token
- Fetches image manifest using Docker Registry v2 API
- Checks that `io.modelcontextprotocol.server.name` annotation matches your server name
- Fails if annotation is missing or doesn't match

### Example server.json
```json
{
  "name": "io.github.username/server-name", 
  "packages": [
    {
      "registry_type": "oci",
      "identifier": "yourusername/your-mcp-server",
      "version": "1.0.0"
    }
  ]
}
```

The identifier is `namespace/repository`, and version is the tag and optionally digest.

The official MCP registry currently only supports the official Docker registry (`https://docker.io`).

</details>

<details>
<summary><strong>📁 MCPB Packages</strong></summary>

### Requirements
**MCP reference** - MCPB package URLs must contain "mcp" somewhere within them, to ensure the correct artifact has been uploaded. This may be with the `.mcpb` extension or in the name of your repository.

**File integrity** - MCPB packages must include a SHA-256 hash for file integrity verification. This is required at publish time and MCP clients will validate this hash before installation.

### How to Generate File Hashes
Calculate the SHA-256 hash of your MCPB file:

```bash
openssl dgst -sha256 server.mcpb
```

### Example server.json
```json
{
  "name": "io.github.username/server-name",
  "packages": [
    {
      "registry_type": "mcpb",
      "identifier": "https://github.com/you/your-repo/releases/download/v1.0.0/server.mcpb",
      "file_sha256": "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce"
    }
  ]
}
```

### File Hash Validation
- **Authors** are responsible for generating correct SHA-256 hashes when creating server.json
- **MCP clients** validate the hash before installing packages to ensure file integrity
- **The official registry** stores hashes but does not validate them
- **Subregistries** may choose to implement their own validation. This enables them to perform security scanning on MCPB files, and ensure clients get the same security scanned content.

The official MCP registry currently only supports artifacts hosted on GitHub or GitLab releases.

</details>

## Step 4: Authenticate

Choose your authentication method based on your namespace:

### GitHub Authentication (for io.github.* namespaces)

```bash
mcp-publisher login github
```

This opens your browser for OAuth authentication.

### DNS Authentication (for custom domains)

```bash
# Generate keypair
openssl genpkey -algorithm Ed25519 -out key.pem

# Get public key for DNS record
echo "_mcp-registry.yourcompany.com. IN TXT \"v=MCPv1; k=ed25519; p=$(openssl pkey -in key.pem -pubout -outform DER | tail -c 32 | base64)\""

# Add the TXT record to your DNS, then login
mcp-publisher login dns --domain yourcompany.com --private-key $(openssl pkey -in key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n')
```

## Step 5: Publish Your Server

With authentication complete, publish your server:

```bash
mcp-publisher publish
```

You'll see output like:
```
✓ Validating server.json
✓ Checking package ownership
✓ Publishing to registry
✓ Server published successfully!

Your server is now available at:
https://registry.modelcontextprotocol.io/servers/io.github.yourname/weather-server
```

## Step 6: Verify Publication

Check that your server appears in the registry:

```bash
curl https://registry.modelcontextprotocol.io/servers/io.github.yourname/weather-server
```

You should see your server metadata returned as JSON.

## Troubleshooting

**"Package validation failed"** - Ensure your package includes the required validation metadata (mcpName field, README mention, or Docker label).

**"Authentication failed"** - Verify you've correctly set up DNS records or are logged into the right GitHub account.

**"Namespace not authorized"** - Your authentication method doesn't match your chosen namespace format.

## Next Steps

- **Update your server**: Publish new versions with updated server.json files
- **Set up CI/CD**: Automate publishing with [GitHub Actions](github-actions.md)
- **Learn more**: Understand [server.json format](../../reference/server-json/generic-server-json.md) in depth

## What You've Accomplished

You've successfully published your first MCP server to the registry! Your server is now discoverable by MCP clients and can be installed by users worldwide.