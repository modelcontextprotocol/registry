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
- "MCP Server Registry API" (or "MCP Registry API"): The OpenAPI specification defined in [openapi.yaml](./api/openapi.yaml). This is a reusable API specification that anyone building any sort of "MCP server registry" should consider adopting / aligning with.
- "Official MCP Registry" (or "MCP Registry"): The application that lives at `https://registry.modelcontextprotocol.io`. This registry currently only catalogs MCP servers, but may be extended in the future to also catalog MCP client/host apps and frameworks.
- "Official MCP Registry API": The REST API served at `https://registry.modelcontextprotocol.io`, which is a superset of the MCP Registry API. Its OpenAPI specification can be downloaded from [https://registry.modelcontextprotocol.io/openapi.yaml](https://registry.modelcontextprotocol.io/openapi.yaml)
- "MCP server registry" (or "MCP registry"): A third party, likely commercial, implementation of the MCP Server Registry API or derivative specification.

### Is the MCP Registry a package registry?

No. The MCP Registry stores metadata about MCP servers and references to where they're hosted (npm, PyPI, NuGet, Docker Hub, etc.), but does not host the actual source code or packages.

### Who should use the MCP Registry directly?

The registry is primarily designed for programmatic consumption by subregistries (Smithery, PulseMCP, Docker Hub, Anthropic, GitHub, etc.). It is NOT currently intended for individual clients or end-users (they should use subregistries).

### Will there be feature X?

See [roadmap.md](../explanations/roadmap.md).

## Publishing Servers

### How do I publish my MCP server?

See the [publisher README](../../cmd/publisher/README.md)

### What namespaces are available?

- **With GitHub verification**: `io.github.yourusername/server-name`, `io.github.yourorg/server-name`
- **With DNS verification**: `com.yourcompany.*`, `com.yourcompany.*/*`
- **With HTTP verification**: `com.yourcompany/*`

### Is open source required?

No. While open source code is encouraged, it is not required for either locally or remotely run servers.

### What package registries are supported?

- npm (Node.js packages)
- PyPI (Python packages)
- NuGet.org (.NET packages)
- GitHub Container Registry (GHCR)
- Docker Hub

More can be added as the community desires; feel free to open an issue if you are interested in building support for another registry.

### Can I publish multiple versions?

Yes, versioning is supported:

- Each version gets its own immutable metadata
- Version strings must be unique for each server
- Old versions remain accessible for compatibility
- The registry tracks which version is "latest" based on semantic version ordering when possible

### How do I update my server metadata?

Submit a new `server.json` with a unique version string. Once published, version metadata is immutable (similar to npm).

### What version format should I use?

The registry accepts any version string up to 255 characters, but we recommend:

- **SHOULD use semantic versioning** (e.g., "1.0.2", "2.1.0-alpha") for predictable ordering
- **SHOULD align with package versions** to reduce confusion
- **MAY use prerelease labels** (e.g., "1.0.0-1") for registry-specific versions

The registry attempts to parse versions as semantic versions for proper ordering. Non-semantic versions are allowed but will be ordered by publication timestamp. Version ranges (e.g., `^1.2.3`, `~1.2.3`, `>=1.2.3`, `1.x`, `1.*`) are rejected; publish a specific version instead. See the [versioning guide](../explanations/versioning.md) for detailed guidance.

### Can I add custom metadata when publishing?

Yes, extensions under the `x-publisher` property are preserved when publishing to the registry. This allows you to include custom metadata specific to your publishing process.

### Can I delete/unpublish my server?

At time of last update, this was open for discussion in [#104](https://github.com/modelcontextprotocol/registry/issues/104).

### Can I publish a private server?

Private servers are those that are only accessible to a narrow set of users. For example, servers published on a private network (like `mcp.acme-corp.internal`) or on private package registries (e.g. `npx -y @acme/mcp --registry https://artifactory.acme-corp.internal/npm`).

These are generally not supported on the official MCP registry, which is designed for publicly accessible MCP servers.

If you want to publish private servers we recommend you host your own MCP subregistry, and add them there.

## Security & Trust

### How do I know a server is from the claimed organization?

DNS verification ensures namespace ownership. For example:

- `com.microsoft/server` requires DNS verification of microsoft.com
- `io.github.name/server` is tied to a GitHub account or GitHub organization `name`

### Is there security scanning?

The MVP delegates security scanning to:
- underlying package registries; and
- subregistries

### How is spam prevented?

- Namespace authentication requirements
- Character limits and regex validation on free-form fields
- Manual takedown of spam or malicious servers

In future we might explore:
- Stricter rate limiting (e.g., 10 new servers per user per day)
- Potential AI-based spam detection
- Community reporting and admin blacklisting capabilities

## API & Integration

### How often should I poll the registry?

Recommended polling frequency:

- `/servers` endpoint: once per hour
- `/servers/:id` endpoint: once per version (results are immutable)
- Design assumes CDN caching between registry and consumers

Also see [#291](https://github.com/modelcontextprotocol/registry/issues/291), which might mean the above can be more regular.

### Will there be webhooks?

Not in the initial MVP, but the architecture supports adding webhooks for update notifications in the future.

### Can I run my own registry instance?

Yes! The API shapes and data formats are intentionally designed for reuse by subregistries. Organizations needing private registries should:

- Implement the same API shape
- Use the same `server.json` format
- Potentially mirror/filter the official registry data

### Can I extend the registry API?

Yes, we support `x-com.example` style extensions in a bunch of places - see the official MCP registry API spec. This can be used to add annotations to many objects, e.g. add security scanning details, enrich package metadata, etc.

If you have a use case that can't be addressed here, raise a GitHub issue!

### Can I use the code here to run my own registry instance?

The registry implementation here is not designed for self-hosting, but you're welcome to try to use it/fork it as necessary. Note that this is not an intended use, and the registry maintainers cannot provide any support for this at this time.

## Operations & Maintenance

### What's the expected reliability?

- This is a community maintained project without full time staffing. You should therefore expect downtime periods of up to 1 business day. No strict guarantees are provided. (Also see discussion in [#150](https://github.com/modelcontextprotocol/registry/issues/150))
- Ideally clients should use subregistries with higher availability guarantees, to avoid direct end-user impact (as subregistries can cache data).

### What if I need to report a spam or malicious server?

1. Report it as abuse to the underlying package registry (e.g. NPM, PyPi, DockerHub, etc.); and
2. Raise a GitHub issue on the registry repo with a title beginning `Abuse report: `

### What if I need to report a security vulnerability in the registry itself?

Follow [the MCP community SECURITY.md](https://github.com/modelcontextprotocol/.github/blob/main/SECURITY.md).
