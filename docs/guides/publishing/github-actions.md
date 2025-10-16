# Automate Publishing with GitHub Actions

Set up automated MCP server publishing using GitHub Actions.

## What You'll Learn

By the end of this tutorial, you'll have:

- A GitHub Actions workflow that automatically publishes your server
- Understanding of GitHub OIDC authentication
- Knowledge of best practices for automated publishing
- Working examples for Node.js, Python, and Docker projects

## Prerequisites

- Understand general publishing requirements like package verification (see the [publishing guide](publish-server.md))
- GitHub repository with your MCP server code

## GitHub Actions Setup

### Step 1: Create Workflow File

Create `.github/workflows/publish-mcp.yml`. Here's an example for NPM-based packages, but the MCP registry publishing steps are the same for all package types:

```yaml
name: Publish to MCP Registry

on:
  push:
    tags: ["v*"]  # Triggers on version tags like v1.0.0

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # Required for OIDC authentication
      contents: read

    steps:
      - name: Checkout code
        uses: actions/checkout@v5

      - name: Setup Node.js  # Adjust for your language
        uses: actions/setup-node@v5
        with:
          node-version: "lts/*"

      - name: Install dependencies
        run: npm ci

      - name: Run tests
        run: npm run test --if-present

      - name: Build package
        run: npm run build --if-present

      - name: Publish to npm
        run: npm publish
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}

      - name: Install MCP Publisher
        run: |
          curl -L "https://github.com/modelcontextprotocol/registry/releases/latest/download/mcp-publisher_$(uname -s | tr '[:upper:]' '[:lower:]')_$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/').tar.gz" | tar xz mcp-publisher

      - name: Login to MCP Registry
        run: ./mcp-publisher login github-oidc

      - name: Publish to MCP Registry
        run: ./mcp-publisher publish
```

### Step 2: Configure Secrets

You don't need any secrets for publishing to the MCP Registry using GitHub OIDC.

However you might need to add secrets for your package registry. For example the workflow above needs a `NPM_TOKEN` (which you can add in Settings → Secrets and variables → Actions).

### Step 3: Tag and Release

Create a version tag to trigger the workflow:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The workflow runs tests, builds your package, publishes to npm, and publishes to the MCP Registry.

## Authentication Methods

### GitHub Actions OIDC (Recommended)

```yaml
- name: Login to MCP Registry
  run: mcp-publisher login github-oidc
```

### GitHub Personal Access Token

```yaml
- name: Login to MCP Registry
  run: mcp-publisher login github --token ${{ secrets.GITHUB_TOKEN }}
  env:
    GITHUB_TOKEN: ${{ secrets.MCP_GITHUB_TOKEN }}
```

Add `MCP_GITHUB_TOKEN` secret with a GitHub PAT that has repo access.

### DNS Authentication

For custom domain namespaces (`com.yourcompany/*`):

```yaml
- name: Login to MCP Registry
  run: |
    echo "${{ secrets.MCP_PRIVATE_KEY }}" > key.pem
    mcp-publisher login dns --domain yourcompany.com --private-key-file key.pem
```

Add your Ed25519 private key as `MCP_PRIVATE_KEY` secret.

## Examples

See these real-world examples of automated publishing workflows:
- [NPM, Docker and MCPB](https://github.com/domdomegg/airtable-mcp-server)
- [NuGet](https://github.com/domdomegg/time-mcp-nuget)
- [PyPI](https://github.com/domdomegg/time-mcp-pypi)

## Tips

You can keep your package version and server.json version in sync automatically with something like:
```yaml
- run: |
    VERSION=${GITHUB_REF#refs/tags/v}
    jq --arg v "$VERSION" '.version = $v' server.json > tmp && mv tmp server.json
```

## Troubleshooting
- **"Authentication failed"**: Ensure `id-token: write` permission is set for OIDC, or check secrets
- **"Package validation failed"**: Verify your package published to your registry (NPM, PyPi etc.) successfully first, and that you have done the necessary validation steps in the [Publishing Tutorial](publish-server.md)
