# Copilot Instructions for MCP Registry

## Important: Publishing MCP servers

The `data/seed.json` file is seed data for local development only. Do NOT create pull requests that add or modify server entries in `data/seed.json` to publish a server.

To publish an MCP server to the registry, use the `mcp-publisher` CLI tool. See `docs/modelcontextprotocol-io/quickstart.mdx` for instructions.

## Development

- Use `make` targets where possible (run `make help` to see available targets)
- Run `make check` to run lint, unit tests, and integration tests
- Run `make dev-compose` to start the local development environment