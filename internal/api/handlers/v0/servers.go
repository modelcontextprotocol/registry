package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/service"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const errRecordNotFound = "record not found"

// ListServersInput represents the input for listing servers
type ListServersInput struct {
	Cursor       string `query:"cursor" doc:"Pagination cursor" required:"false" example:"server-cursor-123"`
	Limit        int    `query:"limit" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" doc:"Filter servers updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" doc:"Search servers by name (substring match)" required:"false" example:"filesystem"`
	Version      string `query:"version" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// ServerDetailInput represents the input for getting server details
type ServerDetailInput struct {
	ServerName string `path:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
}

// ServerVersionDetailInput represents the input for getting a specific version
type ServerVersionDetailInput struct {
	ServerName string `path:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
	Version    string `path:"version" doc:"URL-encoded server version" example:"1.0.0"`
}

// ServerVersionsInput represents the input for listing all versions of a server
type ServerVersionsInput struct {
	ServerName string `path:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
}

// RegisterServersEndpoints registers all server-related endpoints with a custom path prefix
func RegisterServersEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	// List servers endpoint
	huma.Register(api, huma.Operation{
		OperationID: "list-servers" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers",
		Summary:     "List MCP servers",
		Description: "Get a paginated list of MCP servers from the registry",
		Tags:        []string{"servers"},
	}, func(ctx context.Context, input *ListServersInput) (*Response[apiv0.ServerListResponse], error) {
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
		servers, nextCursor, err := registry.ListServers(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get registry list", err)
		}

		// Convert []*ServerResponse to []ServerResponse
		serverValues := make([]apiv0.ServerResponse, len(servers))
		for i, server := range servers {
			serverValues[i] = *server
		}

		return &Response[apiv0.ServerListResponse]{
			Body: apiv0.ServerListResponse{
				Servers: serverValues,
				Metadata: apiv0.Metadata{
					NextCursor: nextCursor,
					Count:      len(servers),
				},
			},
		}, nil
	})

	// Get specific server version endpoint (supports "latest" as special version)
	huma.Register(api, huma.Operation{
		OperationID: "get-server-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}",
		Summary:     "Get specific MCP server version",
		Description: "Get detailed information about a specific version of an MCP server. Use the special version 'latest' to get the latest version.",
		Tags:        []string{"servers"},
	}, func(ctx context.Context, input *ServerVersionDetailInput) (*Response[apiv0.ServerResponse], error) {
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

		var serverResponse *apiv0.ServerResponse
		// Handle "latest" as a special version
		if version == "latest" {
			serverResponse, err = registry.GetServerByName(ctx, serverName)
		} else {
			serverResponse, err = registry.GetServerByNameAndVersion(ctx, serverName, version)
		}

		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server details", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *serverResponse,
		}, nil
	})

	// Get server versions endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers/{serverName}/versions",
		Summary:     "Get all versions of an MCP server",
		Description: "Get all available versions for a specific MCP server",
		Tags:        []string{"servers"},
	}, func(ctx context.Context, input *ServerVersionsInput) (*Response[apiv0.ServerListResponse], error) {
		// URL-decode the server name
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		// Get all versions for this server
		servers, err := registry.GetAllVersionsByServerName(ctx, serverName)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server versions", err)
		}

		// Convert []*ServerResponse to []ServerResponse
		serverValues := make([]apiv0.ServerResponse, len(servers))
		for i, server := range servers {
			serverValues[i] = *server
		}

		return &Response[apiv0.ServerListResponse]{
			Body: apiv0.ServerListResponse{
				Servers: serverValues,
				Metadata: apiv0.Metadata{
					Count: len(servers),
				},
			},
		}, nil
	})
}
