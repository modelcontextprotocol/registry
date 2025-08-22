package v0

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/service"
)

// Metadata contains pagination metadata
type Metadata struct {
	NextCursor string `json:"next_cursor,omitempty"`
	Count      int    `json:"count,omitempty"`
	Total      int    `json:"total,omitempty"`
}

// ListServersInput represents the input for listing servers
type ListServersInput struct {
	Cursor string `query:"cursor" doc:"Pagination cursor (UUID)" format:"uuid" required:"false"`
	Limit  int    `query:"limit" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100"`
}

// ListServersBody represents the paginated server list response body
type ListServersBody struct {
	Servers  []model.ServerResponse `json:"servers" doc:"List of MCP servers with extensions"`
	Metadata *Metadata               `json:"metadata,omitempty" doc:"Pagination metadata"`
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
	}, func(_ context.Context, input *ListServersInput) (*Response[ListServersBody], error) {
		// Validate cursor if provided
		if input.Cursor != "" {
			_, err := uuid.Parse(input.Cursor)
			if err != nil {
				return nil, huma.Error400BadRequest("Invalid cursor parameter")
			}
		}

		// Get paginated results
		servers, nextCursor, err := registry.List(input.Cursor, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get registry list", err)
		}

		// Build response body
		body := ListServersBody{
			Servers: servers,
		}

		// Add metadata if there's a next cursor
		if nextCursor != "" {
			body.Metadata = &Metadata{
				NextCursor: nextCursor,
				Count:      len(servers),
			}
		}

		return &Response[ListServersBody]{
			Body: body,
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
	}, func(_ context.Context, input *ServerDetailInput) (*Response[model.ServerResponse], error) {
		// Get the server details from the registry service
		serverDetail, err := registry.GetByID(input.ID)
		if err != nil {
			if err.Error() == "record not found" {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server details", err)
		}

		return &Response[model.ServerResponse]{
			Body: *serverDetail,
		}, nil
	})
}
