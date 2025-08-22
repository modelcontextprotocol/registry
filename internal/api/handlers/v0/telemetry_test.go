package v0_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/api/router"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

func mockServerEndpoint(registry *MockRegistryService, serverID string) {
	serverDetail := &model.ServerResponse{
		Server: model.ServerDetail{
			Name:        "test-server-detail",
			Description: "Test server detail",
			Repository: model.Repository{
				URL:    "https://github.com/example/test-server-detail",
				Source: "github",
				ID:     "example/test-server-detail",
			},
			VersionDetail: model.VersionDetail{
				Version: "2.0.0",
			},
		},
		XIOModelContextProtocolRegistry: map[string]interface{}{
			"id": serverID,
		},
	}
	registry.Mock.On("GetByID", serverID).Return(serverDetail, nil)
}

func TestPrometheusHandler(t *testing.T) {
	mockRegistry := new(MockRegistryService)

	serverID := uuid.New().String()
	mockServerEndpoint(mockRegistry, serverID)

	cfg := config.NewConfig()
	shutdownTelemetry, metrics, _ := telemetry.InitMetrics("dev")

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

	// Add metrics middleware with options
	api.UseMiddleware(router.MetricTelemetryMiddleware(metrics,
		router.WithSkipPaths("/health", "/metrics", "/ping", "/docs"),
	))
	v0.RegisterHealthEndpoint(api, cfg, metrics)
	v0.RegisterServersEndpoints(api, mockRegistry)

	// Add /metrics for Prometheus metrics using promhttp
	mux.Handle("/metrics", metrics.PrometheusHandler())

	// Create request
	url := "/v0/servers/" + serverID
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
	assert.Contains(t, body, "path=\"/v0/servers/{id}\"")
}
