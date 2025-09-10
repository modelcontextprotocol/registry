package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/api"
)

func TestTrailingSlashMiddleware(t *testing.T) {
	// Create a simple handler that returns "OK"
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with our middleware
	middleware := api.TrailingSlashMiddleware(handler)

	tests := []struct {
		name             string
		path             string
		expectedStatus   int
		expectedLocation string
		expectRedirect   bool
	}{
		{
			name:           "root path should not redirect",
			path:           "/",
			expectedStatus: http.StatusOK,
			expectRedirect: false,
		},
		{
			name:           "path without trailing slash should pass through",
			path:           "/v0/servers",
			expectedStatus: http.StatusOK,
			expectRedirect: false,
		},
		{
			name:             "path with trailing slash should redirect",
			path:             "/v0/servers/",
			expectedStatus:   http.StatusPermanentRedirect,
			expectedLocation: "/v0/servers",
			expectRedirect:   true,
		},
		{
			name:             "nested path with trailing slash should redirect",
			path:             "/v0/servers/123/",
			expectedStatus:   http.StatusPermanentRedirect,
			expectedLocation: "/v0/servers/123",
			expectRedirect:   true,
		},
		{
			name:             "deep nested path with trailing slash should redirect",
			path:             "/v0/auth/github/token/",
			expectedStatus:   http.StatusPermanentRedirect,
			expectedLocation: "/v0/auth/github/token",
			expectRedirect:   true,
		},
		{
			name:           "path with query params and no trailing slash should pass through",
			path:           "/v0/servers?limit=10",
			expectedStatus: http.StatusOK,
			expectRedirect: false,
		},
		{
			name:             "path with query params and trailing slash should redirect preserving query params",
			path:             "/v0/servers/?limit=10",
			expectedStatus:   http.StatusPermanentRedirect,
			expectedLocation: "/v0/servers?limit=10",
			expectRedirect:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			w := httptest.NewRecorder()

			middleware.ServeHTTP(w, req)

			if w.Code != tt.expectedStatus {
				t.Errorf("expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectRedirect {
				location := w.Header().Get("Location")
				if location != tt.expectedLocation {
					t.Errorf("expected Location header %q, got %q", tt.expectedLocation, location)
				}
			}
		})
	}
}
