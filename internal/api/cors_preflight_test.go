package api_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/api"
	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// TestCORSPreflightAllowedMethods checks that the CORS layer permits every HTTP
// method the API actually routes. The status endpoints
// (PATCH /v0/servers/{name}/status and .../versions/{version}/status) are served
// with PATCH, so a cross-origin browser preflight for PATCH has to be allowed or
// the browser refuses to send the real request. Guards against a routed method
// being missing from AllowedMethods.
func TestCORSPreflightAllowedMethods(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(seed)
	require.NoError(t, err)

	cfg := config.NewConfig()
	cfg.JWTPrivateKey = hex.EncodeToString(seed)

	shutdownTelemetry, metrics, err := telemetry.InitMetrics("test")
	require.NoError(t, err)
	defer func() { _ = shutdownTelemetry(nil) }()

	// registryService is nil on purpose: a CORS preflight is answered by the
	// middleware before any route handler runs, so business logic is never hit.
	srv := api.NewServer(cfg, nil, metrics, &v0.VersionBody{Version: "test"})

	for _, method := range []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
	} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodOptions, "/v0/servers/example/status", nil)
			req.Header.Set("Origin", "https://example.com")
			req.Header.Set("Access-Control-Request-Method", method)

			w := httptest.NewRecorder()
			srv.Handler().ServeHTTP(w, req)

			allow := w.Header().Get("Access-Control-Allow-Methods")
			assert.Contains(t, allow, method,
				"CORS preflight for %s should be allowed, got Access-Control-Allow-Methods=%q", method, allow)
		})
	}
}
