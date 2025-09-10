package registries_test

import (
	"context"
	"strings"
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
			name:         "non-existent image should fail (Docker Hub)",
			packageName:  generateRandomImageName(),
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
			registryURL:  model.RegistryURLDocker,
		},
		{
			name:         "real image without MCP annotation should fail (Docker Hub)",
			packageName:  "nginx", // Popular image without MCP annotation
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
			registryURL:  model.RegistryURLDocker,
		},
		{
			name:         "real image with specific tag without MCP annotation should fail (Docker Hub)",
			packageName:  "redis",
			version:      "7-alpine", // Specific tag
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
			registryURL:  model.RegistryURLDocker,
		},
		{
			name:         "namespaced image without MCP annotation should fail (Docker Hub)",
			packageName:  "hello-world", // Simple image for testing
			version:      "latest",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required annotation",
			registryURL:  model.RegistryURLDocker,
		},
		{
			name:        "real image with correct MCP annotation should pass (Docker Hub)",
			packageName: "domdomegg/airtable-mcp-server",
			version:     "1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server", // This should match the annotation
			expectError: false,
			registryURL: model.RegistryURLDocker,
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("Skipping OCI registry tests because we keep hitting DockerHub rate limits")

			registryURL := tt.registryURL
			if registryURL == "" {
				registryURL = model.RegistryURLDocker // default to Docker Hub for backward compatibility
			}

			pkg := model.Package{
				RegistryType:    model.RegistryTypeOCI,
				RegistryBaseURL: registryURL,
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

func TestValidateOCI_GHCR_Integration(t *testing.T) {
	ctx := context.Background()
	
	// Test that GHCR registry is properly configured and accessible
	t.Run("GHCR registry configuration", func(t *testing.T) {
		pkg := model.Package{
			RegistryType:    model.RegistryTypeOCI,
			RegistryBaseURL: model.RegistryURLGHCR,
			Identifier:      "testuser/testimage",
			Version:         "latest",
		}

		// This should fail due to image not existing, but NOT due to registry validation
		err := registries.ValidateOCI(ctx, pkg, "com.example/test")
		assert.Error(t, err)
		
		// Should NOT contain registry validation errors
		assert.NotContains(t, err.Error(), "registry type and base URL do not match")
		assert.NotContains(t, err.Error(), "unsupported registry")
		
		// Should contain image-not-found or similar error (proving we reached GHCR)
		errStr := err.Error()
		hasValidGHCRError := strings.Contains(errStr, "not found") || 
		                    strings.Contains(errStr, "401") || 
		                    strings.Contains(errStr, "403") ||
		                    strings.Contains(errStr, "missing required annotation")
		assert.True(t, hasValidGHCRError, "Expected GHCR-related error, got: %s", errStr)
	})
	
	t.Run("GHCR with github MCP server image", func(t *testing.T) {
		// Test the real GitHub MCP server image
		pkg := model.Package{
			RegistryType:    model.RegistryTypeOCI,
			RegistryBaseURL: model.RegistryURLGHCR,
			Identifier:      "github/github-mcp-server", // Correct GHCR identifier format
			Version:         "main",
		}

		err := registries.ValidateOCI(ctx, pkg, "io.github.github/github-mcp-server")
		
		// We expect this to fail due to missing MCP annotation, but that proves GHCR works
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "missing required annotation")
		
		// Should NOT fail due to registry connectivity issues
		assert.NotContains(t, err.Error(), "registry type and base URL do not match")
		assert.NotContains(t, err.Error(), "unsupported registry")
		assert.NotContains(t, err.Error(), "not found")
		
		t.Log("GHCR integration working - successfully validated image but found missing MCP annotation")
	})
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
