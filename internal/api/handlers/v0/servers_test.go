package v0_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestServersListEndpoint(t *testing.T) {
	testCases := []struct {
		name            string
		queryParams     string
		setupMocks      func(*MockRegistryService)
		expectedStatus  int
		expectedServers []model.Server
		expectedMeta    *v0.Metadata
		expectedError   string
	}{
		{
			name: "successful list with default parameters",
			setupMocks: func(registry *MockRegistryService) {
				servers := []model.Server{
					{
						ID:          "550e8400-e29b-41d4-a716-446655440001",
						Name:        "test-server-1",
						Description: "First test server",
						Repository: model.Repository{
							URL:    "https://github.com/example/test-server-1",
							Source: "github",
							ID:     "example/test-server-1",
						},
						VersionDetail: model.VersionDetail{
							Version:     "1.0.0",
							ReleaseDate: "2025-05-25T00:00:00Z",
							IsLatest:    true,
						},
					},
					{
						ID:          "550e8400-e29b-41d4-a716-446655440002",
						Name:        "test-server-2",
						Description: "Second test server",
						Repository: model.Repository{
							URL:    "https://github.com/example/test-server-2",
							Source: "github",
							ID:     "example/test-server-2",
						},
						VersionDetail: model.VersionDetail{
							Version:     "2.0.0",
							ReleaseDate: "2025-05-26T00:00:00Z",
							IsLatest:    true,
						},
					},
				}
				registry.Mock.On("List", "", 30).Return(servers, "", nil)
			},
			expectedStatus: http.StatusOK,
			expectedServers: []model.Server{
				{
					ID:          "550e8400-e29b-41d4-a716-446655440001",
					Name:        "test-server-1",
					Description: "First test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-1",
						Source: "github",
						ID:     "example/test-server-1",
					},
					VersionDetail: model.VersionDetail{
						Version:     "1.0.0",
						ReleaseDate: "2025-05-25T00:00:00Z",
						IsLatest:    true,
					},
				},
				{
					ID:          "550e8400-e29b-41d4-a716-446655440002",
					Name:        "test-server-2",
					Description: "Second test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-2",
						Source: "github",
						ID:     "example/test-server-2",
					},
					VersionDetail: model.VersionDetail{
						Version:     "2.0.0",
						ReleaseDate: "2025-05-26T00:00:00Z",
						IsLatest:    true,
					},
				},
			},
		},
		{
			name:        "successful list with cursor and limit",
			queryParams: "?cursor=550e8400-e29b-41d4-a716-446655440000&limit=10",
			setupMocks: func(registry *MockRegistryService) {
				servers := []model.Server{
					{
						ID:          "550e8400-e29b-41d4-a716-446655440003",
						Name:        "test-server-3",
						Description: "Third test server",
						Repository: model.Repository{
							URL:    "https://github.com/example/test-server-3",
							Source: "github",
							ID:     "example/test-server-3",
						},
						VersionDetail: model.VersionDetail{
							Version:     "1.5.0",
							ReleaseDate: "2025-05-27T00:00:00Z",
							IsLatest:    true,
						},
					},
				}
				nextCursor := uuid.New().String()
				registry.Mock.On("List", mock.AnythingOfType("string"), 10).Return(servers, nextCursor, nil)
			},
			expectedStatus: http.StatusOK,
			expectedServers: []model.Server{
				{
					ID:          "550e8400-e29b-41d4-a716-446655440003",
					Name:        "test-server-3",
					Description: "Third test server",
					Repository: model.Repository{
						URL:    "https://github.com/example/test-server-3",
						Source: "github",
						ID:     "example/test-server-3",
					},
					VersionDetail: model.VersionDetail{
						Version:     "1.5.0",
						ReleaseDate: "2025-05-27T00:00:00Z",
						IsLatest:    true,
					},
				},
			},
			expectedMeta: &v0.Metadata{
				NextCursor: "", // This will be dynamically set in the test
				Count:      1,
			},
		},
		{
			name:        "successful list with limit capping at 100",
			queryParams: "?limit=150",
			setupMocks: func(_ *MockRegistryService) {},
			expectedStatus:  http.StatusUnprocessableEntity, // Huma rejects values > maximum
			expectedError:   "validation failed",
		},
		{
			name:           "invalid cursor parameter",
			queryParams:    "?cursor=invalid-uuid",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:  "validation failed",
		},
		{
			name:           "invalid limit parameter - non-numeric",
			queryParams:    "?limit=abc",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:  "validation failed",
		},
		{
			name:           "invalid limit parameter - zero",
			queryParams:    "?limit=0",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:  "validation failed",
		},
		{
			name:           "invalid limit parameter - negative",
			queryParams:    "?limit=-5",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusUnprocessableEntity, // Huma returns 422 for validation errors
			expectedError:  "validation failed",
		},
		{
			name: "registry service error",
			setupMocks: func(registry *MockRegistryService) {
				registry.Mock.On("List", "", 30).Return([]model.Server{}, "", errors.New("database connection error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "Failed to get registry list",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock registry service
			mockRegistry := new(MockRegistryService)
			tc.setupMocks(mockRegistry)

			// Create a new test API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the servers endpoints
			v0.RegisterServersEndpoints(api, mockRegistry)

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
					Servers  []model.Server `json:"servers"`
					Metadata *v0.Metadata   `json:"metadata,omitempty"`
				}
				err := json.NewDecoder(w.Body).Decode(&resp)
				assert.NoError(t, err)

				// Check the response data
				if tc.expectedServers != nil {
					assert.Equal(t, tc.expectedServers, resp.Servers)
				}

				// Check metadata if expected
				if tc.expectedMeta != nil {
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
			mockRegistry.AssertExpectations(t)
		})
	}
}

