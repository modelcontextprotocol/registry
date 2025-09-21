# Adding a new package registry

The MCP Registry project is a **metaregistry**, meaning that it hosts metadata for MCP servers but does not host the code for the servers directly.

For local MCP servers, the MCP Registry has pointers in the `packages` node of the [`server.json`](../../reference/server-json/generic-server-json.md) schema that refer to packages in supported package managers.

The list of supported package managers for hosting MCP servers is defined by the `properties.packages[N].properties.registryType` string enum in the [`server.json` schema](../../reference/server-json/server.schema.json). For example, this could be "npm" (for npmjs.com packages) or "pypi" (for PyPI packages).

For remote MCP servers, the package registry is not relevant. The MCP client consumes the server via a URL instead of by downloading and running a package. In other words, this document only applies to local MCP servers.

For the sake of illustration, this document will use npm (the Node.js package manager) as an example at each step.

## Prerequisites

The package registry must meet the following requirements:

1. The package registry supports packaging and executing CLI apps. Local MCP servers use the [stdio transport](https://modelcontextprotocol.io/docs/concepts/transports#standard-input%2Foutput-stdio).
   - npm CLI tools typically express their CLI commands in the [`bin` property of the package.json](https://docs.npmjs.com/cli/v11/configuring-npm/package-json#bin)
1. The package registry (or associated client tooling) has a widely accepted **single-shot** CLI command.
   - npm's `npx` tool executes CLI commands using a [documented execution heuristic](https://docs.npmjs.com/cli/v11/commands/npx#description)
   - For example, the MCP client can map the `server.json` metadata to an `npx` CLI execution, with args and environment variables populated via user input.
1. The package registry supports anonymous package downloads. This allows the MCP client software to use the metadata found in the MCP registry to discover, download, and execute package-based local MCP servers with minimal user intervention.
   - `npx` by default connects to the public npmjs.com registry, allowing simple consumption of public npm packages.
1. The package registry should support a validation mechanism to verify ownership of the server name. This prevents misattribution and ensures that only the actual package owner can reference their packages in server registrations. For example:
   - npm requires an `mcpName` field in `package.json` that matches the server name being registered
   - PyPI requires a `mcp-name:` line in the package README/description
   - Each registry type must implement a validation mechanism accessible via public API

## Steps

These steps may evolve as additional validations or details are discovered and mandated.

1. [Create a feature request issue](https://github.com/modelcontextprotocol/registry/issues/new?template=feature_request.md) on the MCP Registry repository to begin the discussion about adding the package registry.
   - Example for NuGet: https://github.com/modelcontextprotocol/registry/issues/126
1. Open a PR with the following changes:
   - Update the [`server.json` schema](../../reference/server-json/server.schema.json)
     - Add your package registry name to the `registryType` example array.
     - Add your package registry base url to the `registryBaseUrl` example array.
     - Add the single-shot CLI command name to the `runtimeHint` example value array.
   - Update the [`openapi.yaml`](../../reference/api/openapi.yaml)
     - Add your package registry name to the `registryType` enum value array.
     - Add your package registry base url to the `registryBaseUrl` enum value array.
     - Add the single-shot CLI command name to the `runtimeHint` example value array.
   - Add a sample, minimal `server.json` to the [`server.json` examples](../../reference/server-json/generic-server-json.md).
   - Implement a registry validator:
      - Create a new validator file: `internal/validators/registries/yourregistry.go`, following the pattern of existing validators. Examples:
         - **npm**: Checks for an `mcpName` field in `package.json` that matches the server name
         - **PyPI**: Searches for `mcp-name: server-name` format in the package README content
         - **NuGet**: Looks for `mcp-name: server-name` format in the package README file
         - **Docker/OCI**: Validates a Docker image label `io.modelcontextprotocol.server.name` in the image manifest
      - Add corresponding unit tests: `internal/validators/registries/yourregistry_test.go`
      - Register your validator in `internal/validators/validators.go`
   - Update the publishing documentation:
      - Add a new publishing guide: `docs/guides/publishing/publish-[yourregistry].md`, following the pattern of existing publishing guides (e.g., `publish-npm.md`, `publish-pypi.md`)
      - Include instructions on how to prepare packages for your registry, including any specific validation requirements
      - Update `docs/guides/publishing/README.md` to reference your new publishing guide
