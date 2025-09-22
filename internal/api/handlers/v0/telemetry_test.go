package v0_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestPrometheusHandler(t *testing.T) {
	registryService := service.NewRegistryService(database.NewMemoryDB(), config.NewConfig())
	server, err := registryService.Publish(apiv0.ServerJSON{
		Name:        "io.github.example/test-server",
		Description: "Test server detail",
		Repository: model.Repository{
			URL:    "https://github.com/example/test-server",
			Source: "github",
			ID:     "example/test-server",
		},
		Version: "2.0.0",
	})
	assert.NoError(t, err)

	cfg := config.NewConfig()
	shutdownTelemetry, metrics, _ := telemetry.InitMetrics("dev")

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Add metrics middleware with options
	api.UseMiddleware(router.MetricTelemetryMiddleware(metrics,
		router.WithSkipPaths("/health", "/metrics", "/ping", "/docs"),
	))
	v0.RegisterHealthEndpoint(api, cfg, metrics)
	v0.RegisterServersEndpoints(api, registryService)

	// Add /metrics for Prometheus metrics using promhttp
	mux.Handle("/metrics", metrics.PrometheusHandler())

	// Create request
	url := "/v0/servers/" + server.Meta.Official.ServerID
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()

	// Serve the request
	mux.ServeHTTP(w, req)

	// Check the status code
	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// shutdown metrics provider
	_ = shutdownTelemetry(context.Background())

	assert.Equal(t, http.StatusOK, w.Code, "Expected status OK for /metrics endpoint")

	body := w.Body.String()
	// Check if the response body contains expected metrics
	assert.Contains(t, body, "mcp_registry_http_request_duration_bucket")
	assert.Contains(t, body, "mcp_registry_http_requests_total")
	assert.Contains(t, body, "path=\"/v0/servers/{server_id}\"")
}
