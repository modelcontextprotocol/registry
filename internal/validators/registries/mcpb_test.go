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
		name        string
		packageName string
		serverName  string
	}{
		{
			name:        "any MCPB package should pass validation",
			packageName: "https://github.com/example/mcp-server/releases/download/v1.0.0/server.tar.gz",
			serverName:  "com.example/test",
		},
		{
			name:        "another MCPB package should pass validation",
			packageName: "https://gitlab.com/user/project/archive/main.zip",
			serverName:  "com.different/server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg := model.Package{
				RegistryType: model.RegistryTypeMCPB,
				Identifier:   tt.packageName,
			}

			err := registries.ValidateMCPB(ctx, pkg, tt.serverName)
			assert.NoError(t, err, "MCPB packages should always pass validation")
		})
	}
}