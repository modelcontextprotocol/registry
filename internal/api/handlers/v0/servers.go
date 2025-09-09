package v0

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ListServersInput represents the input for listing servers
type ListServersInput struct {
	Cursor       string `query:"cursor" doc:"Pagination cursor (UUID)" format:"uuid" required:"false" example:"550e8400-e29b-41d4-a716-446655440000"`
	Limit        int    `query:"limit" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" doc:"Filter servers updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" doc:"Search servers by name (substring match)" required:"false" example:"filesystem"`
	Version      string `query:"version" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// ServerDetailInput represents the input for getting server details
type ServerDetailInput struct {
	ID string `path:"id" doc:"Server ID (UUID)" format:"uuid"`
}

// RegisterServersEndpoints registers all server-related endpoints
func RegisterServersEndpoints(api huma.API, registry service.RegistryService) {
	// List servers endpoint
	huma.Register(api, huma.Operation{
		OperationID: "list-servers",
		Method:      http.MethodGet,
		Path:        "/v0/servers",
		Summary:     "List MCP servers",
		Description: "Get a paginated list of MCP servers from the registry",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ListServersInput) (*Response[apiv0.ServerListResponse], error) {
		// Validate cursor if provided
		if input.Cursor != "" {
			_, err := uuid.Parse(input.Cursor)
			if err != nil {
				return nil, huma.Error400BadRequest("Invalid cursor parameter")
			}
		}

		// Build filter from input parameters
		filter := &database.ServerFilter{}

		// Parse updated_since parameter
		if input.UpdatedSince != "" {
			// Parse RFC3339 format
			if updatedTime, err := time.Parse(time.RFC3339, input.UpdatedSince); err == nil {
				filter.UpdatedSince = &updatedTime
			} else {
				return nil, huma.Error400BadRequest("Invalid updated_since format: expected RFC3339 timestamp (e.g., 2025-08-07T13:15:04.280Z)")
			}
		}

		// Handle search parameter
		if input.Search != "" {
			filter.SubstringName = &input.Search
		}

		// Handle version parameter
		if input.Version != "" {
			if input.Version == "latest" {
				// Special case: filter for latest versions
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				// Future: exact version matching
				filter.Version = &input.Version
			}
		}

		// Get paginated results with filtering
		servers, nextCursor, err := registry.List(filter, input.Cursor, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get registry list", err)
		}

		return &Response[apiv0.ServerListResponse]{
			Body: apiv0.ServerListResponse{
				Servers: servers,
				Metadata: apiv0.Metadata{
					NextCursor: nextCursor,
					Count:      len(servers),
				},
			},
		}, nil
	})

	// Get server details endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server",
		Method:      http.MethodGet,
		Path:        "/v0/servers/{id}",
		Summary:     "Get MCP server details",
		Description: "Get detailed information about a specific MCP server",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ServerDetailInput) (*Response[apiv0.ServerJSON], error) {
		// Get the server details from the registry service
		serverDetail, err := registry.GetByID(input.ID)
		if err != nil {
			if err.Error() == "record not found" {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server details", err)
		}

		return &Response[apiv0.ServerJSON]{
			Body: *serverDetail,
		}, nil
	})
}
