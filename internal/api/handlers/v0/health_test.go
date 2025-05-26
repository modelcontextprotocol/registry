package v0

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestHealthHandler(t *testing.T) {
	// Test cases
	testCases := []struct {
		name           string
		config         *config.Config
		expectedStatus int
		expectedBody   HealthResponse
	}{
		{
			name: "returns health status with github client id",
			config: &config.Config{
				GithubClientID: "test-github-client-id",
			},
			expectedStatus: http.StatusOK,
			expectedBody: HealthResponse{
				Status:         "ok",
				GitHubClientId: "test-github-client-id",
			},
		},
		{
			name: "works with empty github client id",
			config: &config.Config{
				GithubClientID: "",
			},
			expectedStatus: http.StatusOK,
			expectedBody: HealthResponse{
				Status:         "ok",
				GitHubClientId: "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create handler with the test config
			handler := HealthHandler(tc.config)

			// Create request
			req, err := http.NewRequest("GET", "/health", nil)
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
			var resp HealthResponse
			err = json.NewDecoder(rr.Body).Decode(&resp)
			assert.NoError(t, err)

			// Check the response body
			assert.Equal(t, tc.expectedBody, resp)
		})
	}
}

// TestHealthHandlerIntegration tests the handler with actual HTTP requests
func TestHealthHandlerIntegration(t *testing.T) {
	// Create test server
	cfg := &config.Config{
		GithubClientID: "integration-test-client-id",
	}

	server := httptest.NewServer(HealthHandler(cfg))
	defer server.Close()

	// Send request to the test server
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	// Check status code
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Check content type
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Parse response body
	var healthResp HealthResponse
	err = json.NewDecoder(resp.Body).Decode(&healthResp)
	assert.NoError(t, err)

	// Check the response body
	expectedResp := HealthResponse{
		Status:         "ok",
		GitHubClientId: "integration-test-client-id",
	}
	assert.Equal(t, expectedResp, healthResp)
}
