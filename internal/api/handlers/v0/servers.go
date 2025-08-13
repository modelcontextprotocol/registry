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

// ListServersOutput represents the paginated server list response
type ListServersOutput struct {
	Body struct {
		Servers  []model.Server `json:"servers" doc:"List of MCP servers"`
		Metadata *Metadata      `json:"metadata,omitempty" doc:"Pagination metadata"`
	}
}

// ServerDetailInput represents the input for getting server details
type ServerDetailInput struct {
	ID string `path:"id" doc:"Server ID (UUID)" format:"uuid"`
}

// ServerDetailOutput represents the server detail response
type ServerDetailOutput struct {
	Body model.ServerDetail
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
	}, func(_ context.Context, input *ListServersInput) (*ListServersOutput, error) {
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

		resp := &ListServersOutput{}
		resp.Body.Servers = servers
		
		// Add metadata if there's a next cursor
		if nextCursor != "" {
			resp.Body.Metadata = &Metadata{
				NextCursor: nextCursor,
				Count:      len(servers),
			}
		}

		return resp, nil
	})

	// Get server details endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server",
		Method:      http.MethodGet,
		Path:        "/v0/servers/{id}",
		Summary:     "Get MCP server details",
		Description: "Get detailed information about a specific MCP server",
		Tags:        []string{"servers"},
	}, func(_ context.Context, input *ServerDetailInput) (*ServerDetailOutput, error) {
		// Get the server details from the registry service
		serverDetail, err := registry.GetByID(input.ID)
		if err != nil {
			if err.Error() == "record not found" {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server details", err)
		}

		resp := &ServerDetailOutput{}
		resp.Body = *serverDetail
		return resp, nil
	})
}