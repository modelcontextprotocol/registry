package v0

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/auth"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// UpdateServerStatusInput represents the input for updating server status
type UpdateServerStatusInput struct {
	Authorization string                      `header:"Authorization" doc:"Registry JWT token with edit or admin permissions" required:"true"`
	ServerID      string                      `path:"server_id" doc:"Server ID (UUID)" format:"uuid"`
	Body          UpdateServerStatusRequestBody `body:""`
}

// UpdateServerStatusRequestBody represents the request body for status updates
type UpdateServerStatusRequestBody struct {
	Status string `json:"status" doc:"New status for the server" enum:"active,deprecated,deleted" example:"deprecated"`
}

// RegisterStatusEndpoints registers the status management endpoints
func RegisterStatusEndpoints(api huma.API, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	// Update server status endpoint
	huma.Register(api, huma.Operation{
		OperationID: "update-server-status",
		Method:      http.MethodPatch,
		Path:        "/v0/servers/{server_id}/status",
		Summary:     "Update MCP server status",
		Description: "Update the status of an MCP server. Publishers can manage their own servers, admins can manage any server. Valid statuses: active, deprecated, deleted.",
		Tags:        []string{"servers", "admin"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *UpdateServerStatusInput) (*Response[apiv0.ServerResponse], error) {
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

		// Get current server to check permissions
		currentServer, err := registry.GetByServerID(input.ServerID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get current server", err)
		}

		// Check if user has permission to modify this server's status
		// Publishers can modify their own servers, or anyone with admin permissions can modify any server
		hasEditPermission := jwtManager.HasPermission(currentServer.Server.Name, auth.PermissionActionEdit, claims.Permissions)
		hasAdminPermission := jwtManager.HasPermission("*", auth.PermissionActionEdit, claims.Permissions)

		if !hasEditPermission && !hasAdminPermission {
			return nil, huma.Error403Forbidden("You do not have permission to modify this server's status")
		}

		// Validate status value
		validStatuses := []string{"active", "deprecated", "deleted"}
		isValidStatus := false
		for _, validStatus := range validStatuses {
			if input.Body.Status == validStatus {
				isValidStatus = true
				break
			}
		}
		if !isValidStatus {
			return nil, huma.Error400BadRequest("Invalid status. Valid values are: active, deprecated, deleted")
		}

		// Prevent undeleting servers - once deleted, they stay deleted (preserve original design)
		if currentServer.GetStatus() == model.StatusDeleted && input.Body.Status != "deleted" {
			return nil, huma.Error400BadRequest("Cannot change status of deleted server. Deleted servers cannot be restored.")
		}

		// Update server status
		updatedServer, err := registry.UpdateServerStatus(input.ServerID, input.Body.Status)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error400BadRequest("Failed to update server status", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *updatedServer,
		}, nil
	})
}