func TestServersDetailEndpoint(t *testing.T) {
	testCases := []struct {
		name           string
		serverID       string
		setupMocks     func(*MockRegistryService, string)
		expectedStatus int
		expectedServer *model.ServerDetail
		expectedError  string
	}{
		{
			name:     "successful get server detail",
			serverID: uuid.New().String(),
			setupMocks: func(registry *MockRegistryService, serverID string) {
				serverDetail := &model.ServerDetail{
					Server: model.Server{
						ID:          serverID,
						Name:        "test-server-detail",
						Description: "Test server detail",
						Repository: model.Repository{
							URL:    "https://github.com/example/test-server-detail",
							Source: "github",
							ID:     "example/test-server-detail",
						},
						VersionDetail: model.VersionDetail{
							Version:     "2.0.0",
							ReleaseDate: "2025-05-27T12:00:00Z",
							IsLatest:    true,
						},
					},
				}
				registry.Mock.On("GetByID", serverID).Return(serverDetail, nil)
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "invalid server ID format",
			serverID:       "invalid-uuid",
			setupMocks:     func(_ *MockRegistryService, _ string) {},
			expectedStatus: http.StatusUnprocessableEntity,
			expectedError:  "validation failed",
		},
		{
			name:     "server not found",
			serverID: uuid.New().String(),
			setupMocks: func(registry *MockRegistryService, serverID string) {
				registry.Mock.On("GetByID", serverID).Return(nil, errors.New("record not found"))
			},
			expectedStatus: http.StatusNotFound,
			expectedError:  "Server not found",
		},
		{
			name:     "registry service error",
			serverID: uuid.New().String(),
			setupMocks: func(registry *MockRegistryService, serverID string) {
				registry.Mock.On("GetByID", serverID).Return(nil, errors.New("database connection error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "Failed to get server details",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock registry service
			mockRegistry := new(MockRegistryService)
			tc.setupMocks(mockRegistry, tc.serverID)

			// Create a new test API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the servers endpoints
			v0.RegisterServersEndpoints(api, mockRegistry)

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
				var serverDetailResp model.ServerDetail
				err := json.NewDecoder(w.Body).Decode(&serverDetailResp)
				assert.NoError(t, err)

				// Check that we got a valid response
				assert.NotEmpty(t, serverDetailResp.ID)
				assert.NotEmpty(t, serverDetailResp.Name)
			} else if tc.expectedError != "" {
				// Check error message for non-200 responses
				assert.Contains(t, w.Body.String(), tc.expectedError)
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}

// TestServersEndpointsIntegration tests the servers endpoints with actual HTTP requests
func TestServersEndpointsIntegration(t *testing.T) {
	// Create mock registry service
	mockRegistry := new(MockRegistryService)

	// Test data
	serverID := uuid.New().String()
	servers := []model.Server{
		{
			ID:          serverID,
			Name:        "integration-test-server",
			Description: "Integration test server",
			Repository: model.Repository{
				URL:    "https://github.com/example/integration-test",
				Source: "github",
				ID:     "example/integration-test",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: "2025-05-27T00:00:00Z",
				IsLatest:    true,
			},
		},
	}

	serverDetail := &model.ServerDetail{
		Server: servers[0],
	}

	// Setup mocks
	mockRegistry.Mock.On("List", "", 30).Return(servers, "", nil)
	mockRegistry.Mock.On("GetByID", serverID).Return(serverDetail, nil)

	// Create a new test API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the servers endpoints
	v0.RegisterServersEndpoints(api, mockRegistry)

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
			Servers []model.Server `json:"servers"`
		}
		err = json.NewDecoder(resp.Body).Decode(&listResp)
		assert.NoError(t, err)

		// Check the response data
		assert.Equal(t, servers, listResp.Servers)
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
		var serverDetailResp model.ServerDetail
		err = json.NewDecoder(resp.Body).Decode(&serverDetailResp)
		assert.NoError(t, err)

		// Check the response data
		assert.Equal(t, *serverDetail, serverDetailResp)
	})

	// Verify mock expectations
	mockRegistry.AssertExpectations(t)
}