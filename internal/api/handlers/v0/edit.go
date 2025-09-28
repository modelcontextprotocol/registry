package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// EditServerInput represents the input for editing a server
type EditServerInput struct {
	Authorization string           `header:"Authorization" doc:"Registry JWT token with edit permissions" required:"true"`
	ServerName    string           `path:"server_name" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
	Version       string           `path:"version" doc:"Version to edit" example:"1.0.0"`
	Body          apiv0.ServerJSON `body:""`
}

// RegisterEditEndpoints registers the edit endpoint
func RegisterEditEndpoints(api huma.API, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	// Edit server endpoint
	huma.Register(api, huma.Operation{
		OperationID: "edit-server",
		Method:      http.MethodPut,
		Path:        "/v0/servers/{server_name}/versions/{version}",
		Summary:     "Edit MCP server",
		Description: "Update a specific version of an existing MCP server (admin only)."
		Tags:        []string{"admin"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *EditServerInput) (*Response[apiv0.ServerResponse], error) {
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

		// URL-decode the server name
		serverName, err := url.QueryUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		// Get current server to check permissions against existing name
		currentServer, err := registry.GetServerByNameAndVersion(ctx, serverName, input.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get current server", err)
		}

		// Verify edit permissions for this server using the existing server name
		if !jwtManager.HasPermission(currentServer.Server.Name, auth.PermissionActionEdit, claims.Permissions) {
			return nil, huma.Error403Forbidden("You do not have edit permissions for this server")
		}

		// Prevent renaming servers
		if currentServer.Server.Name != input.Body.Name {
			return nil, huma.Error400BadRequest("Cannot rename server")
		}

		// Prevent undeleting servers - once deleted, they stay deleted
		if currentServer.Server.Status == model.StatusDeleted && input.Body.Status != model.StatusDeleted {
			return nil, huma.Error400BadRequest("Cannot change status of deleted server. Deleted servers cannot be undeleted.")
		}

		// Update the server using the service
		updatedServer, err := registry.UpdateServer(ctx, serverName, input.Version, &input.Body)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error400BadRequest("Failed to edit server", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *updatedServer,
		}, nil
	})
}
