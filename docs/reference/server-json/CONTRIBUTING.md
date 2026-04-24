# Contributing to server.json Schema

This document describes the process for making and releasing changes to the `server.json` schema.

## Making Changes

1. **Modify the OpenAPI spec**: Edit `docs/reference/api/openapi.yaml` with your schema changes. The `ServerDetail` component defines the server.json structure.

2. **Regenerate the schema**: Run `make generate-schema` to update `server.schema.json` from the OpenAPI spec.

3. **Update the changelog**: Add your changes to the "Draft (Unreleased)" section in `CHANGELOG.md`.

4. **Open a PR**: Submit a pull request to this repository for review.

## Releasing Changes

When the draft changes are ready for release:

1. **Update the changelog**: Move changes from "Draft (Unreleased)" to a new dated section (e.g., `## 2025-XX-XX`).

2. **Update the schema URL**: Change the `$id` in the schema and the example URL in `openapi.yaml` from `draft` to the release date (e.g., `2025-XX-XX`).

3. **Merge the PR**: Get approval and merge the changes to main.

4. **Publish to static hosting**: Open a PR on [modelcontextprotocol/static](https://github.com/modelcontextprotocol/static/tree/main/schemas) to add the new versioned schema file. This "locks in" the released schema at its versioned URL.

## Schema Versioning

- **Draft schema**: `https://raw.githubusercontent.com/modelcontextprotocol/registry/main/docs/reference/server-json/draft/server.schema.json` - For in-progress changes, may change without notice.
- **Released schemas**: `https://static.modelcontextprotocol.io/schemas/YYYY-MM-DD/server.schema.json` - Stable, versioned by release date.
