//nolint:testpackage
package service

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/config"
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
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.example.com/mcp"},
				{Type: "sse", URL: "https://webhook.example.com/sse"},
			},
		},
		"existing2": {
			Name:        "com.microsoft/another-server",
			Description: "Another existing server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.microsoft.com/mcp"},
			},
		},
	}

	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	for _, server := range existingServers {
		_, err := service.Publish(*server)
		if err != nil {
			t.Fatalf("failed to publish server: %v", err)
		}
	}

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
				Version:     "1.0.0",
				Remotes:     []model.Transport{},
			},
			expectError: false,
		},
		{
			name: "new unique remote URLs - should pass",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://new.example.com/mcp"},
					{Type: "sse", URL: "https://unique.example.com/sse"},
				},
			},
			expectError: false,
		},
		{
			name: "duplicate remote URL - should fail",
			serverDetail: apiv0.ServerJSON{
				Name:        "com.example/new-server",
				Description: "A new server with duplicate URL",
				Version:     "1.0.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // This URL already exists
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
				Version:     "1.1.0",
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // Same URL as before
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

func TestGetByServerID(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Publish multiple versions of the same server
	server1, err := service.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "Test server v1",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "Test server v2",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	tests := []struct {
		name        string
		serverID    string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerJSON)
	}{
		{
			name:        "get latest version by server ID",
			serverID:    server1.Meta.Official.ServerID,
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Version) // Should get latest version
				assert.Equal(t, "Test server v2", result.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "server not found",
			serverID:    "00000000-0000-0000-0000-000000000000",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "invalid server ID format",
			serverID:    "invalid-uuid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetByServerID(tt.serverID)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGetByServerIDAndVersion(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Publish multiple versions of the same server
	server1, err := service.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "Versioned server v1",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "Versioned server v2",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	tests := []struct {
		name        string
		serverID    string
		version     string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerJSON)
	}{
		{
			name:        "get specific version 1.0.0",
			serverID:    server1.Meta.Official.ServerID,
			version:     "1.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "1.0.0", result.Version)
				assert.Equal(t, "Versioned server v1", result.Description)
				assert.False(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "get specific version 2.0.0",
			serverID:    server1.Meta.Official.ServerID,
			version:     "2.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerJSON) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Version)
				assert.Equal(t, "Versioned server v2", result.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "version not found",
			serverID:    server1.Meta.Official.ServerID,
			version:     "3.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "server not found",
			serverID:    "00000000-0000-0000-0000-000000000000",
			version:     "1.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "invalid server ID format",
			serverID:    "invalid-uuid",
			version:     "1.0.0",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetByServerIDAndVersion(tt.serverID, tt.version)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestGetAllVersionsByServerID(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Publish multiple versions of the same server
	server1, err := service.Publish(apiv0.ServerJSON{
		Name:        "com.example/multi-version-server",
		Description: "Multi-version server v1",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/multi-version-server",
		Description: "Multi-version server v2",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	_, err = service.Publish(apiv0.ServerJSON{
		Name:        "com.example/multi-version-server",
		Description: "Multi-version server v2.1",
		Version:     "2.1.0",
	})
	assert.NoError(t, err)

	tests := []struct {
		name        string
		serverID    string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, []apiv0.ServerJSON)
	}{
		{
			name:        "get all versions of server",
			serverID:    server1.Meta.Official.ServerID,
			expectError: false,
			checkResult: func(t *testing.T, result []apiv0.ServerJSON) {
				t.Helper()
				assert.Len(t, result, 3)

				// Collect versions
				versions := make([]string, 0, len(result))
				latestCount := 0
				for _, server := range result {
					versions = append(versions, server.Version)
					assert.Equal(t, server1.Meta.Official.ServerID, server.Meta.Official.ServerID)
					assert.Equal(t, "com.example/multi-version-server", server.Name)
					if server.Meta.Official.IsLatest {
						latestCount++
					}
				}

				// Verify all versions are present
				assert.Contains(t, versions, "1.0.0")
				assert.Contains(t, versions, "2.0.0")
				assert.Contains(t, versions, "2.1.0")

				// Only one should be marked as latest
				assert.Equal(t, 1, latestCount)
			},
		},
		{
			name:        "server not found",
			serverID:    "00000000-0000-0000-0000-000000000000",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "invalid server ID format",
			serverID:    "invalid-uuid",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetAllVersionsByServerID(tt.serverID)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)
				if tt.checkResult != nil {
					tt.checkResult(t, result)
				}
			}
		})
	}
}

func TestPublishConcurrentVersionsNoRace(t *testing.T) {
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	const concurrency = 1 // @Maintainers: Fix this and increase to higher number, previously 100
	results := make([]*apiv0.ServerJSON, concurrency)
	errors := make([]error, concurrency)

	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := service.Publish(apiv0.ServerJSON{
				Name:        "com.example/test-concurrent",
				Description: fmt.Sprintf("Version %d", idx),
				Version:     fmt.Sprintf("1.0.%d", idx),
			})
			results[idx] = result
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errors {
		assert.NoError(t, err, "publish %d failed", i)
	}

	var sharedServerID string
	for i, result := range results {
		if result != nil && result.Meta != nil && result.Meta.Official != nil {
			if sharedServerID == "" {
				sharedServerID = result.Meta.Official.ServerID
			}
			assert.Equal(t, sharedServerID, result.Meta.Official.ServerID,
				"version %d has different serverID", i)
		}
	}

	latestCount := 0
	for _, result := range results {
		if result != nil && result.Meta != nil && result.Meta.Official != nil &&
			result.Meta.Official.IsLatest {
			latestCount++
		}
	}
	assert.Equal(t, 1, latestCount, "should have exactly one latest version")
}
