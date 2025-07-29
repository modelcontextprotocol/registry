package v0_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/stretchr/testify/assert"
)

// Mock database for testing
// Supports passing dbType
type mockDatabase struct {
	dbType string
}

// Implement List method to satisfy database.Database interface
func (m *mockDatabase) List(ctx context.Context, filter map[string]any, cursor string, limit int) ([]*model.Server, string, error) {
	return []*model.Server{}, "", nil
}

func (m *mockDatabase) Connection() *database.ConnectionInfo {
	return &database.ConnectionInfo{
		IsConnected:     true,
		Type:            database.ConnectionType(m.dbType),
		CollectionCount: 0,
	}
}

func (m *mockDatabase) GetByID(ctx context.Context, id string) (*model.ServerDetail, error) {
	return nil, database.ErrNotFound
}

// Implement Publish method to satisfy database.Database interface
func (m *mockDatabase) Publish(ctx context.Context, serverDetail *model.ServerDetail) error {
	return nil
}

func (m *mockDatabase) ImportSeed(ctx context.Context, seedFilePath string) error {
	return nil
}

func (m *mockDatabase) Close() error {
	return nil
}

func TestHealthHandler(t *testing.T) {
	// Test cases
	testCases := []struct {
		name           string
		config         *config.Config
		dbType         string
		expectedStatus int
		expectedBody   v0.HealthResponse
	}{
		{
			name: "returns health status with github client id (memory)",
			config: &config.Config{
				GithubClientID: "test-github-client-id",
			},
			dbType:         "memory",
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthResponse{
				Status:         "ok",
				GitHubClientID: "test-github-client-id",
			},
		},
		{
			name: "returns health status with github client id (mongo)",
			config: &config.Config{
				GithubClientID: "test-github-client-id",
			},
			dbType:         "mongo",
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthResponse{
				Status:         "ok",
				GitHubClientID: "test-github-client-id",
			},
		},
		{
			name: "works with empty github client id (memory)",
			config: &config.Config{
				GithubClientID: "",
			},
			dbType:         "memory",
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthResponse{
				Status:         "ok",
				GitHubClientID: "",
			},
		},
		{
			name: "works with empty github client id (mongo)",
			config: &config.Config{
				GithubClientID: "",
			},
			dbType:         "mongo",
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthResponse{
				Status:         "ok",
				GitHubClientID: "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create handler with the test config and mock database
			handler := v0.HealthHandler(tc.config, &mockDatabase{dbType: tc.dbType})

			// Create request
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
			if err != nil {
				t.Fatal(err)
			}

			// Create response recorder
			rr := httptest.NewRecorder()

			// Call the handler
			handler.ServeHTTP(rr, req)

			// Check status code
			assert.Equal(t, tc.expectedStatus, rr.Code)

			// Check content type
			assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

			// Parse response body
			var resp v0.HealthResponse
			err = json.NewDecoder(rr.Body).Decode(&resp)
			assert.NoError(t, err)

			// Check the response body
			assert.Equal(t, tc.expectedBody.Status, resp.Status)
			assert.Equal(t, tc.expectedBody.GitHubClientID, resp.GitHubClientID)
		})
	}
}

// Integration test using memory database type
func TestHealthHandlerIntegration(t *testing.T) {
	// Create test server
	cfg := &config.Config{
		GithubClientID: "integration-test-client-id",
	}

	server := httptest.NewServer(v0.HealthHandler(cfg, &mockDatabase{dbType: "memory"}))
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
	var healthResp v0.HealthResponse
	err = json.NewDecoder(resp.Body).Decode(&healthResp)
	assert.NoError(t, err)

	// Check the response body
	expectedResp := v0.HealthResponse{
		Status:         "ok",
		GitHubClientID: "integration-test-client-id",
	}
	assert.Equal(t, expectedResp.Status, healthResp.Status)
	assert.Equal(t, expectedResp.GitHubClientID, healthResp.GitHubClientID)
}
