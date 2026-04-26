package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateCargo_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		serverName   string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "0.1.0",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for Cargo packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "rust-faf-mcp",
			version:      "",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "package version is required for Cargo packages",
		},
		{
			name:         "non-existent crate should fail",
			packageName:  generateRandomPackageName(),
			version:      "0.1.0",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "non-existent version of real crate should fail",
			packageName:  "serde",
			version:      "99.99.99-not-real",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real crate without mcp-name token should fail",
			packageName:  "serde", // most-downloaded crate; no MCP server claim
			version:      "1.0.219",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real crate with mismatched mcp-name should fail",
			packageName:  "tokio",
			version:      "1.40.0",
			serverName:   "io.github.example/completely-different-name",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "additional real crate without mcp-name (rand)",
			packageName:  "rand",
			version:      "0.9.0",
			serverName:   "io.github.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeCargo,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateCargo(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateCargo_RegistryBaseURLMismatch(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "different host", baseURL: "https://example.com"},
		{name: "trailing slash", baseURL: "https://crates.io/"},
		{name: "http (not https)", baseURL: "http://crates.io"},
		{name: "subdomain", baseURL: "https://www.crates.io"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType:    model.RegistryTypeCargo,
				RegistryBaseURL: tt.baseURL,
				Identifier:      "rust-faf-mcp",
				Version:         "0.2.2",
			}

			err := registries.ValidateCargo(ctx, pkg, "io.github.Wolfe-Jam/rust-faf-mcp")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "registry type and base URL do not match")
		})
	}
}

func TestValidateCargo_RejectsMCPBOnlyFields(t *testing.T) {
	ctx := context.Background()

	pkg := model.Package{
		RegistryType: model.RegistryTypeCargo,
		Identifier:   "rust-faf-mcp",
		Version:      "0.2.2",
		FileSHA256:   "0000000000000000000000000000000000000000000000000000000000000000",
	}

	err := registries.ValidateCargo(ctx, pkg, "io.github.Wolfe-Jam/rust-faf-mcp")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cargo packages must not have 'fileSha256' field")
}

// Server names follow io.github.OWNER/REPO and may contain dots, slashes,
// hyphens, underscores, and digits. None of these get HTML-escaped during
// README rendering, so substring match against the rendered HTML is reliable.
// These tests exercise format variations against a real crate that doesn't
// declare any mcp-name (serde) — every case fails ownership, but we verify
// the failure error preserves the exact server name unchanged.
func TestValidateCargo_ServerNameFormats(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		serverName string
	}{
		{name: "canonical io.github format", serverName: "io.github.Wolfe-Jam/rust-faf-mcp"},
		{name: "multiple hyphens", serverName: "io.github.example/multi-hyphen-test-name"},
		{name: "underscore", serverName: "io.github.example/snake_case_name"},
		{name: "numeric suffix", serverName: "io.github.example/server-v2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeCargo,
				Identifier:   "serde",
				Version:      "1.0.219",
			}

			err := registries.ValidateCargo(ctx, pkg, tt.serverName)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.serverName)
		})
	}
}
