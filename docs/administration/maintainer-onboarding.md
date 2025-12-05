# Registry Maintainer Onboarding

This guide covers onboarding new maintainers to the MCP Registry project.

## Checklist

When onboarding a new maintainer, complete the following steps:

### 1. GitHub Organization

- [ ] Add them to the [modelcontextprotocol GitHub organization](https://github.com/modelcontextprotocol)
- [ ] Grant appropriate repository permissions (typically "Maintain" or "Admin" on the registry repo)

### 2. MAINTAINERS.md

- [ ] Add them to the `MAINTAINERS.md` file in [modelcontextprotocol/modelcontextprotocol](https://github.com/modelcontextprotocol/modelcontextprotocol)

### 3. README.md

- [ ] Add them to the "Current key maintainers" section in [README.md](../../README.md)

### 4. Claude GitHub App

The `@claude` bot is automatically available to all modelcontextprotocol org members (no manual step required). The workflow checks org membership dynamically via the GitHub API.

### 5. Google Workspace

- [ ] Create a @modelcontextprotocol.io Google Workspace account for them
- [ ] This is required for admin operations (see [admin-operations.md](./admin-operations.md))

### 6. Discord

- [ ] Invite them to the MCP Discord server
- [ ] Grant them the appropriate moderator/maintainer role
- [ ] Add them to the `#registry-dev` channel

## Offboarding

When a maintainer steps down:

1. Remove them from the GitHub organization (or adjust permissions)
2. Remove them from `MAINTAINERS.md`
3. Remove them from the README.md maintainers list
4. Disable or remove their @modelcontextprotocol.io account
5. Remove Discord moderator role (if applicable)

Note: The `@claude` bot access is automatically revoked when they are removed from the GitHub organization.
