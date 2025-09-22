package v0_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestServersListEndpoint(t *testing.T) {
	testCases := []struct {
		name                 string
		queryParams          string
		setupRegistryService func(service.RegistryService)
		expectedStatus       int
		expectedMeta         *apiv0.Metadata
		expectedError        string
	}{
		{
			name: "successful list with default parameters",
			setupRegistryService: func(registry service.RegistryService) {
				// Publish test servers
				server1 := apiv0.ServerJSON{
					Name:        "com.example/test-server-1",
					Description: "First test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-1",
						Source: "github",
						ID:     "example/test-server-1",
					},
					Version: "1.0.0",
				}
				server2 := apiv0.ServerJSON{
					Name:        "com.example/test-server-2",
					Description: "Second test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-2",
						Source: "github",
						ID:     "example/test-server-2",
					},
					Version: "2.0.0",
				}
				_, _ = registry.Publish(server1)
				_, _ = registry.Publish(server2)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful list with cursor and limit",
			queryParams: "?limit=10",
			setupRegistryService: func(registry service.RegistryService) {
				server := apiv0.ServerJSON{
					Name:        "com.example/test-server-3",
					Description: "Third test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-3",
						Source: "github",
						ID:     "example/test-server-3",
					},
					Version: "1.5.0",
				}
				_, _ = registry.Publish(server)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:                 "successful list with limit capping at 100",
			queryParams:          "?limit=150",
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusUnprocessableEntity, // Huma rejects values > maximum
			expectedError:        "validation failed",
		},
		{
			name:                 "invalid cursor parameter",
			queryParams:          "?cursor=invalid-uuid",
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:        "validation failed",
		},
		{
			name:                 "invalid limit parameter - non-numeric",
			queryParams:          "?limit=abc",
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:        "validation failed",
		},
		{
			name:                 "invalid limit parameter - zero",
			queryParams:          "?limit=0",
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:        "validation failed",
		},
		{
			name:                 "invalid limit parameter - negative",
			queryParams:          "?limit=-5",
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:        "validation failed",
		},
		{
			name: "empty registry returns success",
			setupRegistryService: func(_ service.RegistryService) {
				// Test empty registry - empty setup
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful search by name substring",
			queryParams: "?search=test-server",
			setupRegistryService: func(registry service.RegistryService) {
				server1 := apiv0.ServerJSON{
					Name:        "com.example/test-server-matching",
					Description: "Matching test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-matching",
						Source: "github",
						ID:     "example/test-matching",
					},
					Version: "1.0.0",
				}
				server2 := apiv0.ServerJSON{
					Name:        "com.example/other-server",
					Description: "Non-matching server",
					Repository: model.Repository{
						URL:    "https://github.com/example/other",
						Source: "github",
						ID:     "example/other",
					},
					Version: "1.0.0",
				}
				_, _ = registry.Publish(server1)
				_, _ = registry.Publish(server2)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful updated_since filter with RFC3339",
			queryParams: "?updated_since=2020-01-01T00:00:00Z",
			setupRegistryService: func(registry service.RegistryService) {
				server := apiv0.ServerJSON{
					Name:        "com.example/recent-server",
					Description: "Recently updated server",
					Repository: model.Repository{
						URL:    "https://github.com/example/recent",
						Source: "github",
						ID:     "example/recent",
					},
					Version: "1.0.0",
				}
				_, _ = registry.Publish(server)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "successful version=latest filter",
			queryParams: "?version=latest",
			setupRegistryService: func(registry service.RegistryService) {
				server1 := apiv0.ServerJSON{
					Name:        "com.example/versioned-server",
					Description: "First version",
					Repository: model.Repository{
						URL:    "https://github.com/example/versioned",
						Source: "github",
						ID:     "example/versioned",
					},
					Version: "1.0.0",
				}
				server2 := apiv0.ServerJSON{
					Name:        "com.example/versioned-server",
					Description: "Second version (latest)",
					Repository: model.Repository{
						URL:    "https://github.com/example/versioned",
						Source: "github",
						ID:     "example/versioned",
					},
					Version: "2.0.0",
				}
				_, _ = registry.Publish(server1)
				_, _ = registry.Publish(server2) // This will be marked as latest
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:        "combined search and updated_since filter",
			queryParams: "?search=combined&updated_since=2020-01-01T00:00:00Z",
			setupRegistryService: func(registry service.RegistryService) {
				server1 := apiv0.ServerJSON{
					Name:        "com.example/combined-test",
					Description: "Server with combined filtering",
					Repository: model.Repository{
						URL:    "https://github.com/example/combined",
						Source: "github",
						ID:     "example/combined",
					},
					Version: "1.0.0",
				}
				server2 := apiv0.ServerJSON{
					Name:        "com.example/other-server",
					Description: "Server that doesn't match search",
					Repository: model.Repository{
						URL:    "https://github.com/example/nomatch",
						Source: "github",
						ID:     "example/nomatch",
					},
					Version: "1.0.0",
				}
				_, _ = registry.Publish(server1)
				_, _ = registry.Publish(server2)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:                 "invalid updated_since format",
			queryParams:          "?updated_since=invalid-timestamp",
			setupRegistryService: func(_ service.RegistryService) {},
			expectedStatus:       http.StatusBadRequest,
			expectedError:        "Invalid updated_since format: expected RFC3339",
		},
		{
			name:        "comprehensive query with all parameters",
			queryParams: "?search=filesystem&updated_since=2020-01-01T00:00:00Z&version=latest&limit=50&cursor=",
			setupRegistryService: func(registry service.RegistryService) {
				// Create multiple versions of servers with different names
				server1v1 := apiv0.ServerJSON{
					Name:        "io.example/filesystem-server",
					Description: "Filesystem operations server v1",
					Repository: model.Repository{
						URL:    "https://github.com/example/filesystem",
						Source: "github",
						ID:     "example/filesystem",
					},
					Version: "1.0.0",
				}
				server1v2 := apiv0.ServerJSON{
					Name:        "io.example/filesystem-server",
					Description: "Filesystem operations server v2 (latest)",
					Repository: model.Repository{
						URL:    "https://github.com/example/filesystem",
						Source: "github",
						ID:     "example/filesystem",
					},
					Version: "2.0.0",
				}
				server2 := apiv0.ServerJSON{
					Name:        "com.example/database-server",
					Description: "Database operations server",
					Repository: model.Repository{
						URL:    "https://github.com/example/database",
						Source: "github",
						ID:     "example/database",
					},
					Version: "1.0.0",
				}
				server3 := apiv0.ServerJSON{
					Name:        "org.another/filesystem-tools",
					Description: "Filesystem tools and utilities",
					Repository: model.Repository{
						URL:    "https://github.com/another/filesystem-tools",
						Source: "github",
						ID:     "another/filesystem-tools",
					},
					Version: "3.0.0",
				}
				_, _ = registry.Publish(server1v1)
				_, _ = registry.Publish(server1v2)
				_, _ = registry.Publish(server2)
				_, _ = registry.Publish(server3)
			},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock registry service
			registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())
			tc.setupRegistryService(registryService)

			// Create a new test API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the servers endpoints
			v0.RegisterServersEndpoints(api, registryService)

			// Create request
			url := "/v0/servers" + tc.queryParams
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			// Serve the request
			mux.ServeHTTP(w, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, w.Code)

			if tc.expectedStatus == http.StatusOK {
				// Check content type
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

				// Parse response body
				var resp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)

				// Check the response data
				switch tc.name {
				case "successful search by name substring":
					assert.Len(t, resp.Servers, 1, "Expected exactly one matching server")
					assert.Contains(t, resp.Servers[0].Name, "test-server", "Server name should contain search term")
				case "successful updated_since filter with RFC3339":
					assert.Len(t, resp.Servers, 1, "Expected one server updated after 2020")
					assert.Contains(t, resp.Servers[0].Name, "recent-server")
				case "successful version=latest filter":
					assert.Len(t, resp.Servers, 1, "Expected one latest server")
					assert.Contains(t, resp.Servers[0].Description, "latest")
				case "combined search and updated_since filter":
					assert.Len(t, resp.Servers, 1, "Expected one server matching both filters")
					assert.Contains(t, resp.Servers[0].Name, "combined", "Server name should contain search term")
				case "empty registry returns success":
					assert.Empty(t, resp.Servers, "Expected empty server list for empty registry")
				case "comprehensive query with all parameters":
					// Should return only latest versions of servers matching "filesystem" search term
					// Expected: 2 servers (filesystem-server v2.0.0 and filesystem-tools v3.0.0)
					assert.Len(t, resp.Servers, 2, "Expected two servers matching all filters")
					for _, server := range resp.Servers {
						assert.Contains(t, server.Name, "filesystem", "Server name should contain 'filesystem'")
					}
					// Verify the limit parameter worked (should be at most 50, but we only have 2)
					assert.LessOrEqual(t, len(resp.Servers), 50, "Should respect limit parameter")
					// Verify response includes metadata
					assert.NotNil(t, resp.Metadata, "Expected metadata to be present")
					assert.Equal(t, 2, resp.Metadata.Count, "Metadata count should match returned servers")
				default:
					assert.NotEmpty(t, resp.Servers, "Expected at least one server")
				}

				// General structure validation
				for _, server := range resp.Servers {
					assert.NotEmpty(t, server.Name)
					assert.NotEmpty(t, server.Description)
					assert.NotNil(t, server.Meta)
					assert.NotNil(t, server.Meta.Official)
					assert.NotEmpty(t, server.Meta.Official.VersionID)
				}

				// Check metadata if expected
				if tc.expectedMeta != nil {
					assert.NotNil(t, resp.Metadata, "Expected metadata to be present")
					assert.Equal(t, tc.expectedMeta.Count, resp.Metadata.Count)
					if tc.expectedMeta.NextCursor != "" {
						assert.NotEmpty(t, resp.Metadata.NextCursor)
					}
				}
			} else if tc.expectedError != "" {
				// Check error message for non-200 responses
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}

			// Verify mock expectations
			// No expectations to verify with real service
		})
	}
}

