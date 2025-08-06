package v0_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestServersHandler(t *testing.T) {
	testCases := []struct {
		name            string
		method          string
		queryParams     string
		setupMocks      func(*MockRegistryService)
		expectedStatus  int
		expectedServers []model.Server
		expectedMeta    *v0.Metadata
		expectedError   string
	}{
		{
			name:   "successful list with default parameters",
			method: http.MethodGet,
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
			method:      http.MethodGet,
			queryParams: "?cursor=550e8400-e29b-41d4-a716-446655440000" + "&limit=10",
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
			method:      http.MethodGet,
			queryParams: "?limit=150",
			setupMocks: func(registry *MockRegistryService) {
				servers := []model.Server{}
				registry.Mock.On("List", "", 100).Return(servers, "", nil)
			},
			expectedStatus:  http.StatusOK,
			expectedServers: []model.Server{},
		},
		{
			name:           "invalid cursor parameter",
			method:         http.MethodGet,
			queryParams:    "?cursor=invalid-uuid",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid cursor parameter",
		},
		{
			name:           "invalid limit parameter - non-numeric",
			method:         http.MethodGet,
			queryParams:    "?limit=abc",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Invalid limit parameter",
		},
		{
			name:           "invalid limit parameter - zero",
			method:         http.MethodGet,
			queryParams:    "?limit=0",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Limit must be greater than 0",
		},
		{
			name:           "invalid limit parameter - negative",
			method:         http.MethodGet,
			queryParams:    "?limit=-5",
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "Limit must be greater than 0",
		},
		{
			name:   "registry service error",
			method: http.MethodGet,
			setupMocks: func(registry *MockRegistryService) {
				registry.Mock.On("List", "", 30).Return([]model.Server{}, "", errors.New("database connection error"))
			},
			expectedStatus: http.StatusInternalServerError,
			expectedError:  "database connection error",
		},
		{
			name:           "method not allowed",
			method:         http.MethodPost,
			setupMocks:     func(_ *MockRegistryService) {},
			expectedStatus: http.StatusMethodNotAllowed,
			expectedError:  "Method not allowed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create mock registry service
			mockRegistry := new(MockRegistryService)
			tc.setupMocks(mockRegistry)

			// Create handler
			handler := v0.ServersHandler(mockRegistry)

			// Create request
			url := "/v0/servers" + tc.queryParams
			req, err := http.NewRequestWithContext(context.Background(), tc.method, url, nil)
			if err != nil {
				t.Fatal(err)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call the handler
			handler.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			if tc.expectedStatus == http.StatusOK {
				// Check content type
				assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

				// Parse response body
				var resp v0.PaginatedResponse
				err = json.NewDecoder(rr.Body).Decode(&resp)
				assert.NoError(t, err)

				// Check the response data
				assert.Equal(t, tc.expectedServers, resp.Data)

				// Check metadata if expected
				if tc.expectedMeta != nil {
					assert.Equal(t, tc.expectedMeta.Count, resp.Metadata.Count)
					if tc.expectedMeta.NextCursor != "" {
						assert.NotEmpty(t, resp.Metadata.NextCursor)
					}
				}
			} else if tc.expectedError != "" {
				// Check error message for non-200 responses
				assert.Contains(t, rr.Body.String(), tc.expectedError)
			}

			// Verify mock expectations
			mockRegistry.AssertExpectations(t)
		})
	}
}

// TestServersHandlerIntegration tests the servers list handler with actual HTTP requests
func TestServersHandlerIntegration(t *testing.T) {
	// Create mock registry service
	mockRegistry := new(MockRegistryService)

	servers := []model.Server{
		{
			ID:          "550e8400-e29b-41d4-a716-446655440004",
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

	mockRegistry.Mock.On("List", "", 30).Return(servers, "", nil)

	// Create test server
	server := httptest.NewServer(v0.ServersHandler(mockRegistry))
	defer server.Close()

	// Send request to the test server
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
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
	var paginatedResp v0.PaginatedResponse
	err = json.NewDecoder(resp.Body).Decode(&paginatedResp)
	assert.NoError(t, err)

	// Check the response data
	assert.Equal(t, servers, paginatedResp.Data)
	assert.Empty(t, paginatedResp.Metadata.NextCursor)

	// Verify mock expectations
	mockRegistry.AssertExpectations(t)
}

// TestServersDetailHandlerIntegration tests the servers detail handler with actual HTTP requests
func TestServersDetailHandlerIntegration(t *testing.T) {
	serverID := uuid.New().String()

	// Create mock registry service
	mockRegistry := new(MockRegistryService)

	serverDetail := &model.ServerDetail{
		Server: model.Server{
			ID:          serverID,
			Name:        "integration-test-server-detail",
			Description: "Integration test server detail",
			Repository: model.Repository{
				URL:    "https://github.com/example/integration-test-detail",
				Source: "github",
				ID:     "example/integration-test-detail",
			},
			VersionDetail: model.VersionDetail{
				Version:     "2.0.0",
				ReleaseDate: "2025-05-27T12:00:00Z",
				IsLatest:    true,
			},
		},
	}

	mockRegistry.Mock.On("GetByID", serverID).Return(serverDetail, nil)

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("id", serverID)
		v0.ServersDetailHandler(mockRegistry).ServeHTTP(w, r)
	}))
	defer server.Close()

	// Send request to the test server
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, nil)
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

	// Verify mock expectations
	mockRegistry.AssertExpectations(t)
}
