package api_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/registry/internal/api"
)

func TestNulByteValidationMiddleware(t *testing.T) {
	// Create a simple handler that returns "OK"
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	// Wrap with our middleware
	middleware := api.NulByteValidationMiddleware(handler)

	t.Run("normal path should pass through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("path with query params should pass through", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers?cursor=abc123", nil)
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
		}
	})

	t.Run("path with NUL byte should return 400", func(t *testing.T) {
		// Create request with NUL byte in path by manually setting URL
		req := httptest.NewRequest(http.MethodGet, "/v0/servers/test", nil)
		req.URL.Path = "/v0/servers/\x00"
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
		if !strings.Contains(w.Body.String(), "URL path contains null bytes") {
			t.Errorf("expected body to contain error message, got %q", w.Body.String())
		}
	})

	t.Run("query with NUL byte should return 400", func(t *testing.T) {
		// Create request with NUL byte in query by manually setting RawQuery
		req := httptest.NewRequest(http.MethodGet, "/v0/servers", nil)
		req.URL.RawQuery = "cursor=\x00"
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
		if !strings.Contains(w.Body.String(), "query parameters contain null bytes") {
			t.Errorf("expected body to contain error message, got %q", w.Body.String())
		}
	})

	t.Run("path with embedded NUL byte should return 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v0/servers/test", nil)
		req.URL.Path = "/v0/servers/test\x00name"
		w := httptest.NewRecorder()
		middleware.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
		}
	})
}

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
