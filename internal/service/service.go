package service

import (
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// Retrieve all servers with optional filtering
	List(filter *database.ServerFilter, cursor string, limit int) ([]apiv0.ServerJSON, string, error)
	// Retrieve a single server by registry metadata version ID
	GetByVersionID(versionID string) (*apiv0.ServerJSON, error)
	// Retrieve latest version of a server by server ID
	GetByServerID(serverID string) (*apiv0.ServerJSON, error)
	// Retrieve specific version of a server by server ID and version
	GetByServerIDAndVersion(serverID string, version string) (*apiv0.ServerJSON, error)
	// Retrieve all versions of a server by server ID
	GetAllVersionsByServerID(serverID string) ([]apiv0.ServerJSON, error)
	// Publish a server
	Publish(req apiv0.ServerJSON) (*apiv0.ServerJSON, error)
	// Update an existing server
	EditServer(id string, req apiv0.ServerJSON) (*apiv0.ServerJSON, error)
}
