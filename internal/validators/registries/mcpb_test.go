package registries_test

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/validators/registries"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateMCPB(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		packageName  string
		serverName   string
		expectError  bool
		errorMessage string
	}{
		{
			name:        "valid MCPB package should pass",
			packageName: "https://github.com/domdomegg/airtable-mcp-server/releases/download/v1.7.2/airtable-mcp-server.mcpb",
			serverName:  "io.github.domdomegg/airtable-mcp-server",
			expectError: false,
		},
		{
			name:        "valid MCPB package should pass",
			packageName: "https://github.com/microsoft/playwright-mcp/releases/download/v0.0.36/playwright-mcp-extension-v0.0.36.zip",
			serverName:  "com.microsoft/playwright-mcp",
			expectError: false,
		},
		{
			name:         "valid MCPB package with .mcpb extension should fail accessibility check",
			packageName:  "https://github.com/example/server/releases/download/v1.0.0/server.mcpb",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not publicly accessible",
		},
		{
			name:         "invalid URL without mcp anywhere should fail",
			packageName:  "https://github.com/example/server/releases/download/v1.0.0/server.tar.gz",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "URL must contain 'mcp'",
		},
		{
			name:         "invalid URL format should fail",
			packageName:  "not://a valid url for mcpb!",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "invalid MCPB package URL",
		},
		{
			name:         "non-existent file should fail accessibility check",
			packageName:  "https://github.com/nonexistent/repo/releases/download/v1.0.0/mcp-server.tar.gz",
			serverName:   "com.example/test",
			expectError:  true,
			errorMessage: "not publicly accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeMCPB,
				Identifier:   tt.packageName,
			}

			err := registries.ValidateMCPB(ctx, pkg, tt.serverName)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
