package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidatePyPI_RealPackages(t *testing.T) {
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
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for PyPI packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "mcp-server-example",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for PyPI packages",
		},
		{
			name:         "non-existent package should fail",
			packageName:  generateRandomPackageName(),
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real package without MCP server name should fail",
			packageName:  "requests", // Popular package without MCP server name in keywords/description/URLs
			version:      "2.31.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "ownership validation failed",
		},
		{
			name:         "real package with different server name should fail",
			packageName:  "numpy", // Another popular package
			version:      "1.25.2",
			serverName:   "com.example/completely-different-name",
			expectError:  true,
			errorMessage: "ownership validation failed", // Will fail because numpy doesn't have this server name
		},
		{
			name:        "real package with server name in README should pass",
			packageName: "time-mcp-pypi",
			version:     "1.0.6",
			serverName:  "io.github.domdomegg/time-mcp-pypi",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypePyPI,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidatePyPI(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
