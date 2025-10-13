# OpenAPI Export Tool

A CLI tool to export the OpenAPI specification from the MCP Registry Huma API.

## Quick Start

The easiest way to generate the OpenAPI specification is using the Makefile:

```bash
# Generate OpenAPI specification to docs/reference/api/openapi.yaml
make generate-openapi
```

This automatically:
- Builds the tool
- Sets the required JWT key (using the dev key from .env.example)
- Exports the OpenAPI 3.1 YAML spec

## Building

```bash
make openapi-export
```

This will create the binary at `./bin/openapi-export`.

## Direct Usage

```bash
openapi-export [flags]
```

### Flags

- `-format string` - Output format: `json`, `yaml`, `json3.0`, `yaml3.0` (default: `yaml`)
- `-o, -output string` - Output file path (default: stdout)
- `-version` - Display version information
- `-help` - Display help information

### Environment Variables

The tool requires the following environment variable to be set:

- `MCP_REGISTRY_JWT_PRIVATE_KEY` - A 32-byte Ed25519 seed in hex format

**Note**: For generating documentation, any valid 32-byte hex string works. The JWT key is only needed to satisfy the API initialization requirements - it's not actually used for OpenAPI generation. The Makefile uses the dev key from [.env.example](../../.env.example).

## Examples

### Using Make (Recommended)

Generate OpenAPI spec to docs/reference/api/openapi.yaml:
```bash
make generate-openapi
```

### Direct CLI Usage

Export OpenAPI 3.1 YAML to stdout:
```bash
MCP_REGISTRY_JWT_PRIVATE_KEY=bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c \
  ./bin/openapi-export
```

Export OpenAPI 3.1 JSON to stdout:
```bash
MCP_REGISTRY_JWT_PRIVATE_KEY=bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c \
  ./bin/openapi-export -format json
```

Export to a custom file:
```bash
MCP_REGISTRY_JWT_PRIVATE_KEY=bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c \
  ./bin/openapi-export -o my-openapi.yaml
```

Export OpenAPI 3.0 JSON to file (for compatibility with older tools):
```bash
MCP_REGISTRY_JWT_PRIVATE_KEY=bb2c6b424005acd5df47a9e2c87f446def86dd740c888ea3efb825b23f7ef47c \
  ./bin/openapi-export -format json3.0 -o spec.json
```

## Output Formats

- **json** - OpenAPI 3.1 in JSON format
- **yaml** - OpenAPI 3.1 in YAML format (default)
- **json3.0** - OpenAPI 3.0.3 in JSON format (downgraded for compatibility)
- **yaml3.0** - OpenAPI 3.0.3 in YAML format (downgraded for compatibility)

## Use Cases

This tool is useful for:

- Generating OpenAPI specifications for SDK generation (e.g., using [oapi-codegen](https://github.com/deepmap/oapi-codegen))
- API documentation generation
- Contract testing
- Importing into API tools like Postman, Insomnia, or Swagger Editor
- CI/CD pipelines that need the OpenAPI spec
