package registries

import (
	"context"

	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ValidateMavenAt is a test-only entry point that exposes the Maven validator
// against an arbitrary base URL (so tests can use httptest servers without
// going through the Maven Central allowlist check).
//
// This file ends in _test.go so the symbol is excluded from production builds
// while remaining accessible to the registries_test package.
func ValidateMavenAt(ctx context.Context, baseURL string, pkg model.Package, serverName string) error {
	return validateMavenAgainst(ctx, baseURL, pkg, serverName)
}
