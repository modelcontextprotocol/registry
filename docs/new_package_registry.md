# Adding a new package registry

The MCP Registry project is a **metaregistry**, meaning that it hosts metadata for MCP servers but does not host the code for the servers directly.

For local MCP servers, the MCP Registry has pointers in the `packages` node of the [`server.json`](server-json/README.md) schema that refer to packages in supported package managers.

The list of supported package managers for hosting MCP servers is defined by the `properties.packages[N].properties.registry_name` string enum in the [`server.json` schema](server-json/schema.json). For example, this could be "npm" (for npmjs.com packages) or "pypi" (for PyPI packages).

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

## Steps

These steps are currently very brief because the MCP Registry service is not yet deployed to production. These steps may evolve as additional validations or details are discovered and mandated.

1. [Create a feature request issue](https://github.com/modelcontextprotocol/registry/issues/new?template=feature_request.md) on the MCP Registry repository to begin the discussion about adding the package registry.
   - Example for NuGet: https://github.com/modelcontextprotocol/registry/issues/126
1. Open a PR with the following changes:
   - Update the [`server.json` schema](server-json/schema.json)
     - Add your package registry name to the `registry_name` enum value array.
     - Add the single-shot CLI command name to the `runtime_hint` example value array.
   - Update the [`openapi.yaml`](openapi.yaml)
     - Add your package registry name to the `registry_name` enum value array.
     - Add the single-shot CLI command name to the `runtime_hint` example value array.
     - This duplicates the previous step and will be improved with [issue #159](https://github.com/modelcontextprotocol/registry/issues/159).
   - Add a sample, minimal `server.json` to the [`server.json` examples](server-json/examples.md).
