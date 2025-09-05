# Official Registry Server.json Requirements

This document describes the additional requirements and validation rules that apply when publishing to the official MCP Registry at `registry.modelcontextprotocol.io`.

For step-by-step publishing instructions, see the [publishing guide](../../guides/publishing/publish-server.md).

## Overview

While the [generic server.json format](./generic-server-json.md) defines the base specification, the official registry enforces additional validation to ensure:

- **Namespace authentication** - Servers are published under appropriate namespaces
- **Package ownership verification** - Publishers actually control referenced packages
- **Remote server URL match** - Remote server base urls match namespaces
- **Restricted registry base urls** - Packages are from trusted public registries
- **`_meta` namespace restrictions** - Restricted to `publisher` key only

## Namespace Authentication

Publishers must prove ownership of their namespace. For example to publish to `com.example/server`, the publisher must prove they own the `example.com` domain.

See the [publishing guide](../../guides/publishing/publish-server.md) for authentication details for GitHub and domain namespaces.

## Package Ownership Verification

All packages must include metadata proving the publisher owns them. This prevents impersonation and ensures authenticity (see more reasoning in [#96](https://github.com/modelcontextprotocol/registry/issues/96)).

For detailed verification requirements for each registry type, see the [publishing guide](../../guides/publishing/publish-server.md).

## Remote Server URL Match

Remote servers must use URLs that match the publisher's domain from their namespace. For example, `com.example/server` can only use remote URLs on `example.com` or its subdomains.

## Restricted Registry Base URLs

Only trusted public registries are supported. Private registries and alternative mirrors are not allowed.

**Supported registries:**
- **NPM**: `https://registry.npmjs.org` only
- **PyPI**: `https://pypi.org` only  
- **NuGet**: `https://api.nuget.org` only
- **Docker/OCI**: `https://docker.io` only
- **MCPB**: `https://github.com` releases and `https://gitlab.com` releases only

## `_meta` Namespace Restrictions

The `_meta` field is restricted to the `publisher` key only during publishing. This `_meta.publisher` extension is currently limited to 4KB.

Registry metadata is added automatically and cannot be overridden.
