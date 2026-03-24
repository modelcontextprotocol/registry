# CLAUDE.md
_Guidance for Claude Code (claude.ai/code) when working in this repository. If it's also useful to humans (probably most things!), put the instructions in README.md instead._

Import @README.md

## Important: Publishing MCP servers

The `data/seed.json` file is seed data for local development only. Do NOT create pull requests or commits that add server entries to `data/seed.json` as a way to publish a server to the registry.

To publish an MCP server, use the `mcp-publisher` CLI tool. See `docs/modelcontextprotocol-io/quickstart.mdx` for instructions.
