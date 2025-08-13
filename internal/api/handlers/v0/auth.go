package v0

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// StartAuthInput represents the input for starting an auth flow
type StartAuthInput struct {
	Body struct {
		Method  string `json:"method" doc:"Authentication method" example:"github"`
		RepoRef string `json:"repo_ref" doc:"Repository reference" example:"owner/repo"`
	}
}

// StartAuthBody represents the auth flow start response body
type StartAuthBody struct {
	AuthFlowInfo map[string]string `json:"auth_flow_info" doc:"Authentication flow information"`
	StatusToken  string            `json:"status_token" doc:"Token to check auth status"`
}

// CheckAuthStatusInput represents the input for checking auth status
type CheckAuthStatusInput struct {
	StatusToken string `path:"token" doc:"Status token from auth flow start"`
}

// CheckAuthStatusBody represents the auth status response body
type CheckAuthStatusBody struct {
	Status string `json:"status" doc:"Authentication status" example:"pending"`
}

// RegisterAuthEndpoints registers all auth-related endpoints
func RegisterAuthEndpoints(api huma.API, authService auth.Service) {
	// Start auth flow endpoint
	huma.Register(api, huma.Operation{
		OperationID: "start-auth",
		Method:      http.MethodPost,
		Path:        "/v0/auth/start",
		Summary:     "Start authentication flow",
		Description: "Start an authentication flow for publishing servers",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *StartAuthInput) (*Response[StartAuthBody], error) {
		// Validate required fields
		if input.Body.Method == "" {
			return nil, huma.Error400BadRequest("Auth method is required")
		}
		if input.Body.RepoRef == "" {
			return nil, huma.Error400BadRequest("Repository reference is required")
		}

		// Convert string method to AuthMethod type
		var authMethod model.AuthMethod
		switch input.Body.Method {
		case "github":
			authMethod = model.AuthMethodGitHub
		case "none":
			authMethod = model.AuthMethodNone
		default:
			return nil, huma.Error400BadRequest("Invalid auth method: " + input.Body.Method)
		}

		// Start the auth flow
		flowInfo, statusToken, err := authService.StartAuthFlow(ctx, authMethod, input.Body.RepoRef)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to start auth flow", err)
		}

		return &Response[StartAuthBody]{
			Body: StartAuthBody{
				AuthFlowInfo: flowInfo,
				StatusToken:  statusToken,
			},
		}, nil
	})

	// Check auth status endpoint
	huma.Register(api, huma.Operation{
		OperationID: "check-auth-status",
		Method:      http.MethodGet,
		Path:        "/v0/auth/status/{token}",
		Summary:     "Check authentication status",
		Description: "Check the status of an ongoing authentication flow",
		Tags:        []string{"auth"},
	}, func(ctx context.Context, input *CheckAuthStatusInput) (*Response[CheckAuthStatusBody], error) {
		// Check the auth status
		status, err := authService.CheckAuthStatus(ctx, input.StatusToken)
		if err != nil {
			return nil, huma.Error404NotFound("Auth flow not found or expired")
		}

		return &Response[CheckAuthStatusBody]{
			Body: CheckAuthStatusBody{
				Status: status,
			},
		}, nil
	})
}