package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateNPM_RealPackages(t *testing.T) {
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
			errorMessage: "package identifier is required for NPM packages",
		},
		{
			name:         "empty package version should fail",
			packageName:  "test-package",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package version is required for NPM packages",
		},
		{
			name:         "both empty identifier and version should fail with identifier error first",
			packageName:  "",
			version:      "",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "package identifier is required for NPM packages",
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
			name:         "real package without mcpName should fail",
			packageName:  "express", // Popular package without mcpName field
			version:      "4.18.2",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "lodash", // Another popular package
			version:      "4.17.21",
			serverName:   "com.example/completely-different-name",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package without mcpName should fail",
			packageName:  "airtable-mcp-server",
			version:      "1.5.0",
			serverName:   "io.github.domdomegg/airtable-mcp-server",
			expectError:  true,
			errorMessage: "missing required 'mcpName' field",
		},
		{
			name:         "real package with incorrect mcpName should fail",
			packageName:  "airtable-mcp-server",
			version:      "1.7.2",
			serverName:   "io.github.not-domdomegg/airtable-mcp-server",
			expectError:  true,
			errorMessage: "Expected mcpName 'io.github.not-domdomegg/airtable-mcp-server', got 'io.github.domdomegg/airtable-mcp-server'",
		},
		{
			name:        "real package with correct mcpName should pass",
			packageName: "airtable-mcp-server",
			version:     "1.7.2",
			serverName:  "io.github.domdomegg/airtable-mcp-server",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeNPM,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateNPM(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