func TestServersDetailEndpoint(t *testing.T) {
	// Create mock registry service
	registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())

	// Publish multiple versions of the same server
	testServer1, err := registryService.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "A test server",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = registryService.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "A test server updated",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	testCases := []struct {
		name           string
		serverID       string
		version        string
		expectedStatus int
		expectedServer *apiv0.ServerJSON
		expectedError  string
	}{
		{
			name:           "successful get server detail (latest)",
			serverID:       testServer1.Meta.Official.ServerID,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "successful get server detail with specific version",
			serverID:       testServer1.Meta.Official.ServerID,
			version:        "1.0.0",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "successful get server detail with latest version",
			serverID:       testServer1.Meta.Official.ServerID,
			version:        "2.0.0",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "version not found for server",
			serverID:       testServer1.Meta.Official.ServerID,
			version:        "3.0.0",
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
		{
			name:           "invalid server ID format",
			serverID:       "invalid-uuid",
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "validation failed",
		},
		{
			name:           "server not found",
			serverID:       uuid.New().String(),
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new test API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the servers endpoints
			v0.RegisterServersEndpoints(api, registryService)

			// Create request
			url := "/v0/servers/" + tc.serverID
			if tc.version != "" {
				url += "?version=" + tc.version
			}
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			// Serve the request
			mux.ServeHTTP(w, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, w.Code)

			if tc.expectedStatus == http.StatusOK {
				// Check content type
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

				// Parse response body
				var serverDetailResp apiv0.ServerJSON
				err := json.NewDecoder(w.Body).Decode(&serverDetailResp)
				assert.NoError(t, err)

				// Check that we got a valid response
				assert.NotEmpty(t, serverDetailResp.Name)
			} else if tc.expectedError != "" {
				// Check error message for non-200 responses
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}

			// Verify mock expectations
			// No expectations to verify with real service
		})
	}
}

