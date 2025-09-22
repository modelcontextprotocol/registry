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
		fileSHA256   string
		expectError  bool
		errorMessage string
	}{
		{
			name:        "valid MCPB package should pass",
			packageName: "https://github.com/domdomegg/airtable-mcp-server/releases/download/v1.7.2/airtable-mcp-server.mcpb",
			serverName:  "io.github.domdomegg/airtable-mcp-server",
			fileSHA256:  "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
			expectError: false,
		},
		{
			name:        "valid MCPB package should pass",
			packageName: "https://github.com/microsoft/playwright-mcp/releases/download/v0.0.36/playwright-mcp-extension-v0.0.36.zip",
			serverName:  "com.microsoft/playwright-mcp",
			fileSHA256:  "abc123ef4567890abcdef1234567890abcdef1234567890abcdef1234567890",
			expectError: false,
		},
		{
			name:         "MCPB package without file hash should fail",
			packageName:  "https://github.com/example/server/releases/download/v1.0.0/server.mcpb",
			serverName:   "com.example/test",
			fileSHA256:   "",
			expectError:  true,
			errorMessage: "must include a fileSha256 hash for integrity verification",
		},
		{
			name:         "non-existent .mcpb package should fail accessibility check",
			packageName:  "https://github.com/example/server/releases/download/v1.0.0/server.mcpb",
			serverName:   "com.example/test",
			fileSHA256:   "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
			expectError:  true,
			errorMessage: "not publicly accessible",
		},
		{
			name:         "invalid URL without mcp anywhere should fail",
			packageName:  "https://github.com/example/server/releases/download/v1.0.0/server.tar.gz",
			serverName:   "com.example/test",
			fileSHA256:   "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
			expectError:  true,
			errorMessage: "URL must contain 'mcp'",
		},
		{
			name:         "invalid URL format should fail",
			packageName:  "not://a valid url for mcpb!",
			serverName:   "com.example/test",
			fileSHA256:   "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
			expectError:  true,
			errorMessage: "invalid MCPB package URL",
		},
		{
			name:         "non-existent file should fail accessibility check",
			packageName:  "https://github.com/nonexistent/repo/releases/download/v1.0.0/mcp-server.tar.gz",
			serverName:   "com.example/test",
			fileSHA256:   "fe333e598595000ae021bd27117db32ec69af6987f507ba7a63c90638ff633ce",
			expectError:  true,
			errorMessage: "not publicly accessible",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeMCPB,
				Identifier:   tt.packageName,
				FileSHA256:   tt.fileSHA256,
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
