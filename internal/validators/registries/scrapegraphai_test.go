package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateScrapeGraphAI_RealPackages(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		version      string
		registryURL  string
		serverName   string
		expectError  bool
		errorMessage string
	}{
		{
			name:         "empty package identifier should fail",
			packageName:  "",
			version:      "1.0.0",
			registryURL:  model.RegistryURLPyPI,
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for ScrapeGraphAI packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "scrapegraph-mcp",
			version:      "",
			registryURL:  model.RegistryURLPyPI,
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for ScrapeGraphAI packages",
		},
		{
			name:         "non-existent package on PyPI should fail",
			packageName:  generateRandomPackageName(),
			version:      "1.0.0",
			registryURL:  model.RegistryURLPyPI,
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real PyPI package without MCP server name should fail",
			packageName:  "requests",
			version:      "2.31.0",
			registryURL:  model.RegistryURLPyPI,
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:        "real PyPI package with server name in README should pass",
			packageName: "time-mcp-pypi",
			version:     "1.0.6",
			registryURL: model.RegistryURLPyPI,
			serverName:  "io.github.domdomegg/time-mcp-pypi",
			expectError: false,
		},
		{
			name:         "invalid registry URL should fail",
			packageName:  "scrapegraph-mcp",
			version:      "1.0.0",
			registryURL:  "https://invalid-registry.com",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "registry type and base URL do not match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType:    model.RegistryTypeScrapeGraphAI,
				RegistryBaseURL: tt.registryURL,
				Identifier:      tt.packageName,
				Version:         tt.version,
			}

			err := registries.ValidateScrapeGraphAI(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMessage != "" {
					assert.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