func TestServersVersionsEndpoint(t *testing.T) {
	// Create mock registry service
	registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())

	// Publish multiple versions of the same server
	testServer1, err := registryService.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "A versioned test server",
		Version:     "1.0.0",
	})
	assert.NoError(t, err)

	_, err = registryService.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "A versioned test server updated",
		Version:     "2.0.0",
	})
	assert.NoError(t, err)

	_, err = registryService.Publish(apiv0.ServerJSON{
		Name:        "com.example/versioned-server",
		Description: "A versioned test server latest",
		Version:     "2.1.0",
	})
	assert.NoError(t, err)

	testCases := []struct {
		name           string
		serverID       string
		expectedStatus int
		expectedCount  int
		expectedError  string
	}{
		{
			name:           "successful get all versions",
			serverID:       testServer1.Meta.Official.ServerID,
			expectedStatus: http.StatusOK,
			expectedCount:  3,
		},
		{
			name:           "invalid server ID format",
			serverID:       "invalid-uuid",
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "validation failed",
		},
		{
			name:           "server not found",
			serverID:       uuid.New().String(),
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new test API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the servers endpoints
			v0.RegisterServersEndpoints(api, registryService)

			// Create request
			url := "/v0/servers/" + tc.serverID + "/versions"
			req := httptest.NewRequest(http.MethodGet, url, nil)
			w := httptest.NewRecorder()

			// Serve the request
			mux.ServeHTTP(w, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, w.Code)

			if tc.expectedStatus == http.StatusOK {
				// Check content type
				assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

				// Parse response body
				var versionsResp apiv0.ServerListResponse
				err := json.NewDecoder(w.Body).Decode(&versionsResp)
				assert.NoError(t, err)

				// Check the response data
				assert.Len(t, versionsResp.Servers, tc.expectedCount)
				assert.Equal(t, tc.expectedCount, versionsResp.Metadata.Count)

				// Verify all returned servers have the same server ID but different versions
				for _, server := range versionsResp.Servers {
					assert.Equal(t, tc.serverID, server.Meta.Official.ServerID)
					assert.NotEmpty(t, server.Version)
					assert.Equal(t, "com.example/versioned-server", server.Name)
				}

				// Verify versions are included (should have 1.0.0, 2.0.0, 2.1.0)
				versions := make([]string, 0, len(versionsResp.Servers))
				for _, server := range versionsResp.Servers {
					versions = append(versions, server.Version)
				}
				assert.Contains(t, versions, "1.0.0")
				assert.Contains(t, versions, "2.0.0")
				assert.Contains(t, versions, "2.1.0")
			} else if tc.expectedError != "" {
				// Check error message for non-200 responses
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}
		})
	}
}

