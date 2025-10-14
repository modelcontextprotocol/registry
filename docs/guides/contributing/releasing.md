# Release Guide

## Creating a Release

1. **Go to GitHub**: Navigate to https://github.com/modelcontextprotocol/registry/releases
2. **Click "Draft a new release"**
3. **Choose a tag**: Click "Choose a tag" and type a new semantic version that follows the last one available (e.g., `v1.0.0`)
5. **Generate notes**: Click "Generate release notes" to auto-populate the name and description
6. **Publish**: Click "Publish release"

The release workflow will automatically:
- Build binaries for 6 platforms (Linux, macOS, Windows × amd64, arm64)
- Create and push Docker images with `:latest` and `:X.Y.Z` tags (note: no 'v' prefix)
- Attach all artifacts to the GitHub release
- Generate checksums and signatures

## After Release

- Docker images will be available at:
  - `ghcr.io/modelcontextprotocol/registry:latest` - Latest stable release
  - `ghcr.io/modelcontextprotocol/registry:X.Y.Z` - Specific release version (note: no 'v' prefix)
- Binaries can be downloaded from the GitHub release page

**Note:** Releases do not automatically deploy to production. To deploy a release to production:

1. Update `mcp-registry:imageTag` in `deploy/Pulumi.gcpProd.yaml` to the desired version (e.g., `1.2.3` - note: no 'v' prefix)
2. Commit and push the change to the `main` branch
3. The [deploy-production.yml](../../../.github/workflows/deploy-production.yml) workflow will automatically trigger and deploy the specified version

See the [deployment documentation](../../../deploy/README.md) for more details.

## Docker Image Tags

The registry publishes different Docker image tags for different use cases:

- **`:latest`** - Latest stable release (updated only on releases)
- **`:X.Y.Z`** - Specific release versions (e.g., `:1.0.0` - note: no 'v' prefix)
- **`:main`** - Rolling tag updated on every push to main branch (continuous deployment)
- **`:main-YYYYMMDD-sha`** - Specific development builds from main branch

**Note:** Git release tags include the 'v' prefix (e.g., `v1.0.0`), but Docker image tags follow the standard Docker convention and do not include the 'v' prefix (e.g., `1.0.0`).

## Versioning

We use semantic versioning (SemVer):
- `v1.0.0` - Major release with breaking changes
- `v1.1.0` - Minor release with new features
- `v1.0.1` - Patch release with bug fixes