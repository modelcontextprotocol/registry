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
		expectedServers      []apiv0.ServerJSON
		expectedMeta         *v0.Metadata
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
					VersionDetail: model.VersionDetail{
						Version: "1.0.0",
					},
				}
				server2 := apiv0.ServerJSON{
					Name:        "com.example/test-server-2",
					Description: "Second test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-2",
						Source: "github",
						ID:     "example/test-server-2",
					},
					VersionDetail: model.VersionDetail{
						Version: "2.0.0",
					},
				}
				_, _ = registry.Publish(server1)
				_, _ = registry.Publish(server2)
			},
			expectedStatus:  http.StatusOK,
			expectedServers: nil, // Will be verified differently since IDs are dynamic
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
					VersionDetail: model.VersionDetail{
						Version: "1.5.0",
					},
				}
				_, _ = registry.Publish(server)
			},
			expectedStatus:  http.StatusOK,
			expectedServers: nil, // Will be verified differently since IDs are dynamic
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
			expectedStatus:  http.StatusOK,
			expectedServers: []apiv0.ServerJSON{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock registry service
			registryService := service.NewRegistryServiceWithDB(database.NewMemoryDB())
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
				var resp struct {
					Servers  []apiv0.ServerJSON `json:"servers"`
					Metadata *v0.Metadata       `json:"metadata,omitempty"`
				}
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)

				// Check the response data
				if tc.expectedServers != nil {
					assert.Equal(t, tc.expectedServers, resp.Servers)
				} else {
					// For tests with dynamic data, check structure and count
					assert.NotEmpty(t, resp.Servers, "Expected at least one server")
					for _, server := range resp.Servers {
						assert.NotEmpty(t, server.Name)
						assert.NotEmpty(t, server.Description)
						assert.NotNil(t, server.Meta)
						assert.NotNil(t, server.Meta.IOModelContextProtocolRegistry)
						assert.NotEmpty(t, server.Meta.IOModelContextProtocolRegistry.ID)
					}
				}

				// Check metadata if expected
				if tc.expectedMeta != nil {
					assert.NotNil(t, resp.Metadata, "Expected metadata to be present")
					if resp.Metadata != nil {
						assert.Equal(t, tc.expectedMeta.Count, resp.Metadata.Count)
						if tc.expectedMeta.NextCursor != "" {
							assert.NotEmpty(t, resp.Metadata.NextCursor)
						}
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
	registryService := service.NewRegistryServiceWithDB(database.NewMemoryDB())

	testServer, err := registryService.Publish(apiv0.ServerJSON{
		Name:        "com.example/test-server",
		Description: "A test server",
		VersionDetail: model.VersionDetail{
			Version: "1.0.0",
		},
	})
	assert.NoError(t, err)

	testCases := []struct {
		name           string
		serverID       string
		expectedStatus int
		expectedServer *apiv0.ServerJSON
		expectedError  string
	}{
		{
			name:           "successful get server detail",
			serverID:       testServer.Meta.IOModelContextProtocolRegistry.ID,
			expectedStatus: http.StatusOK,
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

// TestServersEndpointsIntegration tests the servers endpoints with actual HTTP requests
func TestServersEndpointsIntegration(t *testing.T) {
	// Create mock registry service
	registryService := service.NewRegistryServiceWithDB(database.NewMemoryDB())

	// Test data - publish a server and get its actual ID
	testServer := apiv0.ServerJSON{
		Name:        "com.example/integration-test-server",
		Description: "Integration test server",
		Repository: model.Repository{
			URL:    "https://github.com/example/integration-test",
			Source: "github",
			ID:     "example/integration-test",
		},
		VersionDetail: model.VersionDetail{
			Version: "1.0.0",
		},
	}

	published, err := registryService.Publish(testServer)
	assert.NoError(t, err)
	assert.NotNil(t, published)

	serverID := published.Meta.IOModelContextProtocolRegistry.ID
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
		var listResp struct {
			Servers []apiv0.ServerJSON `json:"servers"`
		}
		err = json.NewDecoder(resp.Body).Decode(&listResp)
		assert.NoError(t, err)

		// Check the response data (excluding timestamps which will be different)
		assert.Len(t, listResp.Servers, len(servers))
		if len(listResp.Servers) > 0 {
			assert.Equal(t, servers[0].Name, listResp.Servers[0].Name)
			assert.Equal(t, servers[0].Description, listResp.Servers[0].Description)
			assert.Equal(t, servers[0].Repository, listResp.Servers[0].Repository)
			assert.Equal(t, servers[0].VersionDetail, listResp.Servers[0].VersionDetail)
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
		assert.Equal(t, serverDetail.VersionDetail, serverDetailResp.VersionDetail)
	})

	// Verify mock expectations
	// No expectations to verify with real service
}
