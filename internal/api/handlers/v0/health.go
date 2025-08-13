package v0

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/config"
)

// HealthBody represents the health check response body
type HealthBody struct {
	Status         string `json:"status" example:"ok" doc:"Health status"`
	GitHubClientID string `json:"github_client_id,omitempty" doc:"GitHub OAuth App Client ID"`
}

// RegisterHealthEndpoint registers the health check endpoint
func RegisterHealthEndpoint(api huma.API, cfg *config.Config) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health",
		Method:      http.MethodGet,
		Path:        "/v0/health",
		Summary:     "Health check",
		Description: "Check the health status of the API",
		Tags:        []string{"health"},
	}, func(_ context.Context, _ *struct{}) (*Response[HealthBody], error) {
		return &Response[HealthBody]{
			Body: HealthBody{
				Status:         "ok",
				GitHubClientID: cfg.GithubClientID,
			},
		}, nil
	})
}
