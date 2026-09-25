## Summary

Adds a JSON Schema for MCP client capabilities, providing a formal structure for documenting what features each client supports.

Closes #718

## Acknowledgments

This schema builds on the work done by @jancurn in [mcp-client-capabilities](https://github.com/apify/mcp-client-capabilities), which established the core capability structure and has catalogued 40+ clients. This PR extends that foundation with additional fields for transports, platforms, authentication, and metadata.

## Changes

- **`docs/schemas/client-registry.json`** - JSON Schema 2020-12 definition

## What's Included

This PR focuses solely on the **schema definition**. Intentionally not included:

- **Client data** - The mcp-client-capabilities repo already has the data. Once the schema is agreed upon, that data can be migrated.
- **Validation scripts** - Can be added in a follow-up PR once the schema is finalized.

## Schema Overview

The schema tracks BOTH what clients declare to servers (sampling, elicitation, roots) AND which server capabilities clients can consume (tools, resources, prompts). This is intentionally broader than the MCP spec's `ClientCapabilities` interface.

**Minimal entry:**
```json
{
  "my-client": {
    "title": "My Client",
    "url": "https://example.com",
    "protocolVersion": "2025-11-25"
  }
}
```

**Full entry:**
```json
{
  "claude-code": {
    "title": "My Advanced Client",
    "url": "https://example.com",
    "protocolVersion": "2025-11-25",
    "category": "cli",
    "platforms": ["windows", "macos", "linux"],
    "transports": { "stdio": {} },
    "tools": { "listChanged": true },
    "resources": { "listChanged": true, "subscribe": true },
    "prompts": { "listChanged": true },
    "sampling": { "context": {}, "tools": {} },
    "elicitation": { "form": {}, "url": {} },
    "roots": { "listChanged": true },
    "tasks": {
      "list": {},
      "cancel": {},
      "requests": { "sampling": { "createMessage": {} } }
    },
    "metadata": {
      "vendor": "Some Vendor",
      "lastVerified": "2026-01-15"
    }
  }
}
```

## Features

| Feature | Description |
|---------|-------------|
| **Capabilities** | tools, resources, prompts, sampling, elicitation, roots, completions, logging, instructions, tasks |
| **Status values** | `{}` (supported), or `{ "status": "full\|partial\|unsupported\|untested", "notes": "..." }` |
| **Categories** | ide, chat, cli, automation, browser-extension, library, other |
| **Platforms** | windows, macos, linux, web, ios, android |
| **Transports** | stdio, sse, streamableHttp |
| **Auth** | oauth (with PKCE, grantTypes), header, none |
| **Metadata** | vendor, openSource, sourceUrl, license, lastVerified, conformanceScore |

## Related

- [modelcontextprotocol#1814](https://github.com/modelcontextprotocol/modelcontextprotocol/issues/1814) - Client compatibility matrix
- [mcp-client-capabilities](https://github.com/apify/mcp-client-capabilities) - Community client data

---

🦉 Generated with [Claude Code](https://claude.ai/code) and reviewed by me
