# MCP Registry

The MCP registry provides MCP clients with a list of MCP servers, like an app store for MCP servers.

📖 **[Full documentation](./docs)**

## Development Status

> [!WARNING]  
> The registry is under [active development](#development-status). The registry API spec is unstable and the official MCP registry database may be wiped at any time.

**2025-09-04 update**: We're targeting a 'preview' go-live on 8th September. This may still be unstable and not provide durability guarantees, but is a step towards being more solidified. A general availability (GA) release will follow later.

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

### Quick start:

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

<details>
<summary>Alternative: Local setup without Docker</summary>

**Prerequisites:**
- PostgreSQL running locally
- Go 1.24.x installed

```bash
# Build and run locally
make build
make dev-local
```

The service runs on [`localhost:8080`](http://localhost:8080) by default. This can be configured with environment variables in `.env` - see [.env.example](./.env.example) for a reference.

</details>

<details>
<summary>Alternative: Running a pre-built Docker image</summary>

Pre-built Docker images are automatically published to GitHub Container Registry:

```bash
# Run latest from main branch
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:latest

# Run specific commit build
docker run -p 8080:8080 ghcr.io/modelcontextprotocol/registry:main-20250806-a1b2c3d
```

**Available tags:** `latest`, `main-<date>-<sha>`

</details>

#### Publishing a server

To publish a server, we've built a simple CLI. You can use it with:

```bash
# Build the latest CLI
make publisher

# Use it!
./cmd/publisher/bin/mcp-publisher --help
```

See [the publisher guide](./docs/guides/publishing/use-publisher-cli.md) for more details.

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

```
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

## More documentation

See the [documentation](./docs) for more details if your question has not been answered here!
