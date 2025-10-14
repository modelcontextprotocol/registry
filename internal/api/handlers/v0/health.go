package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

// HealthBody represents the health check response body
type HealthBody struct {
	Status         string `json:"status" example:"ok" doc:"Health status"`
	GitHubClientID string `json:"github_client_id,omitempty" doc:"GitHub OAuth App Client ID"`
}

// RegisterHealthEndpoint registers the health check endpoint with a custom path prefix
func RegisterHealthEndpoint(api huma.API, pathPrefix string, cfg *config.Config, metrics *telemetry.Metrics) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/health",
		Summary:     "Health check",
		Description: "Check the health status of the API",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*Response[HealthBody], error) {
		// Record the health check metrics
		recordHealthMetrics(ctx, metrics, pathPrefix+"/health", cfg.Version)

		return &Response[HealthBody]{
			Body: HealthBody{
				Status:         "ok",
				GitHubClientID: cfg.GithubClientID,
			},
		}, nil
	})
}

// recordHealthMetrics records the health check metrics
func recordHealthMetrics(ctx context.Context, metrics *telemetry.Metrics, path string, version string) {
	attrs := []attribute.KeyValue{
		attribute.String("path", path),
		attribute.String("version", version),
		attribute.String("service", telemetry.Namespace),
	}

	// metric : Up status (1 = healthy, 0 = unhealthy)
	metrics.Up.Record(ctx, 1, metric.WithAttributes(attrs...))
}
