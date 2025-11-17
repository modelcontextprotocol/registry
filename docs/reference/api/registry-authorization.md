# Registry Authorization

MCP registries wishing to implement authentication SHOULD follow the [MCP Authorization Specification](https://modelcontextprotocol.io/specification/draft/basic/authorization).

## How it works

The registry acts as an OAuth 2.1 Resource Server, identical to how MCP servers work. This means:

- **MCP clients** can reuse their existing MCP authorization implementation without any changes
- **Registries** validate access tokens the same way MCP servers do
- **Users** get a consistent login experience across MCP servers and registries

## Registry-Specific Scopes

Registries MAY use these scopes:

- `mcp-registry:read` - List and read server metadata
- `mcp-registry:write` - Publish, update, and delete servers

These are recommendations - registries may use any set of scopes they deem sensible.

Note that scopes only control what *types* of operations a user can perform. Registries should still apply user-level authorization to control which specific resources a user can access. For example, a user with `mcp-registry:write` might only be able to publish servers to namespaces they own, and may not have permissions to edit servers if the registry treats servers as immutable.

## Official Registry Authentication

The official modelcontextprotocol.io registry remains public for reading. For publishing servers, it uses a custom JWT-based authentication system for legacy reasons - see [its API spec](official-registry-api.md#authentication). This may change in future to align with the MCP Authorization Specification.
