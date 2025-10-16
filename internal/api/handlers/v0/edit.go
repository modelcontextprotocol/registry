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
	ServerName    string           `path:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
	Version       string           `path:"version" doc:"URL-encoded version to edit" example:"1.0.0"`
	Status        string           `query:"status" doc:"New status for the server (active, deprecated, deleted)" required:"false" enum:"active,deprecated,deleted"`
	Body          apiv0.ServerJSON `body:""`
}

// RegisterEditEndpoints registers the edit endpoint with a custom path prefix
func RegisterEditEndpoints(api huma.API, pathPrefix string, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	// Edit server endpoint
	huma.Register(api, huma.Operation{
		OperationID: "edit-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPut,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}",
		Summary:     "Edit MCP server",
		Description: "Update a specific version of an existing MCP server (admin only).",
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
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		// URL-decode the version
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Get current server to check permissions against existing name
		currentServer, err := registry.GetServerByNameAndVersion(ctx, serverName, version)
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

		// Validate that the version in the body matches the URL parameter
		if input.Body.Version != version {
			return nil, huma.Error400BadRequest("Version in request body must match URL path parameter")
		}

		// Handle status changes with proper permission validation
		if input.Status != "" {
			newStatus := model.Status(input.Status)

			// Prevent undeleting servers - once deleted, they stay deleted
			if currentServer.Meta.Official != nil &&
				currentServer.Meta.Official.Status == model.StatusDeleted &&
				newStatus != model.StatusDeleted {
				return nil, huma.Error400BadRequest("Cannot change status of deleted server. Deleted servers cannot be undeleted.")
			}

			// For now, only allow status changes for admins
			// Future: Implement logic to allow server authors to change active <-> deprecated
			// but only admins can set to deleted
		}

		// Update the server using the service
		var statusPtr *string
		if input.Status != "" {
			statusPtr = &input.Status
		}
		updatedServer, err := registry.UpdateServer(ctx, serverName, version, &input.Body, statusPtr)
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
