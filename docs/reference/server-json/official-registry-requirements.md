# Official Registry Server.json Requirements

This document describes the additional requirements and validation rules that apply when publishing to the official MCP Registry at `registry.modelcontextprotocol.io`.

For step-by-step publishing instructions, see the [publishing guide](../../modelcontextprotocol-io/quickstart.mdx).

## Overview

While the [generic server.json format](./generic-server-json.md) defines the base specification, the official registry enforces additional validation to ensure:

- **Namespace authentication** - Servers are published under appropriate namespaces
- **Package ownership verification** - Publishers actually control referenced packages
- **Restricted registry base urls** - Packages are from trusted public registries
- **`_meta` namespace restrictions** - Restricted to `publisher` key only

## Namespace Authentication

Publishers must prove ownership of their namespace. For example to publish to `com.example/server`, the publisher must prove they own the `example.com` domain.

See the [publishing guide](../../modelcontextprotocol-io/quickstart.mdx) for authentication details for GitHub and domain namespaces.

## Package Ownership Verification

All packages must include metadata proving the publisher owns them. This prevents impersonation and ensures authenticity (see more reasoning in [#96](https://github.com/modelcontextprotocol/registry/issues/96)).

For detailed verification requirements for each registry type, see the [publishing guide](../../modelcontextprotocol-io/quickstart.mdx).

## Restricted Registry Base URLs

Only trusted public registries are supported. Private registries and alternative mirrors are not allowed.

**Supported registries:**
- **NPM**: `https://registry.npmjs.org` only
- **PyPI**: `https://pypi.org` only
- **NuGet**: `https://api.nuget.org` only
- **Docker/OCI**:
  - Docker Hub (`docker.io`)
  - GitHub Container Registry (`ghcr.io`)
  - Google Artifact Registry (`*.pkg.dev`)
  - Azure Container Registry (`*.azurecr.io`)
  - Microsoft Container Registry (`mcr.microsoft.com`)
- **MCPB**: `https://github.com` releases and `https://gitlab.com` releases only

## `_meta` Namespace Restrictions

The `_meta` field in `server.json` allows publishers to include custom metadata alongside their server definitions.

### Publisher-Provided Metadata

When publishing to the official registry, **only data under the specific key `io.modelcontextprotocol.registry/publisher-provided` will be preserved**. Any other keys in the `_meta` object are silently dropped during publishing and will not be stored or returned by the registry.

**Example:**

```jsonc
{
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "tool": "ci-publisher",
      "version": "1.0.0",
      "custom_data": "your data here"
    },
    "some.other.key": {
      // This will be dropped and not preserved
    }
  }
}
```

**Size limit:** The publisher-provided extension is limited to 4KB (4096 bytes) of JSON. If the marshaled JSON exceeds this limit, publishing will fail with an error indicating the actual size.

### Registry API Metadata vs server.json Metadata

The `_meta` field in `server.json` is **different** from the `_meta` field returned in registry API responses:

- **In `server.json`**: The `_meta` field contains publisher-provided custom metadata under `io.modelcontextprotocol.registry/publisher-provided`
- **In API responses**: The `_meta` field is returned as a separate property at the response level (not inside `server.json`) and contains registry-managed metadata like:
  - `status`: Server lifecycle status (active, deprecated, deleted)
  - `publishedAt`: When the server was first published
  - `updatedAt`: When the server was last updated
  - `isLatest`: Whether this is the latest version

**Example: What you publish (server.json)**

```jsonc
{
  "name": "io.github.example/my-server",
  "version": "1.0.0",
  "description": "My MCP server",
  // ... other fields ...
  "_meta": {
    "io.modelcontextprotocol.registry/publisher-provided": {
      "tool": "ci-publisher",
      "version": "2.0.0"
    }
  }
}
```

**Example: What the registry API returns**

```jsonc
{
  "server": {
    "name": "io.github.example/my-server",
    "version": "1.0.0",
    "description": "My MCP server",
    // ... other fields ...
    "_meta": {
      "io.modelcontextprotocol.registry/publisher-provided": {
        "tool": "ci-publisher",
        "version": "2.0.0"
      }
    }
  },
  "_meta": {
    // Registry-managed metadata at response level
    "status": "active",
    "publishedAt": "2024-01-15T10:30:00Z",
    "updatedAt": "2024-01-15T10:30:00Z",
    "isLatest": true
  }
}
```

Notice how the registry API response has **two** `_meta` fields:
1. Inside the `server` object: Your publisher-provided metadata (preserved from server.json)
2. At the response level: Registry-managed metadata (automatically added by the registry)

Registry-managed metadata cannot be set or overridden by publishers. See the [API documentation](../api/generic-registry-api.md) for the complete response structure.
