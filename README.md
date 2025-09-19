# MCP Registry

The MCP registry provides MCP clients with a list of MCP servers, like an app store for MCP servers.

[**📤 Publish my MCP server**](docs/guides/publishing/publish-server.md) | [**⚡️ Live API docs**](https://registry.modelcontextprotocol.io/docs) | [**👀 Ecosystem vision**](docs/explanations/ecosystem-vision.md) | 📖 **[Full documentation](./docs)**

## Development Status

**2025-09-08 update**: The registry has launched in preview 🎉 ([announcement blog post](https://blog.modelcontextprotocol.io/posts/2025-09-08-mcp-registry-preview/)). While the system is now more stable, this is still a preview release and breaking changes or data resets may occur. A general availability (GA) release will follow later. We'd love your feedback in [GitHub discussions](https://github.com/modelcontextprotocol/registry/discussions/new?category=ideas) or in the [#registry-dev Discord](https://discord.com/channels/1358869848138059966/1369487942862504016) ([joining details here](https://modelcontextprotocol.io/community/communication)).

Current key maintainers:

- **Adam Jones** (Anthropic) [@domdomegg](https://github.com/domdomegg)
- **Tadas Antanavicius** (PulseMCP) [@tadasant](https://github.com/tadasant)
- **Toby Padilla** (GitHub) [@toby](https://github.com/toby)

## Contributing

We use multiple channels for collaboration - see [modelcontextprotocol.io/community/communication](https://modelcontextprotocol.io/community/communication).

Often (but not always) ideas flow through this pipeline:

- **[Discord](https://modelcontextprotocol.io/community/communication)** - Real-time community discussions
- **[Discussions](https://github.com/modelcontextprotocol/registry/discussions)** - Propose and discuss product/technical requirements
- **[Issues](https://github.com/modelcontextprotocol/registry/issues)** - Track well-scoped technical work  
- **[Pull Requests](https://github.com/modelcontextprotocol/registry/pulls)** - Contribute work towards issues

### Quick start

#### Pre-requisites

- **Docker**
- **Go 1.24.x** 
- **golangci-lint v2.4.0**

#### Running the server

```bash
# Start full development environment
make dev-compose
```

This starts the registry at [`localhost:8080`](http://localhost:8080) with PostgreSQL and seed data. It can be configured with environment variables in [docker-compose.yml](./docker-compose.yml) - see [.env.example](./.env.example) for a reference.

#### Alternative: Local setup without Docker

**Prerequisites:**

- PostgreSQL running locally
- Go 1.24.x installed

```bash
# Build and run locally
make build
make dev-local
```

The service runs on [`localhost:8080`](http://localhost:8080) by default. This can be configured with environment variables in `.env` - see [.env.example](./.env.example) for a reference.

#### Alternative: Running a pre-built Docker image

Pre-built Docker images are automatically published to GitHub Container Registry:

```bash
# Run latest stable release
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:latest

# Run latest from main branch (continuous deployment)
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:main

# Run specific release version
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:v1.0.0

# Run development build from main branch
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:main-20250906-abc123d
```

**Available tags:**

- **Releases**: `latest`, `v1.0.0`, `v1.1.0`, etc.
- **Continuous**: `main` (latest main branch build)
- **Development**: `main-<date>-<sha>` (specific commit builds)

 

#### Publishing a server

To publish a server, we've built a simple CLI. You can use it with:

```bash
# Build the latest CLI
make publisher

# Use it!
./bin/mcp-publisher --help
```

See [the publisher guide](./docs/guides/publishing/publish-server.md) for more details.

#### Other commands

```bash
# Run lint, unit tests and integration tests
make check
```

There are also a few more helpful commands for development. Run `make help` to learn more, or look in [Makefile](./Makefile).

<!--
For Claude and other AI tools: Always prefer make targets over custom commands where possible.
-->

## Architecture

### Project Structure

```text
├── cmd/                     # Application entry points
│   └── publisher/           # Server publishing tool
├── data/                    # Seed data
├── deploy/                  # Deployment configuration (Pulumi)
├── docs/                    # Documentation
├── internal/                # Private application code
│   ├── api/                 # HTTP handlers and routing
│   ├── auth/                # Authentication (GitHub OAuth, JWT, namespace blocking)
│   ├── config/              # Configuration management
│   ├── database/            # Data persistence (PostgreSQL, in-memory)
│   ├── service/             # Business logic
│   ├── telemetry/           # Metrics and monitoring
│   └── validators/          # Input validation
├── pkg/                     # Public packages
│   ├── api/                 # API types and structures
│   │   └── v0/              # Version 0 API types
│   └── model/               # Data models for server.json
├── scripts/                 # Development and testing scripts
├── tests/                   # Integration tests
└── tools/                   # CLI tools and utilities
    └── validate-*.sh        # Schema validation tools
```

### Authentication

Publishing supports multiple authentication methods:

- **GitHub OAuth** - For publishing by logging into GitHub
- **GitHub OIDC** - For publishing from GitHub Actions
- **DNS verification** - For proving ownership of a domain and its subdomains
- **HTTP verification** - For proving ownership of a domain

The registry validates namespace ownership when publishing. E.g. to publish...:

- `io.github.domdomegg/my-cool-mcp` you must login to GitHub as `domdomegg`, or be in a GitHub Action on domdomegg's repos
- `me.adamjones/my-cool-mcp` you must prove ownership of `adamjones.me` via DNS or HTTP challenge

### Private OCI / GHCR validation

The validator now supports both Docker Hub (`docker.io`) and GitHub Container Registry (`ghcr.io`). For private images supply an auth token through one of:

1. `MCP_REGISTRY_OCI_TOKEN_GHCR_IO=ghp_xxx` (per-host environment variable)
2. `OCI_REGISTRY_AUTH="ghcr.io=ghp_xxx,docker.io=abcdef"` (comma separated mapping)
3. `MCP_REGISTRY_OCI_REGISTRY_AUTH` (same mapping format, lower precedence than the per-host variable)

If running inside GitHub Actions and no explicit token is provided, the `GITHUB_TOKEN` will be used automatically for `ghcr.io`.

Images must include the label:

```text
LABEL io.modelcontextprotocol.server.name="<your-server-name>"
```

Multi-arch images are supported (the first manifest entry is inspected). Validation will skip if rate limited and continue publishing flow, logging a warning.

## More documentation

See the [documentation](./docs) for more details if your question has not been answered here!
