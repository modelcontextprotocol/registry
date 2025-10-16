# Publish Your MCP Server

Complete guide to publishing an MCP server to the registry.

> [!TIP]
> Using an agentic AI coding assistant? Just use this prompt:
> ```
> Read https://raw.githubusercontent.com/modelcontextprotocol/registry/refs/heads/main/docs/guides/publishing/publish-server.md and https://raw.githubusercontent.com/modelcontextprotocol/registry/refs/heads/main/docs/guides/publishing/github-actions.md. Evaluate the best way to publish this server to the registry (prefer automated CI flows over manual flows where possible), and implement that. If possible, validate the server.json against the $schema before telling the user you are done, using a proper json schema library or tool available on the user's machine. If you get stuck, guide the user through the parts of the publishing process they need to do (e.g. logging into the publisher CLI if doing manual pubishing).
> ```

## What You'll Learn

By the end of this tutorial, you'll have:
- Created a server.json file for your MCP server
- Authenticated with the registry
- Successfully published your server
- Verified your server appears in the registry

## Prerequisites

- An MCP server you've already built ([follow this guide if you don't have one already](https://modelcontextprotocol.io/quickstart/server))
- Your server must be publicly accessible (closed source servers are welcome, but private/internal servers are not supported)

## Deployment Options

You can make your MCP server available in multiple ways:

- **📦 Package deployment**: Published to registries (npm, PyPI, NuGet, Docker Hub, etc.) and run locally by clients
- **🌐 Remote deployment**: Hosted as a web service that clients connect to directly
- **🔄 Hybrid deployment**: Offer both package and remote options for maximum flexibility

Learn more about [MCP server architecture](https://modelcontextprotocol.io/docs/learn/architecture) in the official docs.

## Step 1: Install the Publisher CLI

<details>
<summary><strong>🍺 macOS/Linux/WSL: With Homebrew (recommended)</strong></summary>

Requires [Homebrew](https://brew.sh):

```bash
brew install mcp-publisher
```

</details>

<details>
<summary><strong>⬇️ macOS/Linux/WSL: Pre-built binaries</strong></summary>

```bash
curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar xz mcp-publisher && sudo mv mcp-publisher /usr/local/bin/
```

</details>

<details>
<summary><strong>🏗️ macOS/Linux/WSL: From source</strong></summary>

Requires Git, Make and Go 1.24+:

```bash
# Clone the registry repository
git clone https://github.com/modelcontextprotocol/registry
cd registry
make publisher

# The binary will be at bin/mcp-publisher
export PATH=$PATH:$(pwd)/bin
```

</details>

<details>
<summary><strong>🪟 Windows PowerShell: Pre-built binaries</strong></summary>

```powershell
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -eq "Arm64") { "arm64" } else { "amd64" }; Invoke-WebRequest -Uri "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_windows_$arch.tar.gz" -OutFile "mcp-publisher.tar.gz"; tar xf mcp-publisher.tar.gz mcp-publisher.exe; rm mcp-publisher.tar.gz
# Move mcp-publisher.exe to a directory in your PATH
```

</details>

## Step 2: Initialize Your server.json

Navigate to your server's directory and create a template:

```bash
cd /path/to/your/mcp-server
mcp-publisher init
```

This creates a `server.json` with auto-detected values. You'll see something like:

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.yourname/weather-data-mcp",
  "title": "Weather Data",
  "description": "Access real-time weather data and forecasts",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "@yourname/weather-data-mcp",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      }
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

### Configure Deployment Methods

Configure your server to support packages, remotes, or both:

#### Package Deployment

Add package validation metadata to prove ownership of your packages.


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
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.username/slack-integration-mcp",
  "title": "Slack Integration",
  "description": "Send messages and manage Slack workspaces",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "npm",
      "identifier": "@username/slack-integration-mcp",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      }
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

Add it to your README.md file (which becomes the package description on PyPI). This can be in a comment if you want to hide it from display elsewhere.

### How It Works
- Registry fetches `https://pypi.org/pypi/your-package/json`
- Passes if `mcp-name: server-name` is in the README content

### Example server.json
```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.username/database-query-mcp",
  "title": "Database Query",
  "description": "Execute SQL queries and manage database connections",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "pypi",
      "identifier": "database-query-mcp",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      }
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

Add a README file to your NuGet package that includes the server name. This can be in a comment if you want to hide it from display elsewhere.

### How It Works
- Registry fetches README from `https://api.nuget.org/v3-flatcontainer/{id}/{version}/readme`
- Passes if `mcp-name: server-name` is found in the README content

### Example server.json
```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.username/azure-devops-mcp",
  "title": "Azure DevOps",
  "description": "Manage Azure DevOps work items and pipelines",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "nuget",
      "identifier": "Username.AzureDevOpsMcp",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      }
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
- Registry authenticates with container registries using token-based authentication:
  - **Docker Hub**: Uses `auth.docker.io` token service
  - **GitHub Container Registry**: Uses `ghcr.io` token service
- Fetches image manifest using Docker Registry v2 API
- Checks that `io.modelcontextprotocol.server.name` annotation matches your server name
- Fails if annotation is missing or doesn't match

### Example server.json (Docker Hub)
```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.username/kubernetes-manager-mcp",
  "title": "Kubernetes Manager",
  "description": "Deploy and manage Kubernetes resources",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "oci",
      "registryBaseUrl": "https://docker.io",
      "identifier": "yourusername/kubernetes-manager-mcp",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      }
    }
  ]
}
```

### Example server.json (GitHub Container Registry)
```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.username/git-operations-mcp",
  "title": "Git Operations",
  "description": "Advanced Git repository management and operations",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "oci",
      "registryBaseUrl": "https://ghcr.io",
      "identifier": "username/git-operations-mcp",
      "version": "1.0.0",
      "transport": {
        "type": "stdio"
      }
    }
  ]
}
```

The identifier is `namespace/repository`, and version is the tag and optionally digest.

The official MCP registry currently supports Docker Hub (`https://docker.io`) and GitHub Container Registry (`https://ghcr.io`).

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
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "io.github.username/image-processor-mcp",
  "title": "Image Processor",
  "description": "Process and transform images with various filters",
  "version": "1.0.0",
  "packages": [
    {
      "registryType": "mcpb",
      "identifier": "https://github.com/username/image-processor-mcp/releases/download/v1.0.0/image-processor.mcpb",
      "version": "1.0.0",
      "fileSha256": "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
      "transport": {
        "type": "stdio"
      }
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

#### Remote Deployment

Add the `remotes` field to your `server.json` (can coexist with `packages`):

<details>
<summary><strong>🌐 Remote Server Configuration</strong></summary>

### Requirements

- **Service endpoint**: Your MCP server must be accessible at the specified URL
- **Transport protocol**: Choose from `streamable-http` (recommended) or `sse` (deprecated)
- **URL validation**: For domain namespaces only (see URL requirements below)

### Example server.json

```json
{
  "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
  "name": "com.yourcompany/acme-analytics",
  "title": "ACME Analytics",
  "description": "Real-time business intelligence and reporting platform",
  "version": "2.0.0",
  "remotes": [
    {
      "type": "streamable-http",
      "url": "https://mcp.yourcompany.com/mcp"
    }
  ]
}
```

### Multiple Transport Options

You can offer multiple connection methods:

```json
{
  "remotes": [
    {
      "type": "streamable-http",
      "url": "https://mcp.yourcompany.com/mcp"
    },
    {
      "type": "sse",
      "url": "https://mcp.yourcompany.com/sse"
    }
  ]
}
```

### URL Validation Requirements

- For `com.yourcompany/*` namespaces: URLs must be on `yourcompany.com` or its subdomains
- For `io.github.username/*` namespaces: No URL restrictions (but you must authenticate via GitHub)

### Authentication Headers (Optional)

Configure headers that clients should send when connecting:

```json
{
  "remotes": [
    {
      "type": "streamable-http",
      "url": "https://mcp.yourcompany.com/mcp",
      "headers": [
        {
          "name": "X-API-Key",
          "description": "API key for authentication",
          "isRequired": true,
          "isSecret": true
        }
      ]
    }
  ]
}
```

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
echo "yourcompany.com. IN TXT \"v=MCPv1; k=ed25519; p=$(openssl pkey -in key.pem -pubout -outform DER | tail -c 32 | base64)\""

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
✓ Successfully published
```

## Step 6: Verify Publication

Check that your server appears in the registry by searching for it:

```bash
curl "https://registry.modelcontextprotocol.io/v0/servers?search=io.github.yourname/weather-server"
```

You should see your server metadata returned in the JSON response.

## Troubleshooting

**"Package validation failed"** - Ensure your package includes the required validation metadata (mcpName field, README mention, or Docker label).

**"Authentication failed"** - Verify you've correctly set up DNS records or are logged into the right GitHub account.

**"Namespace not authorized"** - Your authentication method doesn't match your chosen namespace format.

## Examples

See these real-world examples of published servers:
- [NPM, Docker and MCPB example](https://github.com/domdomegg/airtable-mcp-server)
- [NuGet example](https://github.com/domdomegg/time-mcp-nuget)
- [PyPI example](https://github.com/domdomegg/time-mcp-pypi)

## Next Steps

- **Update your server**: Publish new versions with updated server.json files
- **Set up CI/CD**: Automate publishing with [GitHub Actions](github-actions.md)
- **Learn more**: Understand [server.json format](../../reference/server-json/generic-server-json.md) in depth
- **More examples**: See [remote server configurations](../../reference/server-json/generic-server-json.md#remote-server-example) and [hybrid deployments](../../reference/server-json/generic-server-json.md#server-with-remote-and-package-options) in the schema documentation

## What You've Accomplished

You've successfully published your first MCP server to the registry! Your server is now discoverable by MCP clients and can be installed by users worldwide.
