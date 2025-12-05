package v0_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
)

func TestVersionEndpoint(t *testing.T) {
	// Test cases
	testCases := []struct {
		name         string
		versionInfo  *v0.VersionBody
		expectedBody map[string]string
	}{
		{
			name: "returns version information",
			versionInfo: &v0.VersionBody{
				Version:   "v1.2.3",
				GitCommit: "abc123def456",
				BuildTime: "2025-10-14T12:00:00Z",
			},
			expectedBody: map[string]string{
				"version":    "v1.2.3",
				"git_commit": "abc123def456",
				"build_time": "2025-10-14T12:00:00Z",
			},
		},
		{
			name: "returns dev version information",
			versionInfo: &v0.VersionBody{
				Version:   "dev",
				GitCommit: "unknown",
				BuildTime: "unknown",
			},
			expectedBody: map[string]string{
				"version":    "dev",
				"git_commit": "unknown",
				"build_time": "unknown",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new test API
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			// Register the version endpoint
			v0.RegisterVersionEndpoint(api, "/v0", tc.versionInfo)

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, "/v0/version", nil)
			w := httptest.NewRecorder()

			// Serve the request
			mux.ServeHTTP(w, req)

			// Check the status code
			assert.Equal(t, http.StatusOK, w.Code)

			// Check the response body
			body := w.Body.String()
			assert.Contains(t, body, `"version":"`+tc.expectedBody["version"]+`"`)
			assert.Contains(t, body, `"git_commit":"`+tc.expectedBody["git_commit"]+`"`)
			assert.Contains(t, body, `"build_time":"`+tc.expectedBody["build_time"]+`"`)
		})
	}
}
