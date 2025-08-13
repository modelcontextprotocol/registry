package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// PublishServerInput represents the input for publishing a server
type PublishServerInput struct {
	Authorization string `header:"Authorization" doc:"GitHub OAuth token" required:"true"`
	Body          model.PublishRequest
}


// RegisterPublishEndpoint registers the publish endpoint
func RegisterPublishEndpoint(api huma.API, registry service.RegistryService, authService auth.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "publish-server",
		Method:      http.MethodPost,
		Path:        "/v0/publish",
		Summary:     "Publish MCP server",
		Description: "Publish a new MCP server to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *PublishServerInput) (*Response[model.Server], error) {
		// Extract bearer token
		const bearerPrefix = "Bearer "
		authHeader := input.Authorization
		if len(authHeader) < len(bearerPrefix) || !strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			return nil, huma.Error401Unauthorized("Invalid Authorization header format. Expected 'Bearer <token>'")
		}
		token := authHeader[len(bearerPrefix):]

		// Convert PublishRequest body to ServerDetail
		serverDetail := input.Body.ServerDetail

		// Huma handles validation automatically based on struct tags
		// But we can add custom validation if needed
		if serverDetail.Name == "" {
			return nil, huma.Error400BadRequest("Name is required")
		}
		if serverDetail.VersionDetail.Version == "" {
			return nil, huma.Error400BadRequest("Version is required")
		}

		// Determine authentication method based on server name prefix
		var authMethod model.AuthMethod
		if strings.HasPrefix(serverDetail.Name, "io.github") {
			authMethod = model.AuthMethodGitHub
		} else {
			authMethod = model.AuthMethodNone
		}

		// Setup authentication info
		a := model.Authentication{
			Method:  authMethod,
			Token:   token,
			RepoRef: serverDetail.Name,
		}

		// Validate authentication
		valid, err := authService.ValidateAuth(ctx, a)
		if err != nil {
			return nil, huma.Error401Unauthorized("Authentication failed", err)
		}
		if !valid {
			return nil, huma.Error401Unauthorized("Invalid authentication credentials")
		}

		// Publish the server details
		err = registry.Publish(&serverDetail)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to publish server", err)
		}

		// Create response with the published server data
		return &Response[model.Server]{
			Body: model.Server{
				ID:            serverDetail.ID,
				Name:          serverDetail.Name,
				Description:   serverDetail.Description,
				Status:        serverDetail.Status,
				Repository:    serverDetail.Repository,
				VersionDetail: serverDetail.VersionDetail,
			},
		}, nil
	})
}
