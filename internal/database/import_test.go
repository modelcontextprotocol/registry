package database_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
)

func TestReadSeedFile_LocalFile(t *testing.T) {
	// Create a temporary seed file
	tempFile := "/tmp/test_seed.json"
	seedData := []model.ServerDetail{
		{
			Server: model.Server{
				ID:          "test-id-1",
				Name:        "test-server-1",
				Description: "Test server 1",
				Repository: model.Repository{
					URL:    "https://github.com/test/repo1",
					Source: "github",
					ID:     "123",
				},
				VersionDetail: model.VersionDetail{
					Version:     "1.0.0",
					ReleaseDate: "2023-01-01T00:00:00Z",
					IsLatest:    true,
				},
			},
		},
	}

	// Write seed data to temp file
	data, err := json.Marshal(seedData)
	assert.NoError(t, err)

	err = func() error {
		f, err := os.Create(tempFile)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = f.Write(data)
		return err
	}()
	assert.NoError(t, err)
	defer os.Remove(tempFile)

	// Test reading the file
	result, err := database.ReadSeedFile(context.Background(), tempFile)
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "test-server-1", result[0].Name)
}

func TestReadSeedFile_DirectHTTPURL(t *testing.T) {
	// Create a test HTTP server that serves seed JSON directly
	seedData := []model.ServerDetail{
		{
			Server: model.Server{
				ID:          "test-id-1",
				Name:        "test-server-1",
				Description: "Test server 1",
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(seedData); err != nil {
			http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	// Test reading from HTTP URL ending in .json
	result, err := database.ReadSeedFile(context.Background(), server.URL+"/seed.json")
	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "test-server-1", result[0].Name)
}

func TestReadSeedFile_RegistryURL(t *testing.T) {
	// Create mock registry servers
	server1 := model.Server{
		ID:          "server-1",
		Name:        "Test Server 1",
		Description: "First test server",
	}
	server2 := model.Server{
		ID:          "server-2",
		Name:        "Test Server 2",
		Description: "Second test server",
	}

	serverDetail1 := model.ServerDetail{
		Server: server1,
		Packages: []model.Package{
			{
				Name:    "test-package-1",
				Version: "1.0.0",
			},
		},
	}
	serverDetail2 := model.ServerDetail{
		Server: server2,
		Packages: []model.Package{
			{
				Name:    "test-package-2",
				Version: "2.0.0",
			},
		},
	}

	// Create a test HTTP server that simulates the registry API
	mux := http.NewServeMux()

	// Handle /v0/servers endpoint (paginated)
	mux.HandleFunc("/v0/servers", func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")

		var response database.PaginatedResponse
		switch cursor {
		case "":
			// First page
			response = database.PaginatedResponse{
				Data: []model.Server{server1},
				Metadata: database.Metadata{
					NextCursor: "next-cursor-1",
					Count:      1,
				},
			}
		case "next-cursor-1":
			// Second page
			response = database.PaginatedResponse{
				Data: []model.Server{server2},
				Metadata: database.Metadata{
					Count: 1,
					// No NextCursor means end of pagination
				},
			}
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode JSON", http.StatusInternalServerError)
		}
	})

	// Handle individual server detail endpoints
	mux.HandleFunc("/v0/servers/server-1", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(serverDetail1); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/v0/servers/server-2", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(serverDetail2); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Test reading from registry root URL (this should trigger pagination)
	result, err := database.ReadSeedFile(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Len(t, result, 2)

	// Verify the servers were imported correctly
	assert.Equal(t, "Test Server 1", result[0].Name)
	assert.Equal(t, "Test Server 2", result[1].Name)

	// Verify packages were included
	assert.Len(t, result[0].Packages, 1)
	assert.Equal(t, "test-package-1", result[0].Packages[0].Name)
	assert.Len(t, result[1].Packages, 1)
	assert.Equal(t, "test-package-2", result[1].Packages[0].Name)
}
