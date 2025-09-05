# Namespacing and Security

Namespacing prevents name squatting, makes impersonation and typo squatting much harder, and provides attribution through domain-based identity.

For practical steps on how to authenticate for different namespaces, see the [publishing guide](../guides/publishing/publish-server.md#authenticate).

## Namespace Format

All server names follow reverse-DNS format. Examples:
- `io.github.alice/weather-server` - GitHub user `alice`
- `com.acme/internal-tool` - Company domain `acme.com`
- `org.nonprofit.research/data-analyzer` - Subdomain `research.nonprofit.org`

## Security Model

### Ownership Verification

Publishing to a namespace requires proving you control the corresponding identity:

**GitHub namespaces** (`io.github.*`):
- OAuth login to GitHub account/organization
- OIDC tokens in GitHub Actions workflows

**Domain namespaces** (`com.company.*`):
- DNS verification: TXT record at `_mcp-registry.company.com`
- HTTP verification: File at `https://company.com/.well-known/mcp-registry-auth`

## Namespace Scoping

Different authentication methods grant different namespace access:

**GitHub OAuth/OIDC**:
- `io.github.username/*` (for personal accounts)
- `io.github.orgname/*` (for organizations)

**DNS verification**:
- `com.domain/*` and `com.domain.*/*` (domain + all subdomains)

**HTTP verification**:
- `com.domain/*` only (exact domain, no subdomains)

## Limitations

**Domain ownership changes**: If someone loses/sells a domain, they lose publishing rights. Similarly if someone gains a domain, they gain publishing rights.
**Package validation**: Registry validates namespace ownership, but actual packages may still be malicious etc.
