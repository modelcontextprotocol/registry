// Package router contains API routing logic
package router

import (
	"context"
	"encoding/json"
	"errors"
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

// statusClientClosed mirrors NGINX's non-standard 499 — used for requests
// where the client disconnected before we finished. Distinguishing these from
// real server errors keeps the availability metric meaningful under bursts of
// scraper traffic that time out and reconnect.
const statusClientClosed = 499

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

		// If the client disconnected before the handler finished, the handler
		// likely converted the resulting context.Canceled into a huma 5xx and
		// tried to write a response to a closed socket. NGINX records that
		// case as a 499 (client closed). Without this remap we count it as a
		// server error: a single ServiceNow-style burst that times out a
		// few thousand list-servers requests inflates http_errors_total even
		// though no client ever saw a 5xx, and the availability alert fires
		// on what is effectively just slow responses.
		//
		// Only context.Canceled is remapped — context.DeadlineExceeded would
		// indicate a server-side timeout we set ourselves and should still
		// count as a server error if/when we add per-request deadlines.
		if reqErr := ctx.Context().Err(); reqErr != nil && errors.Is(reqErr, context.Canceled) {
			statusCode = statusClientClosed
		}

		// Combine common and custom attributes
		attrs := []attribute.KeyValue{
			attribute.String("method", method),
			attribute.String("path", routePath),
			attribute.Int("status_code", statusCode),
		}

		// Record metrics
		metrics.Requests.Add(ctx.Context(), 1, metric.WithAttributes(attrs...))

		// Skip the error counter for client-closed requests so the availability
		// metric reflects server-visible errors only.
		if statusCode >= 400 && statusCode != statusClientClosed {
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
	w.Header().Set("X-Content-Type-Options", "nosniff")
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

	// Add UI and 404 handler for all other routes
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			// Serve UI at root. The page renders publisher-controlled content
			// (server names, descriptions, repository URLs) — server-side
			// validation plus a JS escape function are the primary XSS
			// defences; these headers are defence-in-depth in case any of
			// those slip.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			// connect-src is unrestricted because the UI exposes a base-URL
			// selector (prod / staging / custom) that issues cross-origin
			// XHRs to whichever target the operator picks. Constraining
			// connect-src would silently break that affordance. The other
			// directives still meaningfully limit the page's attack surface.
			w.Header().Set("Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; "+
					"style-src 'self' 'unsafe-inline' https://cdn.tailwindcss.com; "+
					"img-src 'self' data:; "+
					"connect-src *; "+
					"frame-ancestors 'none'; "+
					"base-uri 'self'; "+
					"form-action 'self'")
			_, err := w.Write([]byte(v0.GetUIHTML()))
			if err != nil {
				http.Error(w, "Failed to write response", http.StatusInternalServerError)
			}
			return
		}

		// Handle 404 for all non-matched routes
		handle404(w, r)
	})

	return api
}
