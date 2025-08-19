# MCP Registry Publisher Tool

The MCP Registry Publisher Tool is designed to publish Model Context Protocol (MCP) server details to an MCP registry. This tool uses GitHub OAuth device flow authentication to securely manage the publishing process.

## Building the Tool

You can build the publisher tool using the provided build script:

```bash
# Build for current platform
./build.sh

# Build for all supported platforms (optional)
./build.sh --all
```

The compiled binary will be placed in the `bin` directory.

## Usage

The tool supports two main commands:

### Publishing a server

```bash
# Basic usage with GitHub OAuth (interactive authentication)
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE>

# Use GitHub Actions OIDC (for CI/CD pipelines)
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> --auth-method github-oidc

# Force a new login even if a token exists
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> --login
```

### Creating a server.json file

```bash
# Create a new server.json file
./bin/mcp-publisher create --name "io.github.owner/repo" --description "My server" --repo-url "https://github.com/owner/repo"
```

### Command-line Arguments

- `--registry-url`: URL of the MCP registry (required)
- `--mcp-file`: Path to the MCP configuration file (required)
- `--login`: Force a new GitHub authentication even if a token already exists (overwrites existing token file)
- `--auth-method`: Authentication method to use (default: github-at)
  - `github-at`: Interactive GitHub OAuth device flow authentication
  - `github-oidc`: GitHub Actions OIDC authentication (for CI/CD)
  - `dns`: DNS-based public/private key authentication
  - `http`: HTTP-based public/private key authentication
  - `none`: No authentication (for registry contributors testing locally)
- `--dns-domain`: Domain name for DNS authentication (required for dns auth method)
- `--dns-private-key`: 64-character hex seed for DNS authentication (required for dns auth method)
- `--http-domain`: Domain name for HTTP authentication (required for http auth method)
- `--http-private-key`: 64-character hex seed for HTTP authentication (required for http auth method)

## Creating a server.json file

The tool provides a `create` command to help generate a properly formatted `server.json` file. This command takes various flags to specify the server details and generates a complete server.json file that you can then modify as needed.

### Usage

```bash
./bin/mcp-publisher create [flags]
```

### Create Command Flags

#### Required Flags
- `--name`, `-n`: Server name (e.g., io.github.owner/repo-name)
- `--description`, `-d`: Server description
- `--repo-url`: Repository URL

#### Optional Flags
- `--version`, `-v`: Server version (default: "1.0.0")
- `--repo-source`: Repository source (default: "github")
- `--output`, `-o`: Output file path (default: "server.json")
- `--execute`, `-e`: Command to execute the server (generates runtime arguments)
- `--registry`: Package registry name (default: "npm")
- `--package-name`: Package name (defaults to server name)
- `--package-version`: Package version (defaults to server version)
- `--runtime-hint`: Runtime hint (e.g., "docker")
- `--env-var`: Environment variable in format NAME:DESCRIPTION (can be repeated)
- `--package-arg`: Package argument in format VALUE:DESCRIPTION (can be repeated)

### Create Examples

#### Basic NPX Server

```bash
./bin/mcp-publisher create \
  --name "io.github.example/my-server" \
  --description "My MCP server" \
  --repo-url "https://github.com/example/my-server" \
  --execute "npx @example/my-server --verbose" \
  --env-var "API_KEY:Your API key for the service"
```

#### Docker Server

```bash
./bin/mcp-publisher create \
  --name "io.github.example/docker-server" \
  --description "Docker-based MCP server" \
  --repo-url "https://github.com/example/docker-server" \
  --runtime-hint "docker" \
  --execute "docker run --mount type=bind,src=/data,dst=/app/data example/server" \
  --env-var "CONFIG_PATH:Path to configuration file"
```

#### Server with Package Arguments

```bash
./bin/mcp-publisher create \
  --name "io.github.example/full-server" \
  --description "Complete server example" \
  --repo-url "https://github.com/example/full-server" \
  --execute "npx @example/server" \
  --package-arg "-s:Specify services and permissions" \
  --package-arg "--config:Configuration file path" \
  --env-var "API_KEY:Service API key" \
  --env-var "DEBUG:Enable debug mode"
```

The `create` command will generate a `server.json` file with:
- Proper structure and formatting
- Runtime arguments parsed from the `--execute` command
- Environment variables with descriptions
- Package arguments for user configuration
- All necessary metadata

