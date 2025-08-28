package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PublishServerInput represents the input for publishing a server
type PublishServerInput struct {
	Authorization string               `header:"Authorization" doc:"Registry JWT token (obtained from /v0/auth/token/github)" required:"true"`
	Body          apiv0.PublishRequest `body:""`
}

// RegisterPublishEndpoint registers the publish endpoint
func RegisterPublishEndpoint(api huma.API, registry service.RegistryService, cfg *config.Config) {
	// Create JWT manager for token validation
	jwtManager := auth.NewJWTManager(cfg)

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
	}, func(ctx context.Context, input *PublishServerInput) (*Response[apiv0.ServerRecord], error) {
		// Extract bearer token
		const bearerPrefix = "Bearer "
		authHeader := input.Authorization
		if len(authHeader) < len(bearerPrefix) || !strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			return nil, huma.Error401Unauthorized("Invalid Authorization header format. Expected 'Bearer <token>'")
		}
		token := authHeader[len(bearerPrefix):]

		// Validate Registry JWT token
		claims, err := jwtManager.ValidateToken(ctx, token)
		if err != nil {
			return nil, huma.Error401Unauthorized("Invalid or expired Registry JWT token", err)
		}

		// Perform all schema validation
		if err := validators.ValidatePublishRequest(input.Body); err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}

		// Verify that the token has permission to publish the server
		if !jwtManager.HasPermission(input.Body.Server.Name, auth.PermissionActionPublish, claims.Permissions) {
			return nil, huma.Error403Forbidden("You do not have permission to publish this server")
		}

		// Publish the server with extensions
		publishedServer, err := registry.Publish(input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish server", err)
		}

		// Return the published server in extension wrapper format
		return &Response[apiv0.ServerRecord]{
			Body: *publishedServer,
		}, nil
	})
}
