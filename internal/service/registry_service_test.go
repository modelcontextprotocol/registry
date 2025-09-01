//nolint:testpackage
package service

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestValidateNoDuplicateRemoteURLs(t *testing.T) {
	// Create test data
	existingServers := map[string]*apiv0.ServerJSON{
		"existing1": {
			Name:        "com.example/existing-server",
			Description: "An existing server",
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
			Remotes: []model.Remote{
				{URL: "https://api.example.com/mcp"},
				{URL: "https://webhook.example.com/sse"},
			},
		},
		"existing2": {
			Name:        "com.microsoft/another-server",
			Description: "Another existing server",
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
			Remotes: []model.Remote{
				{URL: "https://api.microsoft.com/mcp"},
			},
		},
	}

	memDB := database.NewMemoryDB(existingServers)
	service := NewRegistryServiceWithDB(memDB)

	tests := []struct {
		name         string
		serverDetail apiv0.ServerJSON
		expectError  bool
		errorMsg     string
	}{
		{
			name: "no remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server with no remotes",
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Remotes: []model.Remote{},
			},
			expectError: false,
		},
		{
			name: "new unique remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server",
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Remotes: []model.Remote{
					{URL: "https://new.example.com/mcp"},
					{URL: "https://unique.example.com/sse"},
				},
			},
			expectError: false,
		},
		{
			name: "duplicate remote URL - should fail",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server with duplicate URL",
				VersionDetail: model.VersionDetail{
					Version: "1.0.0",
				},
				Remotes: []model.Remote{
					{URL: "https://api.example.com/mcp"}, // This URL already exists
				},
			},
			expectError: true,
			errorMsg:    "remote URL https://api.example.com/mcp is already used by server com.example/existing-server",
		},
		{
			name: "updating same server with same URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/existing-server", // Same name as existing
				Description: "Updated existing server",
				VersionDetail: model.VersionDetail{
					Version: "1.1.0",
				},
				Remotes: []model.Remote{
					{URL: "https://api.example.com/mcp"}, // Same URL as before
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			impl := service.(*registryServiceImpl)

			err := impl.validateNoDuplicateRemoteURLs(ctx, tt.serverDetail)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
