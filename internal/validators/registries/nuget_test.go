package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateNuGet_RealPackages(t *testing.T) {
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
			name:         "non-existent package should fail",
			packageName:  generateRandomNuGetPackageName(),
			version:      "1.0.0",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real package without version should fail",
			packageName:  "Newtonsoft.Json",
			version:      "", // No version provided
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "requires a specific version",
		},
		{
			name:         "real package with non-existent version should fail",
			packageName:  "Newtonsoft.Json",
			version:      "999.999.999", // Version that doesn't exist
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not found",
		},
		{
			name:         "real package without mcp-name should fail",
			packageName:  "Newtonsoft.Json",
			version:      "13.0.3", // Popular version
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "missing required '<mcp-name>' element",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeNuGet,
				Identifier:   tt.packageName,
				Version:      tt.version,
			}

			err := registries.ValidateNuGet(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}