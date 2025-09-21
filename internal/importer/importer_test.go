package importer_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/importer"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportService_LocalFile(t *testing.T) {
	// Create a temporary seed file
	tempFile := "/tmp/test_import_seed.json"
	seedData := []apiv0.ServerJSON{
		{
			Name:        "io.github.test/test-server-1",
			Description: "Test server 1",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo1",
				Source: "github",
				ID:     "123",
			},
			Version: "1.0.0",
			Meta: &apiv0.ServerMeta{
				Official: &apiv0.RegistryExtensions{
					ServerID:    "server-id-1",
					VersionID:   "test-id-1",
					PublishedAt: time.Now(),
					UpdatedAt:   time.Now(),
					IsLatest:    true,
				},
			},
		},
	}

	jsonData, err := json.Marshal(seedData)
	require.NoError(t, err)

	err = os.WriteFile(tempFile, jsonData, 0600)
	require.NoError(t, err)
	defer os.Remove(tempFile)

	memDB := database.NewMemoryDB()

	// Create importer service and test import
	service := importer.NewService(memDB)
	err = service.ImportFromPath(context.Background(), tempFile)
	require.NoError(t, err)

	// Verify the server was imported
	servers, _, err := memDB.List(context.Background(), nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, servers, 1)
	assert.Equal(t, "io.github.test/test-server-1", servers[0].Name)
}

func TestImportService_HTTPFile(t *testing.T) {
	// Create a test HTTP server
	seedData := []apiv0.ServerJSON{
		{
			Name:        "io.github.test/http-test-server",
			Description: "HTTP test server",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo1",
				Source: "github",
				ID:     "123",
			},
			Version: "2.0.0",
			Meta: &apiv0.ServerMeta{
				Official: &apiv0.RegistryExtensions{
					ServerID:    "server-id-test",
					VersionID:   "test-id-2",
					PublishedAt: time.Now(),
					UpdatedAt:   time.Now(),
					IsLatest:    true,
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(seedData)
	}))
	defer server.Close()

	memDB := database.NewMemoryDB()

	// Create importer service and test import
	service := importer.NewService(memDB)
	err := service.ImportFromPath(context.Background(), server.URL+"/seed.json")
	require.NoError(t, err)

	// Verify the server was imported
	servers, _, err := memDB.List(context.Background(), nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, servers, 1)
	assert.Equal(t, "io.github.test/http-test-server", servers[0].Name)
}

func TestImportService_RegistryAPI(t *testing.T) {
	// Create test data
	testServers := []apiv0.ServerJSON{
		{
			Name:        "io.github.test/api-server-1",
			Description: "API server 1",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo1",
				Source: "github",
				ID:     "123",
			},
			Version: "1.0.0",
			Meta: &apiv0.ServerMeta{
				Official: &apiv0.RegistryExtensions{
					ServerID:    "server-id-test",
					VersionID:   "api-test-id-1",
					PublishedAt: time.Now(),
					UpdatedAt:   time.Now(),
					IsLatest:    true,
				},
			},
		},
		{
			Name:        "io.github.test/api-server-2",
			Description: "API server 2",
			Repository: model.Repository{
				URL:    "https://github.com/test/repo2",
				Source: "github",
				ID:     "456",
			},
			Version: "2.0.0",
			Meta: &apiv0.ServerMeta{
				Official: &apiv0.RegistryExtensions{
					ServerID:    "server-id-test",
					VersionID:   "api-test-id-2",
					PublishedAt: time.Now(),
					UpdatedAt:   time.Now(),
					IsLatest:    true,
				},
			},
		},
	}

	// Create test server that responds with paginated results
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")

		var response struct {
			Servers  []apiv0.ServerJSON `json:"servers"`
			Metadata *struct {
				NextCursor string `json:"next_cursor,omitempty"`
			} `json:"metadata,omitempty"`
		}

		switch cursor {
		case "":
			// First page
			response.Servers = []apiv0.ServerJSON{testServers[0]}
			response.Metadata = &struct {
				NextCursor string `json:"next_cursor,omitempty"`
			}{NextCursor: "page2"}
		case "page2":
			// Second page
			response.Servers = []apiv0.ServerJSON{testServers[1]}
			// No next cursor means end of results
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	memDB := database.NewMemoryDB()

	// Create importer service and test import
	service := importer.NewService(memDB)
	err := service.ImportFromPath(context.Background(), server.URL+"/v0/servers")
	require.NoError(t, err)

	// Verify both servers were imported
	servers, _, err := memDB.List(context.Background(), nil, "", 10)
	require.NoError(t, err)
	assert.Len(t, servers, 2)

	names := []string{servers[0].Name, servers[1].Name}
	assert.Contains(t, names, "io.github.test/api-server-1")
	assert.Contains(t, names, "io.github.test/api-server-2")
}
