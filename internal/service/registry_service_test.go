//nolint:testpackage
package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateNoDuplicateRemoteURLs(t *testing.T) {
	ctx := context.Background()

	// Create test data
	existingServers := map[string]*apiv0.ServerJSON{
		"existing1": {
			Schema:      model.CurrentSchemaURL,
			Name:        "com.example/existing-server",
			Description: "An existing server",
			Version:     "1.0.0",
			Remotes: []model.Transport{
				{Type: "streamable-http", URL: "https://api.example.com/mcp"},
				{Type: "sse", URL: "https://webhook.example.com/sse"},
			},
		},
		"existing2": {
			Schema:      model.CurrentSchemaURL,
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

	// Create existing servers using the new CreateServer method
	for _, server := range existingServers {
		_, err := service.CreateServer(ctx, server)
		require.NoError(t, err, "failed to create server: %v", err)
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
				Schema:      model.CurrentSchemaURL,
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
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/new-server-unique",
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
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/new-server-duplicate",
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
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/existing-server", // Same name as existing
				Description: "Updated existing server",
				Version:     "1.1.0", // Different version
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://api.example.com/mcp"}, // Same URL as before
				},
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			impl := service.(*registryServiceImpl)

			err := impl.validateNoDuplicateRemoteURLs(ctx, nil, tt.serverDetail)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetServerByName(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create multiple versions of the same server
	_, err := service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "Test server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/test-server",
		Description: "Test server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:        "get latest version by server name",
			serverName:  "com.example/test-server",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Server.Version) // Should get latest version
				assert.Equal(t, "Test server v2", result.Server.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "server not found",
			serverName:  "com.example/non-existent",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetServerByName(ctx, tt.serverName)

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

func TestGetServerByNameAndVersion(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/versioned-server"

	// Create multiple versions of the same server
	_, err := service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Versioned server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Versioned server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		version     string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:        "get specific version 1.0.0",
			serverName:  serverName,
			version:     "1.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "1.0.0", result.Server.Version)
				assert.Equal(t, "Versioned server v1", result.Server.Description)
				assert.False(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "get specific version 2.0.0",
			serverName:  serverName,
			version:     "2.0.0",
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "2.0.0", result.Server.Version)
				assert.Equal(t, "Versioned server v2", result.Server.Description)
				assert.True(t, result.Meta.Official.IsLatest)
			},
		},
		{
			name:        "version not found",
			serverName:  serverName,
			version:     "3.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
		{
			name:        "server not found",
			serverName:  "com.example/non-existent",
			version:     "1.0.0",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetServerByNameAndVersion(ctx, tt.serverName, tt.version)

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

func TestGetAllVersionsByServerName(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/multi-version-server"

	// Create multiple versions of the same server
	_, err := service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Multi-version server v1",
		Version:     "1.0.0",
	})
	require.NoError(t, err)

	_, err = service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Multi-version server v2",
		Version:     "2.0.0",
	})
	require.NoError(t, err)

	_, err = service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Multi-version server v2.1",
		Version:     "2.1.0",
	})
	require.NoError(t, err)

	tests := []struct {
		name        string
		serverName  string
		expectError bool
		errorMsg    string
		checkResult func(*testing.T, []*apiv0.ServerResponse)
	}{
		{
			name:        "get all versions of server",
			serverName:  serverName,
			expectError: false,
			checkResult: func(t *testing.T, result []*apiv0.ServerResponse) {
				t.Helper()
				assert.Len(t, result, 3)

				// Collect versions
				versions := make([]string, 0, len(result))
				latestCount := 0
				for _, server := range result {
					versions = append(versions, server.Server.Version)
					assert.Equal(t, serverName, server.Server.Name)
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
			serverName:  "com.example/non-existent",
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.GetAllVersionsByServerName(ctx, tt.serverName)

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

func TestCreateServerConcurrentVersionsNoRace(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	const concurrency = 100
	serverName := "com.example/test-concurrent"
	results := make([]*apiv0.ServerResponse, concurrency)
	errors := make([]error, concurrency)

	var wg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result, err := service.CreateServer(ctx, &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        serverName,
				Description: fmt.Sprintf("Version %d", idx),
				Version:     fmt.Sprintf("1.0.%d", idx),
			})
			results[idx] = result
			errors[idx] = err
		}(i)
	}
	wg.Wait()

	// All publishes should succeed
	for i, err := range errors {
		assert.NoError(t, err, "create server %d failed", i)
	}

	// All results should have the same server name
	for i, result := range results {
		if result != nil {
			assert.Equal(t, serverName, result.Server.Name, "version %d has different server name", i)
		}
	}

	// Query database to check the final state after all creates complete
	allVersions, err := service.GetAllVersionsByServerName(ctx, serverName)
	require.NoError(t, err, "failed to get all versions")

	latestCount := 0
	var latestVersion string
	for _, r := range allVersions {
		if r.Meta.Official.IsLatest {
			latestCount++
			latestVersion = r.Server.Version
		}
	}

	assert.Equal(t, 1, latestCount, "should have exactly one latest version in database, found version: %s", latestVersion)
	assert.Len(t, allVersions, concurrency, "should have all %d versions", concurrency)
}

func TestUpdateServer(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/update-test-server"
	version := "1.0.0"

	// Create initial server
	_, err := service.CreateServer(ctx, &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Original description",
		Version:     version,
		Remotes: []model.Transport{
			{Type: "streamable-http", URL: "https://original.example.com/mcp"},
		},
	})
	require.NoError(t, err)

	tests := []struct {
		name          string
		serverName    string
		version       string
		updatedServer *apiv0.ServerJSON
		newStatus     *string
		expectError   bool
		errorMsg      string
		checkResult   func(*testing.T, *apiv0.ServerResponse)
	}{
		{
			name:       "successful server update",
			serverName: serverName,
			version:    version,
			updatedServer: &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        serverName,
				Description: "Updated description",
				Version:     version,
				Remotes: []model.Transport{
					{Type: "streamable-http", URL: "https://updated.example.com/mcp"},
				},
			},
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Updated description", result.Server.Description)
				assert.Len(t, result.Server.Remotes, 1)
				assert.Equal(t, "https://updated.example.com/mcp", result.Server.Remotes[0].URL)
				assert.NotZero(t, result.Meta.Official.UpdatedAt)
			},
		},
		{
			name:       "update with status change",
			serverName: serverName,
			version:    version,
			updatedServer: &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        serverName,
				Description: "Updated with status change",
				Version:     version,
			},
			newStatus:   stringPtr(string(model.StatusDeprecated)),
			expectError: false,
			checkResult: func(t *testing.T, result *apiv0.ServerResponse) {
				t.Helper()
				assert.Equal(t, "Updated with status change", result.Server.Description)
				assert.Equal(t, model.StatusDeprecated, result.Meta.Official.Status)
			},
		},
		{
			name:       "update non-existent server",
			serverName: "com.example/non-existent",
			version:    "1.0.0",
			updatedServer: &apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/non-existent",
				Description: "Should fail",
				Version:     "1.0.0",
			},
			expectError: true,
			errorMsg:    "record not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := service.UpdateServer(ctx, tt.serverName, tt.version, tt.updatedServer, tt.newStatus)

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

