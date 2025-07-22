# MCP Registry FAQ

These questions come up often in discussions about the MCP Registry. If you have a question that isn't answered here, please start a discussion on the [MCP Registry Discussions page](https://github.com/modelcontextprotocol/registry/discussions).

## General Questions

### What is the MCP Registry?

The MCP Registry is the official centralized metadata repository for publicly-accessible MCP servers. It provides:

- A single place for server creators to publish metadata about their servers
- A REST API for MCP clients and aggregators to discover available servers
- Standardized installation and configuration information
- Namespace management through DNS verification

### What is the difference between "Official MCP Registry", "MCP Registry", "MCP registry", "MCP Registry API", etc?

There are four underlying concepts:
- "MCP Server Registry API" (or "MCP Registry API"): The OpenAPI specification defined in [openapi.yaml](./server-registry-api/openapi.yaml). This is a reusable API specification that anyone building any sort of "MCP server registry" should consider adopting / aligning with.
- "Official MCP Registry" (or "MCP Registry"): The application that lives at `https://registry.modelcontextprotocol.io`. This registry currently only catalogs MCP servers, but may be extended in the future to also catalog MCP client/host apps and frameworks.
- "Official MCP Registry API": The REST API that lives at `https://registry.modelcontextprotocol.io/api`, with an OpenAPI specification defined at [official-registry-openapi.yaml](./server-registry-api/official-registry-openapi.yaml)
- "MCP server registry" (or "MCP registry"): A third party, likely commercial, implementation of the MCP Server Registry API or derivative specification.

### Is the MCP Registry a package registry?

No. The MCP Registry stores metadata about MCP servers and references to where they're hosted (npm, PyPI, NuGet, Docker Hub, etc.), but does not host the actual source code or packages.

### Who should use the MCP Registry directly?

The registry is designed for programmatic consumption by:

- MCP client applications (Claude Desktop, Cline, etc.)
- Server aggregators (Smithery, PulseMCP, etc.)
- NOT individual end-users (they should use MCP clients or aggregator UIs)

### Will there be a UI for browsing servers?

A UI is planned as a future milestone after the initial API launch, but is not part of the MVP.

## Publishing Servers

### How do I publish my MCP server?

Servers are published by submitting a `server.json` file through our CLI tool. The process requires:

1. GitHub authentication
2. A public GitHub repository (even for closed-source servers - just for the metadata)
3. Your server package published to a supported registry (npm, PyPI, NuGet, Docker Hub, etc.)
4. Optional: DNS verification for custom namespacing

### What namespaces are available?

- **With DNS verification**: `com.yourcompany/server-name` (reverse DNS notation)
- **Without DNS verification**: `io.github.yourusername/server-name`
- DNS verification is done via TXT records and enables authoritative namespacing

### Is open source required?

No. While open source code is encouraged, it is not required for either locally or remotely run servers.

### What package registries are supported?

- npm (Node.js packages)
- PyPI (Python packages)
- NuGet.org (.NET packages)
- GitHub Container Registry (GHCR)

More can be added as the community desires; feel free to open an issue if you are interested in building support for another registry.

### Can I publish multiple versions?

Yes, versioning is supported:

- Each version gets its own immutable metadata
- Version bumps are required for updates
- Old versions remain accessible for compatibility
- The registry tracks which version is "latest"

### How do I update my server metadata?

Submit a new `server.json` with an incremented version number. Once published, version metadata is immutable (similar to npm).

### Can I delete/unpublish my server?

A reverse-publication flow is planned to allow quick deletion of accidentally published data.

## Security & Trust

### How do I know a server is from the claimed organization?

DNS verification ensures namespace ownership. For example:

- `com.microsoft/server` requires DNS verification of microsoft.com
- `io.github.username/server` is tied to a GitHub account or GitHub organization

### What about typosquatting?

The registry implements (or is slated to soon implement):

- Automatic blocking of names within a certain edit distance of existing servers
- Community reporting mechanisms

### Is there security scanning?

The MVP delegates security to the underlying package registries. Future iterations may include:

- Vulnerability scanning
- Dependency analysis

### How is spam prevented?

- GitHub authentication requirement
- Rate limiting (e.g., 10 new servers per user per day)
- Character limits and regex validation on free-form fields
- Potential AI-based spam detection
- Community reporting and admin blacklisting capabilities

## API & Integration

### How often should I poll the registry?

Recommended polling frequency:

- `/servers` endpoint: once per hour
- `/servers/:id` endpoint: once per version (results are immutable)
- Design assumes CDN caching between registry and consumers

### Will there be webhooks?

Not in the initial MVP, but the architecture supports adding webhooks for update notifications in the future.

### Can I run my own registry instance?

While the API shapes and data formats are designed for reuse, the registry implementation itself is not designed for self-hosting. Organizations needing private registries should:

- Implement the same API shape
- Use the same `server.json` format
- Potentially mirror/filter the official registry data

## Operations & Maintenance

### What's the expected reliability?

- 24-hour+ downtime tolerance is acceptable, so you shouldn't rely on the registry being always available.
- No direct end-user impact (consumers cache data)
- Relies on CDN for actual availability
- Volunteer maintainer model (no formal on-call)

### How are DNS records verified?

- Initial verification via TXT records
- Daily re-verification to catch domain transfers
- Publishing blocked if verification fails
- Historical packages remain available with warnings

### What about GitHub repository transfers?

The registry tracks GitHub repository IDs (not just URLs) to detect transfers. Daily checks update metadata if repositories move.

### How is namespace transfer handled?

Namespace transfers (e.g., when someone leaves a company) are handled through the DNS verification system and GitHub organization membership.

## Future Considerations

### Will download counts be tracked?

Download count tracking is being considered as a quality signal, but must be designed carefully to not impact CDN caching.

### What about tags or categories?

Categorization and curation are intentionally left to consumers (MCP clients and aggregators) to avoid maintenance burden and subjective decisions.

### Will there be quality metrics?

Quality assessment is explicitly out of scope for the official registry. This is delegated to:

- MCP clients (for their specific use cases)
- Third-party aggregators
- Community reviews on external platforms

### What about internationalization?

Internationalization is a future consideration but not part of the MVP.

### Will private registries be supported?

The registry design (API shapes, data formats) is intended to be reusable for private deployments, but the official registry will only host public servers.
