# Releases

## Creating a Release

1. Create a new release on [GitHub](https://github.com/modelcontextprotocol/registry/releases) with a semver tag (e.g., `v1.3.9`)
2. This triggers the [release workflow](.github/workflows/release.yml) which:
   - Builds `registry` and `mcp-publisher` binaries for all platforms using GoReleaser
   - Signs artifacts with cosign
   - Generates SBOMs
   - Builds and pushes container images to `ghcr.io/modelcontextprotocol/registry`

## Deploying to Production

1. Update `deploy/Pulumi.gcpProd.yaml` (e.g., through a PR or by pushing directly to main):
   ```yaml
   mcp-registry:imageTag: 1.3.9
   ```
2. The [deploy-production workflow](.github/workflows/deploy-production.yml) automatically deploys

## Staging

Staging auto-deploys from `main` via [deploy-staging.yml](.github/workflows/deploy-staging.yml). It always runs the latest `main` branch code.

## Rollback

To rollback production, update `deploy/Pulumi.gcpProd.yaml` to the previous version and push.

**Note:** Rollbacks may not work as expected if the release included database migrations, since migrations are not automatically reversed.