func TestUpdateServer_SkipValidationForDeletedServers(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	// Enable registry validation to test that it gets skipped for deleted servers
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: true})

	serverName := "com.example/validation-skip-test"
	version := "1.0.0"

	// Create server with invalid package configuration (this would fail registry validation)
	invalidServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Server with invalid package for testing validation skip",
		Version:     version,
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "non-existent-package-for-validation-test",
				Version:      "1.0.0",
				Transport:    model.Transport{Type: "stdio"},
			},
		},
	}

	// Create initial server (validation disabled for creation in this test)
	originalConfig := service.(*registryServiceImpl).cfg.EnableRegistryValidation
	service.(*registryServiceImpl).cfg.EnableRegistryValidation = false
	_, err := service.CreateServer(ctx, invalidServer)
	require.NoError(t, err, "failed to create server with validation disabled")
	service.(*registryServiceImpl).cfg.EnableRegistryValidation = originalConfig

	// First, set server to deleted status
	deletedStatus := string(model.StatusDeleted)
	_, err = service.UpdateServer(ctx, serverName, version, invalidServer, &deletedStatus)
	require.NoError(t, err, "should be able to set server to deleted (validation should be skipped)")

	// Verify server is now deleted
	updatedServer, err := service.GetServerByNameAndVersion(ctx, serverName, version)
	require.NoError(t, err)
	assert.Equal(t, model.StatusDeleted, updatedServer.Meta.Official.Status)

	// Now try to update a deleted server - validation should be skipped
	updatedInvalidServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        serverName,
		Description: "Updated description for deleted server",
		Version:     version,
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "another-non-existent-package-for-validation-test",
				Version:      "2.0.0",
				Transport:    model.Transport{Type: "stdio"},
			},
		},
	}

	// This should succeed despite invalid packages because server is deleted
	result, err := service.UpdateServer(ctx, serverName, version, updatedInvalidServer, nil)
	assert.NoError(t, err, "updating deleted server should skip registry validation")
	assert.NotNil(t, result)
	assert.Equal(t, "Updated description for deleted server", result.Server.Description)
	assert.Equal(t, model.StatusDeleted, result.Meta.Official.Status)

	// Test updating a server being set to deleted status
	activeServer := &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "com.example/being-deleted-test",
		Description: "Server being deleted",
		Version:     "1.0.0",
		Packages: []model.Package{
			{
				RegistryType: "npm",
				Identifier:   "yet-another-non-existent-package",
				Version:      "1.0.0",
				Transport:    model.Transport{Type: "stdio"},
			},
		},
	}

	// Create active server (with validation disabled)
	service.(*registryServiceImpl).cfg.EnableRegistryValidation = false
	_, err = service.CreateServer(ctx, activeServer)
	require.NoError(t, err)
	service.(*registryServiceImpl).cfg.EnableRegistryValidation = originalConfig

	// Update server and set to deleted in same operation - should skip validation
	newDeletedStatus := string(model.StatusDeleted)
	result2, err := service.UpdateServer(ctx, "com.example/being-deleted-test", "1.0.0", activeServer, &newDeletedStatus)
	assert.NoError(t, err, "updating server being set to deleted should skip registry validation")
	assert.NotNil(t, result2)
	assert.Equal(t, model.StatusDeleted, result2.Meta.Official.Status)
}

