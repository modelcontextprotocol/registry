# Repository References in server.json

The [`server.json` schema](schema.json) MAY contain a `repository` property at the root of the JSON object. The `repository` object provides metadata about the MCP server's source code. This enables users and security experts to inspect the code of the MCP service, thereby improving the transparency of what the MCP server is doing at runtime.

The inclusion of the `repository` object is RECOMMENDED for both local and remote MCP servers.

Consumers of the `server.json` metadata MAY use the `source` property to determine which specific source forge is used for hosting the MCP server's code. The value of `source` SHOULD be a string enum (a well-known list of values defined by the MCP Registry deployment).

The `url` property MAY be used to browse the source code. Some source forges, such as GitHub, support `git clone <url>` on the URL, which also works for web browsing. For the purposes of the Official MCP Registry, the URL MUST be accessible in a web browser.

The `id` property is owned and determined by the source forge, such as GitHub. This value SHOULD be stable across repository renames and, if applicable on the source forge, MAY be used to detect repository resurrection attacks. If a repository is renamed, the `id` value SHOULD remain constant. If the repository is deleted and then recreated later, the `id` value SHOULD change.

Determining the `id` is specific to the source forge. For GitHub, the following [GitHub CLI](https://cli.github.com/) command MAY be used (works for both public and private repositories):

```bash
gh auth login
gh api repos/<repo owner>/<repo name> --jq '.id'
```

MCP server registries MAY define their own policies for allowed `source` values and whether the `url` MUST be publicly accessible.

An MCP server registry SHOULD validate that the `id` matches the given `url`, perhaps by invoking source-specific REST APIs to match the `id`. MCP server publish tooling MAY compute the `id` value dynamically and enrich the `server.json` payload provided to the publish endpoint to simplify the workflow.

## Official MCP Registry Policies

The `repository` metadata MAY be included in the `server.json`, as in the general MCP Registry protocol.

The Official MCP Registry has policies related to the `repository` object that are stricter than those the general MCP Registry protocol allows.

See the [`registry-schema.json`](registry-schema.json) for the allowed `source` values.

The repository referenced by the `repository` property SHOULD be publicly accessible, but this is not REQUIRED.

The `id` MUST match the repository referenced by the `url`.
