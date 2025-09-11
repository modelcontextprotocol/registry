package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestRegistryConfig(t *testing.T) {
	tests := []struct {
		name           string
		registryURL    string
		expectError    bool
		expectedAPIURL string
	}{
		{
			name:           "Docker Hub registry should work",
			registryURL:    model.RegistryURLDocker,
			expectError:    false,
			expectedAPIURL: "https://registry-1.docker.io",
		},
		{
			name:           "GHCR registry should work",
			registryURL:    model.RegistryURLGHCR,
			expectError:    false,
			expectedAPIURL: "https://ghcr.io",
		},
		{
			name:        "Unknown registry should fail",
			registryURL: "https://unknown-registry.com",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := registries.GetRegistryConfig(tt.registryURL)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.registryURL, config.RegistryURL)
				assert.Equal(t, tt.expectedAPIURL, config.APIBaseURL)
				assert.NotNil(t, config.AuthFunc)
			}
		})
	}
}

func TestValidateOCI_RealPackages(t *testing.T) {
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
			name:         "non-existent image should fail",
			packageName:  generateRandomImageName(),
			version:      "latest",
			serverName:   "com.example/test",
			registryURL:  model.RegistryURLDocker,
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real image without MCP annotation should fail",
			packageName:  "nginx", // Popular image without MCP annotation
			version:      "latest",
			serverName:   "com.example/test",
			registryURL:  model.RegistryURLDocker,
			expectError:  true,
			errorMessage: "missing required annotation",
		},
		{
			name:         "real image with specific tag without MCP annotation should fail",
			packageName:  "redis",
			version:      "7-alpine", // Specific tag
			serverName:   "com.example/test",
			registryURL:  model.RegistryURLDocker,
			expectError:  true,
			errorMessage: "missing required annotation",
		},
		{
			name:         "namespaced image without MCP annotation should fail",
			packageName:  "hello-world", // Simple image for testing
			version:      "latest",
			serverName:   "com.example/test",
			registryURL:  model.RegistryURLDocker,
			expectError:  true,
			errorMessage: "missing required annotation",
		},
		{
			name:        "real image with correct MCP annotation should pass",
			packageName: "domdomegg/airtable-mcp-server",
			version:     "1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server", // This should match the annotation
			registryURL: model.RegistryURLDocker,
			expectError: false,
		},
		{
			name:        "GHCR image with correct MCP annotation should pass",
			packageName: "nkapila6/mcp-local-rag",
			version:     "latest",
			serverName:  "io.github.nkapila6/mcp-local-rag", // This should match the annotation
			registryURL: model.RegistryURLGHCR,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Skip("Skipping OCI registry tests because we keep hitting DockerHub rate limits")

			pkg := model.Package{
				RegistryType:    model.RegistryTypeOCI,
				Identifier:      tt.packageName,
				Version:         tt.version,
				RegistryBaseURL: tt.registryURL,
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