After creation, you may need to manually edit the file to:
- Adjust argument descriptions and requirements
- Set environment variable optionality (`is_required`, `is_secret`)
- Add remote server configurations
- Fine-tune runtime and package arguments

## Authentication

The tool supports multiple authentication methods to accommodate different use cases:

### GitHub OAuth Device Flow (`github-at`) - Default

For interactive use:

1. **Automatic Setup**: The tool automatically retrieves the GitHub Client ID from the registry's health endpoint
2. **First Run Authentication**: When first run (or with the `--login` flag), the tool initiates the GitHub device flow
3. **User Authorization**: You'll be provided with a URL and a verification code to enter on GitHub
4. **Token Storage**: After successful authentication, the tool saves the access token locally in `.mcpregistry_github_token` for future use
5. **Token Exchange**: The GitHub token is exchanged for a short-lived registry token, which is saved locally in `.mcpregistry_registry_token`
6. **Secure Communication**: The registry token is sent in the HTTP Authorization header with the Bearer scheme for all registry API calls

```bash
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> --auth-method github-at
```

### GitHub Actions OIDC (`github-oidc`)

For CI/CD pipelines using GitHub Actions:

1. **Prerequisites**: Your GitHub Actions workflow must have `id-token: write` permissions
2. **Token Exchange**: The OIDC token is exchanged directly for a registry token via `/v0/auth/github-oidc`
3. **No Storage**: No local token files are created; authentication is ephemeral per workflow run

**Example GitHub Actions workflow:**

```yaml
name: Publish MCP Server
on:
  push:
    tags: ['v*']

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # Required for OIDC token generation
      contents: read   # E.g. To read the server.json file from your Git repo
    steps:
      - uses: actions/checkout@v4
      - name: Publish to MCP Registry
        run: |
          ./bin/mcp-publisher publish \
            --registry-url "https://registry.modelcontextprotocol.org" \
            --mcp-file "./server.json" \
            --auth-method github-oidc
```

### DNS Authentication (`dns`)

For domain-based authentication using public/private key cryptography:

1. **Generate Ed25519 keypair**: 
   ```bash
   openssl genpkey -algorithm Ed25519 -out /tmp/key.pem && \
   echo "\n\nDNS record to add to your domain:" && \
   echo "  Type: TXT" && \
   echo "  Value: v=MCPv1; k=ed25519; p=$(openssl pkey -in /tmp/key.pem -pubout -outform DER | tail -c 32 | base64)" && \
   echo "" && \
   echo "Private key for --dns-private-key flag:" && \
   echo "  $(openssl pkey -in /tmp/key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n')\n" && \
   rm /tmp/key.pem
   ```
2. **Add DNS TXT record**: Add a TXT record to your domain with format: `v=MCPv1; k=ed25519; p=<base64-public-key>`
3. **Use CLI arguments**: Provide domain and private key via command line flags

```bash
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> \
  --auth-method dns --dns-domain example.com --dns-private-key abc123...
```

This grants publishing permissions for both `example.com/*` and `*.example.com/*` namespaces.

### HTTP Authentication (`http`)

For domain-based authentication using HTTP-hosted public keys:

1. **Generate Ed25519 keypair**: 
   ```bash
   openssl genpkey -algorithm Ed25519 -out /tmp/key.pem && \
   echo "\n\nFile to host on your domain:" && \
   echo "  URL: https://yoursite.com/.well-known/mcp-registry-auth" && \
   echo "  Content: v=MCPv1; k=ed25519; p=$(openssl pkey -in /tmp/key.pem -pubout -outform DER | tail -c 32 | base64)" && \
   echo "" && \
   echo "Private key for --http-private-key flag:" && \
   echo "  $(openssl pkey -in /tmp/key.pem -noout -text | grep -A3 "priv:" | tail -n +2 | tr -d ' :\n')\n" && \
   rm /tmp/key.pem
   ```
2. **Host public key**: Create an HTTP endpoint at `https://yoursite.com/.well-known/mcp-registry-auth` that returns: `v=MCPv1; k=ed25519; p=<base64-public-key>`
3. **Use CLI arguments**: Provide domain and private key via command line flags

```bash
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> \
  --auth-method http --http-domain example.com --http-private-key abc123...
```

This grants publishing permissions for the `example.com/*` namespace.

### No Authentication (`none`)

Mainly for registry contributors, for testing locally:

```bash
./bin/mcp-publisher publish --registry-url <REGISTRY_URL> --mcp-file <PATH_TO_MCP_FILE> --auth-method none
```
