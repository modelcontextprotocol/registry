package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTrailingSlashMiddleware(t *testing.T) {
	// Create a simple handler that returns "OK"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with our middleware
	middleware := TrailingSlashMiddleware(handler)

	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedLocation string
		expectRedirect bool
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
			name:           "path with trailing slash should redirect",
			path:           "/v0/servers/",
			expectedStatus: http.StatusMovedPermanently,
			expectedLocation: "/v0/servers",
			expectRedirect: true,
		},
		{
			name:           "nested path with trailing slash should redirect",
			path:           "/v0/servers/123/",
			expectedStatus: http.StatusMovedPermanently,
			expectedLocation: "/v0/servers/123",
			expectRedirect: true,
		},
		{
			name:           "deep nested path with trailing slash should redirect", 
			path:           "/v0/auth/github/token/",
			expectedStatus: http.StatusMovedPermanently,
			expectedLocation: "/v0/auth/github/token",
			expectRedirect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.path, nil)
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