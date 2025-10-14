package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PublishServerInput represents the input for publishing a server
type PublishServerInput struct {
	Authorization string           `header:"Authorization" doc:"Registry JWT token (obtained from /v0/auth/token/github)" required:"true"`
	Body          apiv0.ServerJSON `body:""`
}

// RegisterPublishEndpoint registers the publish endpoint with a custom path prefix
func RegisterPublishEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, cfg *config.Config) {
	// Create JWT manager for token validation
	jwtManager := auth.NewJWTManager(cfg)

	huma.Register(api, huma.Operation{
		OperationID: "publish-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/publish",
		Summary:     "Publish MCP server",
		Description: "Publish a new MCP server to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *PublishServerInput) (*Response[apiv0.ServerResponse], error) {
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

		// Verify that the token has permission to publish the server
		if !jwtManager.HasPermission(input.Body.Name, auth.PermissionActionPublish, claims.Permissions) {
			return nil, huma.Error403Forbidden(buildPermissionErrorMessage(input.Body.Name, claims.Permissions))
		}

		// Publish the server with extensions
		publishedServer, err := registry.CreateServer(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish server", err)
		}

		// Return the published server response with metadata
		return &Response[apiv0.ServerResponse]{
			Body: *publishedServer,
		}, nil
	})
}

// buildPermissionErrorMessage creates a detailed error message showing what permissions
// the user has and what they're trying to publish
func buildPermissionErrorMessage(attemptedResource string, permissions []auth.Permission) string {
	var permissionStrs []string
	for _, perm := range permissions {
		if perm.Action == auth.PermissionActionPublish {
			permissionStrs = append(permissionStrs, perm.ResourcePattern)
		}
	}

	errorMsg := "You do not have permission to publish this server"
	if len(permissionStrs) > 0 {
		errorMsg += ". You have permission to publish: " + strings.Join(permissionStrs, ", ")
	} else {
		errorMsg += ". You do not have any publish permissions"
	}
	errorMsg += ". Attempting to publish: " + attemptedResource

	return errorMsg
}
