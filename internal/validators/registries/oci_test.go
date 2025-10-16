package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateOCI_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		serverName   string
		expectError  bool
		errorMessage string
		registryURL  string
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for OCI packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "test-image",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for OCI packages",
		},
		{
			name:         "both empty identifier and version should fail with identifier error first",
			packageName:  "",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for OCI packages",
		},
		{
			name:         "non-existent image should fail",
			packageName:  generateRandomImageName(),
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real image without MCP annotation should fail",
			packageName:  "nginx", // Popular image without MCP annotation
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
		},
		{
			name:         "real image with specific tag without MCP annotation should fail",
			packageName:  "redis",
			version:      "7-alpine", // Specific tag
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
		},
		{
			name:         "namespaced image without MCP annotation should fail",
			packageName:  "hello-world", // Simple image for testing
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
		},
		{
			name:        "real image with correct MCP annotation should pass",
			packageName: "domdomegg/airtable-mcp-server",
			version:     "1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server", // This should match the annotation
			expectError: false,
		},
		{
			name:         "GHCR image without MCP annotation should fail",
			packageName:  "actions/runner", // GitHub's action runner image (real image without MCP annotation)
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
			registryURL:  model.RegistryURLGHCR,
		},
		{
			name:         "real GHCR image without MCP annotation should fail",
			packageName:  "github/github-mcp-server", // Real GitHub MCP server image
			version:      "main",
			serverName:   "io.github.github/github-mcp-server",
			expectError:  true,
			errorMessage: "missing required annotation", // Image exists but lacks MCP annotation
			registryURL:  model.RegistryURLGHCR,
		},
		{
			name:        "GHCR image with correct MCP annotation should pass",
			packageName: "nkapila6/mcp-local-rag", // Real MCP server with proper annotation
			version:     "latest",
			serverName:  "io.github.nkapila6/mcp-local-rag",
			expectError: false,
			registryURL: model.RegistryURLGHCR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("Skipping OCI registry tests because we keep hitting DockerHub rate limits")

			pkg := model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: tt.registryURL,
				Identifier:      tt.packageName,
				Version:         tt.version,
			}

			err := registries.ValidateOCI(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateOCI_UnsupportedRegistry(t *testing.T) {
	ctx := context.Background()

	// Test with unsupported registry in canonical reference format
	pkg := model.Package{
		RegistryType: model.RegistryTypeOCI,
		Identifier:   "unsupported-registry.com/test/image:latest",
	}

	err := registries.ValidateOCI(ctx, pkg, "com.example/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "registry type and base URL do not match")
	assert.Contains(t, err.Error(), "Expected: https://docker.io or https://ghcr.io")
}

func TestValidateOCI_SupportedRegistries(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		identifier string
		expected   bool
	}{
		{
			name:       "Docker Hub should be supported",
			identifier: "docker.io/test/image:latest",
			expected:   true,
		},
		{
			name:       "GHCR should be supported",
			identifier: "ghcr.io/test/image:latest",
			expected:   true,
		},
		{
			name:       "Unsupported registry should fail",
			identifier: "quay.io/test/image:latest",
			expected:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.identifier,
			}

			err := registries.ValidateOCI(ctx, pkg, "com.example/test")
			if tt.expected {
				// Should not fail immediately on registry validation
				// (may fail later due to network/image not found, but not due to unsupported registry)
				if err != nil {
					assert.NotContains(t, err.Error(), "registry type and base URL do not match")
				}
			} else {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "registry type and base URL do not match")
			}
		})
	}
}

func TestValidateOCI_RejectsOldFormat(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		pkg          model.Package
		errorMessage string
	}{
		{
			name: "OCI package with registryBaseUrl should be rejected",
			pkg: model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "docker.io/test/image:latest",
			},
			errorMessage: "OCI packages must not have 'registryBaseUrl' field",
		},
		{
			name: "OCI package with version field should be rejected",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
				Version:      "1.0.0",
			},
			errorMessage: "OCI packages must not have 'version' field",
		},
		{
			name: "OCI package with both old format fields should fail on registryBaseUrl first",
			pkg: model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: "https://docker.io",
				Identifier:      "test/image",
				Version:         "1.0.0",
			},
			errorMessage: "OCI packages must not have 'registryBaseUrl' field",
		},
		{
			name: "OCI package with canonical format should pass old format validation",
			pkg: model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   "docker.io/test/image:latest",
			},
			errorMessage: "", // Should pass old format check (will fail later due to image not existing)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := registries.ValidateOCI(ctx, tt.pkg, "com.example/test")

			if tt.errorMessage != "" {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else if err != nil {
				// Should not fail with old format error (may fail with other errors like image not found)
				assert.NotContains(t, err.Error(), "must not have 'registryBaseUrl'")
				assert.NotContains(t, err.Error(), "must not have 'version'")
			}
		})
	}
}
