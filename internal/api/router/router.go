// Package router contains API routing logic
package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	v0 "github.com/modelcontextprotocol/registry/internal/api/handlers/v0"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// Middleware configuration options
type middlewareConfig struct {
	skipPaths map[string]bool
}

type MiddlewareOption func(*middlewareConfig)

// getRoutePath extracts the route pattern from the context
func getRoutePath(ctx huma.Context) string {
	// Try to get the operation from context
	if op := ctx.Operation().Path; op != "" {
		return ctx.Operation().Path
	}

	// Fallback to URL path (less ideal for metrics as it includes path parameters)
	return ctx.URL().Path
}

func MetricTelemetryMiddleware(metrics *telemetry.Metrics, options ...MiddlewareOption) func(huma.Context, func(huma.Context)) {
	config := &middlewareConfig{
		skipPaths: make(map[string]bool),
	}

	for _, opt := range options {
		opt(config)
	}

	return func(ctx huma.Context, next func(huma.Context)) {
		path := ctx.URL().Path

		// Skip instrumentation for specified paths
		// extract the last part of the path to match against skipPaths
		pathParts := strings.Split(path, "/")
		pathToMatch := "/" + pathParts[len(pathParts)-1]
		if config.skipPaths[pathToMatch] || config.skipPaths[path] {
			next(ctx)
			return
		}

		start := time.Now()
		method := ctx.Method()
		routePath := getRoutePath(ctx)

		next(ctx)

		duration := time.Since(start).Seconds()
		statusCode := ctx.Status()

		// Combine common and custom attributes
		attrs := []attribute.KeyValue{
			attribute.String("method", method),
			attribute.String("path", routePath),
			attribute.Int("status_code", statusCode),
		}

		// Record metrics
		metrics.Requests.Add(ctx.Context(), 1, metric.WithAttributes(attrs...))

		if statusCode >= 400 {
			metrics.ErrorCount.Add(ctx.Context(), 1, metric.WithAttributes(attrs...))
		}

		metrics.RequestDuration.Record(ctx.Context(), duration, metric.WithAttributes(attrs...))
	}
}

// WithSkipPaths allows skipping instrumentation for specific paths
func WithSkipPaths(paths ...string) MiddlewareOption {
	return func(c *middlewareConfig) {
		for _, path := range paths {
			c.skipPaths[path] = true
		}
	}
}

// handle404 returns a helpful 404 error with suggestions for common mistakes
func handle404(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(http.StatusNotFound)

	path := r.URL.Path
	detail := "Endpoint not found. See /docs for the API documentation."

	// Provide suggestions for common API endpoint mistakes
	if !strings.HasPrefix(path, "/v0/") && !strings.HasPrefix(path, "/v0.1/") {
		detail = fmt.Sprintf(
			"Endpoint not found. Did you mean '%s' or '%s'? See /docs for the API documentation.",
			"/v0.1"+path,
			"/v0"+path,
		)
	}

	errorBody := map[string]interface{}{
		"title":  "Not Found",
		"status": 404,
		"detail": detail,
	}

	// Use JSON marshal to ensure consistent formatting
	jsonData, err := json.Marshal(errorBody)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonData)
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// NewHumaAPI creates a new Huma API with all routes registered
func NewHumaAPI(cfg *config.Config, registry service.RegistryService, mux *http.ServeMux, metrics *telemetry.Metrics, versionInfo *v0.VersionBody) huma.API {
	// Create Huma API configuration
	humaConfig := huma.DefaultConfig("Official MCP Registry", "1.0.0")
	humaConfig.Info.Description = "A community driven registry service for Model Context Protocol (MCP) servers.\n\n[GitHub repository](https://github.com/modelcontextprotocol/registry) | [Documentation](https://github.com/modelcontextprotocol/registry/tree/main/docs)"
	// Disable $schema property in responses: https://github.com/danielgtaylor/huma/issues/230
	humaConfig.CreateHooks = []func(huma.Config) huma.Config{}

	// Create a new API using humago adapter for standard library
	api := humago.New(mux, humaConfig)

	// Add OpenAPI tag metadata with descriptions
	api.OpenAPI().Tags = []*huma.Tag{
		{
			Name:        "servers",
			Description: "Operations for discovering and retrieving MCP servers",
		},
		{
			Name:        "publish",
			Description: "Operations for publishing MCP servers to the registry",
		},
		{
			Name:        "auth",
			Description: "Authentication operations for obtaining tokens to publish servers",
		},
		{
			Name:        "admin",
			Description: "Administrative operations for managing servers (requires elevated permissions)",
		},
		{
			Name:        "health",
			Description: "Health check endpoint for monitoring service availability",
		},
		{
			Name:        "ping",
			Description: "Simple ping endpoint for testing connectivity",
		},
		{
			Name:        "version",
			Description: "Version information endpoint for retrieving build and version details",
		},
	}

	// Add metrics middleware with options
	api.UseMiddleware(MetricTelemetryMiddleware(metrics,
		WithSkipPaths("/health", "/metrics", "/ping", "/docs"),
	))

	// Register routes for all API versions
	RegisterV0Routes(api, cfg, registry, metrics, versionInfo)
	RegisterV0_1Routes(api, cfg, registry, metrics, versionInfo)

	// Add /metrics for Prometheus metrics using promhttp
	mux.Handle("/metrics", metrics.PrometheusHandler())

	// Add redirect from / to docs and 404 handler for all other routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "https://github.com/modelcontextprotocol/registry/tree/main/docs", http.StatusTemporaryRedirect)
			return
		}

		// Handle 404 for all other routes
		handle404(w, r)
	})

	return api
}