func TestListServers(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create test servers
	testServers := []struct {
		name        string
		version     string
		description string
	}{
		{"com.example/server-alpha", "1.0.0", "Alpha server"},
		{"com.example/server-beta", "1.0.0", "Beta server"},
		{"com.example/server-gamma", "2.0.0", "Gamma server"},
	}

	for _, server := range testServers {
		_, err := service.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        server.name,
			Description: server.description,
			Version:     server.version,
		})
		require.NoError(t, err)
	}

	tests := []struct {
		name          string
		filter        *database.ServerFilter
		cursor        string
		limit         int
		expectedCount int
		expectError   bool
	}{
		{
			name:          "list all servers",
			filter:        nil,
			limit:         10,
			expectedCount: 3,
		},
		{
			name: "filter by name",
			filter: &database.ServerFilter{
				Name: stringPtr("com.example/server-alpha"),
			},
			limit:         10,
			expectedCount: 1,
		},
		{
			name: "filter by version",
			filter: &database.ServerFilter{
				Version: stringPtr("1.0.0"),
			},
			limit:         10,
			expectedCount: 2,
		},
		{
			name:          "pagination with limit",
			filter:        nil,
			limit:         2,
			expectedCount: 2,
		},
		{
			name:   "cursor pagination",
			filter: nil,
			cursor: "com.example/server-alpha",
			limit:  10,
			// Should return servers after 'server-alpha' alphabetically
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, nextCursor, err := service.ListServers(ctx, tt.filter, tt.cursor, tt.limit)

			if tt.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			assert.Len(t, results, tt.expectedCount)

			// Test cursor behavior
			if tt.limit < len(testServers) && len(results) == tt.limit {
				assert.NotEmpty(t, nextCursor, "Should return next cursor when results are limited")
			}
		})
	}
}

func TestVersionComparison(t *testing.T) {
	ctx := context.Background()
	testDB := database.NewTestDB(t)
	service := NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	serverName := "com.example/version-comparison-server"

	// Create versions in non-chronological order to test version comparison logic
	versions := []struct {
		version     string
		description string
		delay       time.Duration // Delay to simulate different publish times
	}{
		{"2.0.0", "Version 2.0.0", 0},
		{"1.0.0", "Version 1.0.0", 10 * time.Millisecond},
		{"2.1.0", "Version 2.1.0", 20 * time.Millisecond},
		{"1.5.0", "Version 1.5.0", 30 * time.Millisecond},
	}

	for _, v := range versions {
		if v.delay > 0 {
			time.Sleep(v.delay)
		}
		_, err := service.CreateServer(ctx, &apiv0.ServerJSON{
			Schema:      model.CurrentSchemaURL,
			Name:        serverName,
			Description: v.description,
			Version:     v.version,
		})
		require.NoError(t, err, "Failed to create version %s", v.version)
	}

	// Get the latest version - should be 2.1.0 based on semantic versioning
	latest, err := service.GetServerByName(ctx, serverName)
	require.NoError(t, err)

	assert.Equal(t, "2.1.0", latest.Server.Version, "Latest version should be 2.1.0")
	assert.True(t, latest.Meta.Official.IsLatest)

	// Verify only one version is marked as latest
	allVersions, err := service.GetAllVersionsByServerName(ctx, serverName)
	require.NoError(t, err)

	latestCount := 0
	for _, version := range allVersions {
		if version.Meta.Official.IsLatest {
			latestCount++
		}
	}
	assert.Equal(t, 1, latestCount, "Exactly one version should be marked as latest")
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}
