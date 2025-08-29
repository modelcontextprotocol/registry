# MCP Registry

> [!WARNING]  
> The registry is under [active development](#development-status). The registry API spec is unstable and the official MCP registry database may be wiped at any time.

📖 **[API Documentation](https://staging.registry.modelcontextprotocol.io/docs)** | 📚 **[Technical Docs](./docs)**

## What is the registry?

The registry will provide MCP clients with a list of MCP servers. Like an app store for MCP servers. (In future it might do more, like also hosting a list of clients.)

There are two parts to the registry project:
1. 🟦 **The MCP registry spec**: An [API specification](./docs/server-registry-api/) that allows anyone to implement a registry.
2. 🟥 **The Official MCP registry**: A hosted registry following the MCP registry spec at [`registry.modelcontextprotocol.io`](https://registry.modelcontextprotocol.io). This serves as the **authoritative repository** for publicly-available MCP servers. Server creators publish once, and all consumers (MCP clients, aggregators, marketplaces) reference the same canonical data. This is owned by the MCP open-source community, backed by major trusted contributors to the MCP ecosystem such as Anthropic, GitHub, PulseMCP and Microsoft.

The registry is built around the [`server.json`](./docs/server-json/) format - a standardized way to describe MCP servers that works across discovery, initialization, and packaging scenarios.

In time, we expect the ecosystem to look a bit like this:

![](./docs/ecosystem-diagram.excalidraw.svg)

Note that MCP registries are _metaregistries_. They host metadata about packages, but not the package code or binaries. Instead, they reference other package registries (like NPM, PyPi or Docker) for this.

Additionally, we expect clients pull from _subregistries_. These subregistries add value to the registry ecosystem by providing curation, or extending it with additional metadata. The Official MCP registry expects a lot of API requests from ETL jobs from these subregistries.

## Development Status

**2025-08-26 update**: We're targeting a 'preview' go-live on 4th September. This may still be unstable and not provide durability guarantees, but is a step towards being more solidified. A general availability (GA) release will follow later.

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

See [the publisher README](./cmd/publisher/README.md) for more details.

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
├── docs/                    # Technical documentation
│   ├── server-json/         # server.json specification & examples
│   └── server-registry-api/ # API specification
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

See the [docs](./docs) folder for more details if your question has not been answered here!
