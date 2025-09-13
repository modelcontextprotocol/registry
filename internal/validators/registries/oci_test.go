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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("Skipping OCI registry tests because we keep hitting DockerHub rate limits")

			pkg := model.Package{
				RegistryType: model.RegistryTypeOCI,
				Identifier:   tt.packageName,
				Version:      tt.version,
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

func TestValidateOCI_GHCR(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		serverName   string
		registryURL  string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "GHCR: non-existent image should fail",
			packageName:  generateRandomImageName(),
			version:      "latest",
			serverName:   "com.example/test",
			registryURL:  model.RegistryURLGHCR,
			expectError:  true,
			errorMessage: "failed to authenticate with GitHub Container Registry", // GHCR returns 403 for non-existent repos
		},
		{
			name:         "GHCR: real image without MCP annotation should fail", 
			packageName:  "modelcontextprotocol/registry", // Registry's own image  
			version:      "latest",
			serverName:   "com.example/test",
			registryURL:  model.RegistryURLGHCR,
			expectError:  true,
			errorMessage: "not found", // Returns 404 as image is not on GHCR
		},
		{
			name:        "GHCR: test image with correct MCP annotation should pass",
			packageName: "nkapila6/mcp-local-rag",
			version:     "latest",
			serverName:  "io.github.nkapila6/mcp-local-rag", // Should match annotation
			registryURL: model.RegistryURLGHCR,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Running GHCR tests to verify functionality
			
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