// TestServersEndpointsIntegration tests the servers endpoints with actual HTTP requests
func TestServersEndpointsIntegration(t *testing.T) {
	// Create mock registry service
	registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())

	// Test data - publish a server and get its actual ID
	testServer := apiv0.ServerJSON{
		Name:        "com.example/integration-test-server",
		Description: "Integration test server",
		Repository: model.Repository{
			URL:    "https://github.com/example/integration-test",
			Source: "github",
			ID:     "example/integration-test",
		},
		Version: "1.0.0",
	}

	published, err := registryService.Publish(testServer)
	assert.NoError(t, err)
	assert.NotNil(t, published)

	serverID := published.Meta.Official.ServerID
	servers := []apiv0.ServerJSON{*published}
	serverDetail := published

	// Create a new test API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the servers endpoints
	v0.RegisterServersEndpoints(api, registryService)

	// Create test server
	server := httptest.NewServer(mux)
	defer server.Close()

	// Test list endpoint
	t.Run("list servers integration", func(t *testing.T) {
		ctx := context.Background()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v0/servers", nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check status code
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Check content type
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		// Parse response body
		var listResp apiv0.ServerListResponse
		err = json.NewDecoder(resp.Body).Decode(&listResp)
		assert.NoError(t, err)

		// Check the response data (excluding timestamps which will be different)
		assert.Len(t, listResp.Servers, len(servers))
		if len(listResp.Servers) > 0 {
			assert.Equal(t, servers[0].Name, listResp.Servers[0].Name)
			assert.Equal(t, servers[0].Description, listResp.Servers[0].Description)
			assert.Equal(t, servers[0].Repository, listResp.Servers[0].Repository)
			assert.Equal(t, servers[0].Version, listResp.Servers[0].Version)
		}
	})

	// Test get server detail endpoint
	t.Run("get server detail integration", func(t *testing.T) {
		ctx := context.Background()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/v0/servers/"+serverID, nil)
		if err != nil {
			t.Fatalf("Failed to create request: %v", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send request: %v", err)
		}
		defer resp.Body.Close()

		// Check status code
		assert.Equal(t, http.StatusOK, resp.StatusCode)

		// Check content type
		assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

		// Parse response body
		var serverDetailResp apiv0.ServerJSON
		err = json.NewDecoder(resp.Body).Decode(&serverDetailResp)
		assert.NoError(t, err)

		// Check the response data (excluding timestamps which will be different)
		assert.Equal(t, serverDetail.Name, serverDetailResp.Name)
		assert.Equal(t, serverDetail.Description, serverDetailResp.Description)
		assert.Equal(t, serverDetail.Repository, serverDetailResp.Repository)
		assert.Equal(t, serverDetail.Version, serverDetailResp.Version)
	})

	// Verify mock expectations
	// No expectations to verify with real service
}
