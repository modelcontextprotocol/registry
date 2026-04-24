# Contributing to the MCP Registry

Thank you for your interest in contributing!

## Want to publish an MCP server?

**Do NOT open a pull request to add your server to `data/seed.json`.**

The `data/seed.json` file is seed data used only for local development. Modifying it will not publish your server to the registry.

To publish an MCP server, use the official `mcp-publisher` CLI tool. See the [publishing quickstart guide](docs/modelcontextprotocol-io/quickstart.mdx) for step-by-step instructions.

## Contributing to the registry codebase

We welcome contributions to the registry itself! Here's how to get started:

### Communication channels

We use multiple channels for collaboration - see [modelcontextprotocol.io/community/communication](https://modelcontextprotocol.io/community/communication).

Often (but not always) ideas flow through this pipeline:

- **[Discord](https://modelcontextprotocol.io/community/communication)** - Real-time community discussions
- **[Discussions](https://github.com/modelcontextprotocol/registry/discussions)** - Propose and discuss product/technical requirements
- **[Issues](https://github.com/modelcontextprotocol/registry/issues)** - Track well-scoped technical work
- **[Pull Requests](https://github.com/modelcontextprotocol/registry/pulls)** - Contribute work towards issues

### Development setup

See the [README](README.md#quick-start) for prerequisites and instructions on running the server locally.

### Running checks

```bash
# Run lint, unit tests and integration tests
make check
```

Run `make help` for more available commands.