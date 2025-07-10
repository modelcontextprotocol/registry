# Repository References in server.json

The [`server.json` schema](schema.json) contains an optional `repository` property at the root of the JSON object. The `repository` object is metadata about the MCP server's source code. This allows users and security experts to inspect the code of the MCP service, thereby improving the transparency of what the MCP server is doing at runtime.

It is recommended for both local and remote MCP servers.

This object has the following properties, all of which are **required**:

| Property | Short Description                                       | GitHub Example                                   |
| -------- | ------------------------------------------------------- | ------------------------------------------------ |
| `source` | Well-defined string value representing the source forge | `github`                                         |
| `url`    | Website for the source repository                       | https://github.com/modelcontextprotocol/registry |
| `id`     | Stable identifier referring to the repository           | `927890076`                                      |

Consumers of the `server.json` metadata can use the `source` property to understand which specific source forge is used for hosting the MCP server's code. This is intended to be a string enum (a well-known list of values, defined by the MCP Registry deployment).

The `url` can be used to browse the source code. Some source forges, such as GitHub, support `git clone <url>` on the URL, which also works for web browsing. This is coincidental for the purposes of the Official MCP Registry, and the URL only needs to be accessible in a web browser.

The `id` value is owned and determined by the source forge, such as GitHub. This value is meant to be stable across repository renames and, if applicable on the source forge, can be used to detect repository resurrection attacks. If a repository is renamed, the `id` value should remain constant. If the repository is deleted and then recreated later, the `id` value should change.

Determining the `id` is an operation specific to the source forge. For GitHub, the following [GitHub CLI](https://cli.github.com/) command can be used (works for both public and private repositories):

```bash
gh auth login
gh api repos/<repo owner>/<repo name> --jq '.id'
```

MCP server registries can define their own policies for allowed `source` values and whether the `url` must be publicly accessible.

An MCP server registry should validate that the `id` matches the given `url`, perhaps by invoking source-specific REST APIs to match the `id`. MCP server publish tooling may opt to compute the `id` value dynamically and enrich the `server.json` payload provided to the publish endpoint to simplify the workflow.

## Official MCP Registry Policies

The `repository` metadata is optional (as in the general MCP Registry protocol).

The Official MCP Registry has some policies related to the `repository` object that are stricter than those the general MCP Registry protocol allows.

See the [`registry-schema.json`](registry-schema.json) for the allowed `source` values.

A publicly accessible repository is recommended but not required.

The `id` MUST match the repository referenced by the `url`.
