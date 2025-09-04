# MCP Server Publisher Guide

This guide will explain how you can publish your MCP server to the official registry.

## Quick Start

```bash
# 1. Create a template server.json file
mcp-publisher init

# 2. Edit server.json with your server details
# 3. Add registry validation metadata to your package

# 4. Authenticate with the registry (one-time setup)
mcp-publisher login <method>

# 5. Publish your server
mcp-publisher publish
```

## Step 1: Initialize Template

Create a template `server.json` file with smart auto-detection:

```bash
mcp-publisher init
```

We'll try to autodetect your configuration, but you'll probably need to fill in some placeholder in the next step.

## Step 2: Configure Server Details

Edit the generated `server.json` file with your specific details:

- **Server name** - Must follow reverse-DNS format (e.g., `io.github.username/server-name`)
  - In step 4, you'll have to authenticate to this namespace. If you use your github username, you'll need to login with GitHub. If you use a custom domain, you'll need to set up a DNS or HTTP verification.
- **Description** - Clear explanation of what your server does
- **Package references** - Point to your published packages
- **Runtime configuration** - Arguments, environment variables, etc.
  - We recommend trying to reduce the configuration your server needs to start up, and using environment variables where configuration is needed.

**📖 Resources:**
- **[server.json Examples](../server-json/examples.md)** - Real-world server configurations
- **[server.json Specification](../server-json/)** - Complete field reference  

## Step 3: Add Package Validation Metadata

All packages must include ownership verification metadata to prevent misattribution. When you publish, the registry validates that you actually control the packages you're referencing by checking for specific metadata in each package.

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

### Example server.json
```json
{
  "name": "io.github.username/server-name",
  "packages": [
    {
      "registry_type": "mcpb",
      "identifier": "https://github.com/you/your-repo/releases/download/v1.0.0/server.mcpb"
    }
  ]
}
```

The official MCP registry currently only supports artifacts hosted on GitHub or GitLab releases.

</details>

## Step 4: Authenticate with Registry

Choose your authentication method based on your use case:

<details>
<summary><strong>🐙 GitHub</strong></summary>

GitHub auth grants publishing rights for `io.github.{username}/*` and `io.github.{org}/*` namespaces

### Interactive

```bash
mcp-publisher login github
```

- Opens browser for GitHub OAuth

### CI/CD in GitHub Actions

```bash
mcp-publisher login github-oidc
```

- Uses GitHub Actions OIDC tokens automatically
- Requires `id-token: write` permission in workflow

#### GitHub Actions Example
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
      - name: Login to Registry
        run: mcp-publisher login github-oidc
      - name: Publish to Registry  
        run: mcp-publisher publish
```

</details>

<details>
<summary><strong>🌐 DNS</strong></summary>

DNS auth grants publishing rights for `com.yourdomain/*` and `com.yourdomain.*/*` namespaces.

```bash
# 1. Generate keypair and get DNS record
openssl genpkey -algorithm Ed25519 -out key.pem
echo "Add this TXT record to your domain:"
echo "_mcp-registry.yourdomain.com. IN TXT \"v=MCPv1; k=ed25519; p=$(openssl pkey -in key.pem -pubout -outform DER | tail -c 32 | base64)\""

# 2. Login with private key
mcp-publisher login dns --domain yourdomain.com --private-key $(openssl pkey -in key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n')
```

</details>

<details>
<summary><strong>🔗 HTTP</strong></summary>

HTTP auth grants publishing rights for the `com.yourdomain/*` namespace.

```bash
# 1. Generate keypair and host public key
openssl genpkey -algorithm Ed25519 -out key.pem
echo "Host at: https://yourdomain.com/.well-known/mcp-registry-auth"
echo "Content: v=MCPv1; k=ed25519; p=$(openssl pkey -in key.pem -pubout -outform DER | tail -c 32 | base64)"

# 2. Login with private key
mcp-publisher login http --domain yourdomain.com --private-key $(openssl pkey -in key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n')
```

</details>

## Step 5: Publish Your Server

Once you've completed steps 1-4, publish your server:

```bash
mcp-publisher publish
```

If successful, your server will be available at `https://registry.modelcontextprotocol.io`!
