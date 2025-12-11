# Schema Files

This directory contains JSON Schema files that are embedded into the Go binary (using the `go:embed` directive) for runtime validation.

## How Schema Files Get Here

Schema files are automatically synced from the [modelcontextprotocol/static](https://github.com/modelcontextprotocol/static) repository via the GitHub Actions workflow `.github/workflows/sync-schema.yml`.

The workflow:
1. Checks out the `modelcontextprotocol/static` repository
2. Copies all versioned schema files from `static-repo/schemas/*/server.schema.json`
3. Saves them here as `{version}.json` (e.g., `2025-10-17.json`)
4. Automatically commits and pushes any new or updated schemas

**Do not manually edit files in this directory** - they are managed by the sync workflow.

## Usage

These schema files are embedded into the Go binary using the `go:embed` directive for offline schema validation. The embedded schemas are used by the validation code in `internal/validators/schema.go` to validate `server.json` files against their specified schema version.

## File Naming

Files are named `{YYYY-MM-DD}.json` where the date corresponds to the schema version (e.g., `2025-10-17.json`). This matches the version in the schema's `$id` field.
