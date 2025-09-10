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

	pkg := model.Package{
		RegistryType:    model.RegistryTypeOCI,
		RegistryBaseURL: "https://unsupported-registry.com",
		Identifier:      "test/image",
		Version:         "latest",
	}

	err := registries.ValidateOCI(ctx, pkg, "com.example/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "registry type and base URL do not match")
	assert.Contains(t, err.Error(), "Expected: https://docker.io or https://ghcr.io")
}

func TestValidateOCI_SupportedRegistries(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name        string
		registryURL string
		expected    bool
	}{
		{
			name:        "Docker Hub should be supported",
			registryURL: model.RegistryURLDocker,
			expected:    true,
		},
		{
			name:        "GHCR should be supported",
			registryURL: model.RegistryURLGHCR,
			expected:    true,
		},
		{
			name:        "Unsupported registry should fail",
			registryURL: "https://quay.io",
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: tt.registryURL,
				Identifier:      "test/image",
				Version:         "latest",
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
