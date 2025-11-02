package v0_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/stretchr/testify/assert"
)

func TestPingEndpoint(t *testing.T) {
	// Create a new test API
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Register the ping endpoint
	v0.RegisterPingEndpoint(api, "/v0")

	// Create a test request
	req := httptest.NewRequest(http.MethodGet, "/v0/ping", nil)
	w := httptest.NewRecorder()

	// Serve the request
	mux.ServeHTTP(w, req)

	// Check the status code
	assert.Equal(t, http.StatusOK, w.Code)

	// Check the response body contains pong
	body := w.Body.String()
	assert.Contains(t, body, `"pong":true`)
}
