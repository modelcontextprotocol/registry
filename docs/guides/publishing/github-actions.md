# Automate Publishing with GitHub Actions

Set up automated MCP server publishing using GitHub Actions.

## What You'll Learn

By the end of this tutorial, you'll have:
- A GitHub Actions workflow that automatically publishes your server
- Understanding of GitHub OIDC authentication
- Knowledge of best practices for automated publishing
- Working examples for Node.js, Python, and Docker projects

## Prerequisites

- Already published your server manually (complete [Publishing Tutorial](publish-server.md) first)
- GitHub repository with your MCP server code
- Basic understanding of CI/CD concepts
- 20-30 minutes

## GitHub Actions Setup

### Step 1: Create Workflow File

Create `.github/workflows/publish-mcp.yml` in your repository:

```yaml
name: Publish to MCP Registry

on:
  push:
    tags: ['v*']  # Triggers on version tags like v1.0.0

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write  # Required for OIDC authentication
      contents: read
    
    steps:
      - name: Checkout code
        uses: actions/checkout@v4
      
      - name: Setup Node.js  # Adjust for your language
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          registry-url: 'https://registry.npmjs.org'
      
      - name: Install dependencies
        run: npm ci
      
      - name: Run tests
        run: npm test
      
      - name: Build package  
        run: npm run build
      
      - name: Publish to npm
        run: npm publish
        env:
          NODE_AUTH_TOKEN: ${{ secrets.NPM_TOKEN }}
      
      - name: Install MCP Publisher
        run: |
          # Build publisher from source (requires Go)
          git clone https://github.com/modelcontextprotocol/registry publisher-repo
          cd publisher-repo
          make publisher
          cp cmd/publisher/bin/mcp-publisher ../mcp-publisher
          cd ..
          chmod +x mcp-publisher
      
      - name: Login to MCP Registry
        run: ./mcp-publisher login github-oidc
      
      - name: Publish to MCP Registry
        run: ./mcp-publisher publish
```

### Step 2: Configure Secrets

For npm publishing, add your NPM token to repository secrets:

1. Go to repository Settings → Secrets and variables → Actions
2. Add `NPM_TOKEN` with your npm access token

**Note:** GitHub OIDC authentication requires no additional secrets for MCP Registry publishing.

### Step 3: Tag and Release

Create a version tag to trigger the workflow:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The workflow will:
1. Run tests
2. Build your package  
3. Publish to npm
4. Automatically authenticate with the MCP Registry
5. Publish updated server.json

## Authentication Methods by CI Platform

### GitHub Actions - OIDC (Recommended)

```yaml
- name: Login to MCP Registry
  run: mcp-publisher login github-oidc
```

**Advantages:** 
- No secrets to manage
- Automatically scoped to your repository
- Most secure option

### GitHub Actions - Personal Access Token

```yaml
- name: Login to MCP Registry
  run: mcp-publisher login github --token ${{ secrets.GITHUB_TOKEN }}
  env:
    GITHUB_TOKEN: ${{ secrets.MCP_GITHUB_TOKEN }}
```

Add `MCP_GITHUB_TOKEN` secret with a GitHub PAT that has repo access.

### DNS Authentication (Any CI)

For custom domain namespaces (`com.yourcompany/*`):

```yaml
- name: Login to MCP Registry
  run: |
    echo "${{ secrets.MCP_PRIVATE_KEY }}" > key.pem
    mcp-publisher login dns --domain yourcompany.com --private-key-file key.pem
```

Add your Ed25519 private key as `MCP_PRIVATE_KEY` secret.

## Language-Specific Examples

### Python Project

```yaml
name: Publish Python MCP Server

on:
  push:
    tags: ['v*']

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
      contents: read
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup Python
        uses: actions/setup-python@v4
        with:
          python-version: '3.11'
      
      - name: Install Poetry
        run: pipx install poetry
      
      - name: Build package
        run: poetry build
      
      - name: Publish to PyPI
        run: poetry publish
        env:
          POETRY_PYPI_TOKEN_PYPI: ${{ secrets.PYPI_TOKEN }}
      
      - name: Install MCP Publisher
        run: |
          git clone https://github.com/modelcontextprotocol/registry publisher-repo
          cd publisher-repo && make publisher && cd ..
          cp publisher-repo/cmd/publisher/bin/mcp-publisher mcp-publisher
      
      - name: Publish to MCP Registry
        run: |
          ./mcp-publisher login github-oidc
          ./mcp-publisher publish
```

### Docker Project

```yaml
name: Publish Docker MCP Server

on:
  push:
    tags: ['v*']

jobs:
  publish:
    runs-on: ubuntu-latest
    permissions:
      id-token: write
      contents: read
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Setup Docker Buildx
        uses: docker/setup-buildx-action@v3
      
      - name: Login to Docker Hub
        uses: docker/login-action@v3
        with:
          username: ${{ secrets.DOCKER_USERNAME }}
          password: ${{ secrets.DOCKER_PASSWORD }}
      
      - name: Extract version
        id: version
        run: echo "version=${GITHUB_REF#refs/tags/v}" >> $GITHUB_OUTPUT
      
      - name: Build and push
        uses: docker/build-push-action@v5
        with:
          context: .
          push: true
          tags: yourname/your-server:${{ steps.version.outputs.version }}
          labels: |
            io.modelcontextprotocol.server.name=io.github.yourname/your-server
      
      - name: Install MCP Publisher
        run: |
          git clone https://github.com/modelcontextprotocol/registry publisher-repo
          cd publisher-repo && make publisher && cd ..
          cp publisher-repo/cmd/publisher/bin/mcp-publisher mcp-publisher
      
      - name: Publish to MCP Registry
        run: |
          ./mcp-publisher login github-oidc
          ./mcp-publisher publish
```


## Best Practices

### 1. Version Alignment

Keep your package version and server.json version in sync:

```yaml
- name: Update server.json version
  run: |
    VERSION=${GITHUB_REF#refs/tags/v}
    jq --arg version "$VERSION" '.version = $version' server.json > tmp.json
    mv tmp.json server.json
```

### 2. Conditional Publishing

Only publish to registry after package publishing succeeds:

```yaml
- name: Publish to npm
  run: npm publish
  id: npm-publish

- name: Publish to MCP Registry
  if: steps.npm-publish.outcome == 'success'
  run: ./mcp-publisher publish
```


## Troubleshooting

**"Publisher binary not found"** - Ensure you download the correct binary for your CI platform (linux/mac/windows).

**"Authentication failed"** - For GitHub OIDC, verify `id-token: write` permission is set. For other methods, check secret configuration.

**"Package validation failed"** - Ensure your package was published successfully before MCP Registry publishing runs.

**"Version already exists"** - Each server.json version must be unique. Consider using build numbers: `1.0.0-build.123`.

## What You've Accomplished

You now have automated MCP server publishing that:
- Triggers on version tags
- Runs tests before publishing
- Publishes to package registry first
- Automatically publishes to MCP Registry
- Handles authentication securely
- Provides failure notifications

Your MCP server publishing is now fully automated - just tag a release and everything happens automatically!