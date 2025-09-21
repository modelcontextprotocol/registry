package service

import (
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// Retrieve all servers with optional filtering
	List(filter *database.ServerFilter, cursor string, limit int) ([]apiv0.ServerResponse, string, error)
	// Retrieve a single server by registry metadata version ID
	GetByVersionID(versionID string) (*apiv0.ServerResponse, error)
	// Retrieve latest version of a server by server ID
	GetByServerID(serverID string) (*apiv0.ServerResponse, error)
	// Retrieve specific version of a server by server ID and version
	GetByServerIDAndVersion(serverID string, version string) (*apiv0.ServerResponse, error)
	// Retrieve all versions of a server by server ID
	GetAllVersionsByServerID(serverID string) ([]apiv0.ServerResponse, error)
	// Publish a server (input is still ServerJSON, output is ServerResponse)
	Publish(req apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// Update an existing server (input is still ServerJSON, output is ServerResponse)
	EditServer(id string, req apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// Update server status in registry metadata
	UpdateServerStatus(serverID string, status string) (*apiv0.ServerResponse, error)
}
