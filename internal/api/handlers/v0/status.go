package v0

import (
	"context"
	"errors"
	"fmt"
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

// UpdateServerStatusBody represents the request body for updating server status
type UpdateServerStatusBody struct {
	Status        string  `json:"status" required:"true" enum:"active,deprecated,deleted" doc:"New server lifecycle status"`
	StatusMessage *string `json:"statusMessage,omitempty" maxLength:"500" doc:"Optional message explaining the status change (e.g., reason for deprecation)"`
}

// UpdateServerStatusInput represents the input for updating server status
type UpdateServerStatusInput struct {
	Authorization string                 `header:"Authorization" doc:"Registry JWT token with publish or edit permissions" required:"true"`
	ServerName    string                 `path:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
	Version       string                 `path:"version" doc:"URL-encoded version to update" example:"1.0.0"`
	Body          UpdateServerStatusBody `body:""`
}

// validateStatusTransition validates if the status transition is allowed
// Returns nil if status or message differs from current (something to update)
// Returns error if nothing changes (no-op) or if status transition is invalid
func validateStatusTransition(currentServer *apiv0.ServerResponse, newStatus model.Status, body UpdateServerStatusBody) error {
	// Validate newStatus is a valid value
	validStatuses := map[model.Status]bool{
		model.StatusActive:     true,
		model.StatusDeprecated: true,
		model.StatusDeleted:    true,
	}
	if !validStatuses[newStatus] {
		return huma.Error400BadRequest(fmt.Sprintf("Invalid status: %s. Must be one of: active, deprecated, deleted", newStatus))
	}

	// Reject status_message when setting status to active
	if newStatus == model.StatusActive && body.StatusMessage != nil {
		return huma.Error400BadRequest("status_message cannot be provided when setting status to active")
	}

	if currentServer.Meta.Official == nil {
		return nil
	}

	currentStatus := currentServer.Meta.Official.Status
	currentMessage := currentServer.Meta.Official.StatusMessage
	newMessage := body.StatusMessage

	statusChanges := currentStatus != newStatus
	messageChanges := (currentMessage == nil) != (newMessage == nil) ||
		(currentMessage != nil && newMessage != nil && *currentMessage != *newMessage)

	// Valid if either status or message changes
	if statusChanges || messageChanges {
		return nil
	}

	return huma.Error400BadRequest("No changes to apply: status and message are already set to the provided values")
}

// RegisterStatusEndpoints registers the status update endpoint with a custom path prefix
func RegisterStatusEndpoints(api huma.API, pathPrefix string, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	// Update server status endpoint
	huma.Register(api, huma.Operation{
		OperationID: "update-server-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPatch,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}/status",
		Summary:     "Update MCP server status",
		Description: "Update the status metadata of a specific version of an MCP server. Requires publish or edit permission for the server. This endpoint allows changing status and status message without requiring the full server configuration.",
		Tags:        []string{"servers"},
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

		newStatus := model.Status(input.Body.Status)

		// Get the server and verify it exists
		// Include deleted servers since we need to be able to restore them
		currentServer, err := registry.GetServerByNameAndVersion(ctx, serverName, version, true)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server version not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server", err)
		}

		// Verify publish or edit permissions for this server
		hasPublish := jwtManager.HasPermission(currentServer.Server.Name, auth.PermissionActionPublish, claims.Permissions)
		hasEdit := jwtManager.HasPermission(currentServer.Server.Name, auth.PermissionActionEdit, claims.Permissions)
		if !hasPublish && !hasEdit {
			return nil, huma.Error403Forbidden("You do not have publish or edit permissions for this server")
		}

		// Validate status transition is allowed
		if err := validateStatusTransition(currentServer, newStatus, input.Body); err != nil {
			return nil, err
		}

		// Build status change request
		statusChange := buildStatusChangeRequestFromBody(input.Body)

		// Update the server status using the service
		updatedServer, err := registry.UpdateServerStatus(ctx, serverName, version, statusChange)
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

// buildStatusChangeRequestFromBody constructs a StatusChangeRequest from the request body
func buildStatusChangeRequestFromBody(body UpdateServerStatusBody) *service.StatusChangeRequest {
	var statusMessage *string

	newStatus := model.Status(body.Status)

	// When transitioning to active status, clear status_message
	if newStatus != model.StatusActive {
		statusMessage = body.StatusMessage
	}

	return &service.StatusChangeRequest{
		NewStatus:     newStatus,
		StatusMessage: statusMessage,
	}
}

// UpdateAllVersionsStatusInput represents the input for updating all versions' status
type UpdateAllVersionsStatusInput struct {
	Authorization string                 `header:"Authorization" doc:"Registry JWT token with publish or edit permissions" required:"true"`
	ServerName    string                 `path:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
	Body          UpdateServerStatusBody `body:""`
}

// UpdateAllVersionsStatusResponse represents the response for updating all versions' status
type UpdateAllVersionsStatusResponse struct {
	UpdatedCount int                    `json:"updatedCount" doc:"Number of versions updated"`
	Servers      []apiv0.ServerResponse `json:"servers" doc:"List of all updated server versions"`
}

// RegisterAllVersionsStatusEndpoints registers the all-versions status update endpoint
func RegisterAllVersionsStatusEndpoints(api huma.API, pathPrefix string, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	// Update all versions status endpoint
	huma.Register(api, huma.Operation{
		OperationID: "update-server-all-versions-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPatch,
		Path:        pathPrefix + "/servers/{serverName}/status",
		Summary:     "Update status for all versions of an MCP server",
		Description: "Update the status metadata of all versions of an MCP server in a single transaction. Requires publish or edit permission for the server. Either all versions are updated or none on failure.",
		Tags:        []string{"servers"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *UpdateAllVersionsStatusInput) (*Response[UpdateAllVersionsStatusResponse], error) {
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

		// Get any version to verify server exists and check permissions
		// Include deleted servers since we need to be able to restore them
		currentServer, err := registry.GetServerByName(ctx, serverName, true)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server", err)
		}

		// Verify publish or edit permissions for this server
		hasPublish := jwtManager.HasPermission(currentServer.Server.Name, auth.PermissionActionPublish, claims.Permissions)
		hasEdit := jwtManager.HasPermission(currentServer.Server.Name, auth.PermissionActionEdit, claims.Permissions)
		if !hasPublish && !hasEdit {
			return nil, huma.Error403Forbidden("You do not have publish or edit permissions for this server")
		}

		newStatus := model.Status(input.Body.Status)

		// Fetch all versions to validate the bulk status transition
		allVersions, err := registry.GetAllVersionsByServerName(ctx, serverName, true)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get server versions", err)
		}

		// Validate bulk status transition - reject if no changes would occur
		if err := validateBulkStatusTransition(allVersions, newStatus, input.Body); err != nil {
			return nil, err
		}

		// Build status change request
		statusChange := buildStatusChangeRequestFromBody(input.Body)

		// Update all versions' status using the service
		updatedServers, err := registry.UpdateAllVersionsStatus(ctx, serverName, statusChange)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error400BadRequest("Failed to update server status", err)
		}

		// Convert to response format
		servers := make([]apiv0.ServerResponse, len(updatedServers))
		for i, s := range updatedServers {
			servers[i] = *s
		}

		return &Response[UpdateAllVersionsStatusResponse]{
			Body: UpdateAllVersionsStatusResponse{
				UpdatedCount: len(servers),
				Servers:      servers,
			},
		}, nil
	})
}

// validateBulkStatusTransition validates if a bulk status transition would result in any changes
// Returns an error if no changes would occur (all versions already have target status and message)
func validateBulkStatusTransition(versions []*apiv0.ServerResponse, newStatus model.Status, body UpdateServerStatusBody) error {
	if len(versions) == 0 {
		return nil
	}

	// Check if any version would have changes applied
	for _, version := range versions {
		err := validateStatusTransition(version, newStatus, body)
		if err == nil {
			// This version has changes, so the bulk operation is valid
			return nil
		}
	}

	// No versions would have any changes
	return huma.Error400BadRequest("No changes to apply: all versions already have the requested status and message")
}
