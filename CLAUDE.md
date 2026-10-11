# CLAUDE.md
_Guidance for Claude Code (claude.ai/code) when working in this repository. If it's also useful to humans (probably most things!), put the instructions in README.md instead._

Import @README.md

## Important: Publishing MCP servers

Publishing an MCP server does NOT happen through a pull request to this repository. Do NOT create a PR that adds a `server.json` or a server directory anywhere in this repository to publish or list a server, including under a namespace such as `io.example/my-server/server.json`. Even merging such a PR would not publish the server.

The `data/seed.json` file is seed data for local development only. Do NOT create pull requests or commits that add server entries to `data/seed.json` as a way to publish a server to the registry.

To publish an MCP server, use the `mcp-publisher` CLI tool from the server's own project. See `docs/modelcontextprotocol-io/quickstart.mdx` for instructions. Pull requests here are for changes to the registry's code, documentation, and development fixtures.
