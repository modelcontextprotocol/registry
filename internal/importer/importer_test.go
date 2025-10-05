package importer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/importer"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportService_LocalFile(t *testing.T) {
	// Create a temporary seed file
	tempFile := "/tmp/test_import_seed.json"
	seedData := []*apiv0.ServerJSON{
		{
			Schema:      model.CurrentSchemaURL,
			Name:        "io.github.test/test-server-1",
			Description: "Test server 1",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo1",
				Source: "github",
				ID:     "123",
			},
			Version: "1.0.0",
		},
	}

	jsonData, err := json.Marshal(seedData)
	require.NoError(t, err)

	err = os.WriteFile(tempFile, jsonData, 0600)
	require.NoError(t, err)
	defer os.Remove(tempFile)

	// Create registry service
	testDB := database.NewTestDB(t)
	registryService := service.NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create importer service and test import
	importerService := importer.NewService(registryService)
	err = importerService.ImportFromPath(context.Background(), tempFile)
	require.NoError(t, err)

	// Verify the server was imported using registry service
	servers, _, err := registryService.ListServers(context.Background(), nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, servers, 1)
	assert.Equal(t, "io.github.test/test-server-1", servers[0].Server.Name)
	assert.Equal(t, "1.0.0", servers[0].Server.Version)
	assert.Equal(t, "Test server 1", servers[0].Server.Description)
	assert.NotNil(t, servers[0].Meta.Official)
	assert.Equal(t, model.StatusActive, servers[0].Meta.Official.Status)
}

func TestImportService_HTTPFile(t *testing.T) {
	// Create a test HTTP server
	seedData := []*apiv0.ServerJSON{
		{
			Schema:      model.CurrentSchemaURL,
			Name:        "io.github.test/http-test-server",
			Description: "HTTP test server",
			Repository: model.Repository{
				URL:    "https://github.com/test/http-repo",
				Source: "github",
				ID:     "456",
			},
			Version: "2.0.0",
		},
	}

	jsonData, err := json.Marshal(seedData)
	require.NoError(t, err)

	// Create test HTTP server
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(jsonData)
	}))
	defer httpServer.Close()

	// Create registry service
	testDB := database.NewTestDB(t)
	registryService := service.NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Create importer service and test import
	importerService := importer.NewService(registryService)
	err = importerService.ImportFromPath(context.Background(), httpServer.URL+"/seed.json")
	require.NoError(t, err)

	// Verify the server was imported
	servers, _, err := registryService.ListServers(context.Background(), nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, servers, 1)
	assert.Equal(t, "io.github.test/http-test-server", servers[0].Server.Name)
	assert.Equal(t, "2.0.0", servers[0].Server.Version)
	assert.Equal(t, "HTTP test server", servers[0].Server.Description)
	assert.NotNil(t, servers[0].Meta.Official)
}

func TestImportService_RegistryPagination(t *testing.T) {
	ctx := context.Background()

	// Create registry service with test data
	testDB := database.NewTestDB(t)
	registryService := service.NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})

	// Setup source registry with test data
	sourceServers := []*apiv0.ServerJSON{
		{
			Schema:      model.CurrentSchemaURL,
			Name:        "com.source/server-1",
			Description: "Source server 1",
			Version:     "1.0.0",
		},
		{
			Schema:      model.CurrentSchemaURL,
			Name:        "com.source/server-2",
			Description: "Source server 2",
			Version:     "1.0.0",
		},
	}

	for _, server := range sourceServers {
		_, err := registryService.CreateServer(ctx, server)
		require.NoError(t, err)
	}

	// Create test HTTP server that serves the registry API
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		servers, _, _ := registryService.ListServers(ctx, nil, "", 10)

		// Convert to response format
		serverValues := make([]apiv0.ServerResponse, len(servers))
		for i, server := range servers {
			serverValues[i] = *server
		}

		response := apiv0.ServerListResponse{
			Servers: serverValues,
			Metadata: apiv0.Metadata{
				Count: len(servers),
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer httpServer.Close()

	// Create target registry for import
	targetDB := database.NewTestDB(t)
	targetRegistryService := service.NewRegistryService(targetDB, &config.Config{EnableRegistryValidation: false})

	// Create importer service and test registry import
	importerService := importer.NewService(targetRegistryService)
	err := importerService.ImportFromPath(context.Background(), httpServer.URL+"/v0/servers")
	require.NoError(t, err)

	// Verify servers were imported
	importedServers, _, err := targetRegistryService.ListServers(context.Background(), nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, importedServers, 2)

	// Verify server details
	serverNames := make([]string, len(importedServers))
	for i, server := range importedServers {
		serverNames[i] = server.Server.Name
	}
	assert.Contains(t, serverNames, "com.source/server-1")
	assert.Contains(t, serverNames, "com.source/server-2")
}

func TestImportService_ErrorHandling(t *testing.T) {
	// Create registry service
	testDB := database.NewTestDB(t)
	registryService := service.NewRegistryService(testDB, &config.Config{EnableRegistryValidation: false})
	importerService := importer.NewService(registryService)

	tests := []struct {
		name        string
		path        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "non-existent local file",
			path:        "/tmp/non-existent-file.json",
			expectError: true,
			errorMsg:    "failed to read seed data",
		},
		{
			name:        "invalid JSON file",
			path:        "/tmp/invalid.json",
			expectError: true,
			errorMsg:    "failed to read seed data",
		},
		{
			name:        "non-existent HTTP URL",
			path:        "http://non-existent-domain-12345.com/seed.json",
			expectError: true,
			errorMsg:    "failed to read seed data",
		},
	}

	// Create invalid JSON file for testing
	invalidJSON := []byte("{invalid json}")
	tempFile, err := os.CreateTemp("", "invalid-*.json")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())
	err = os.WriteFile(tempFile.Name(), invalidJSON, 0600)
	require.NoError(t, err)

	// Update test case to use temp file
	for i := range tests {
		if tests[i].path == "/tmp/invalid.json" {
			tests[i].path = tempFile.Name()
			break
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := importerService.ImportFromPath(context.Background(), tt.path)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
