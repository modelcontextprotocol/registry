package service

import (
	"github.com/modelcontextprotocol/registry/internal/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// Retrieve all servers with optional filtering
	List(filter *database.ServerFilter, cursor string, limit int) ([]apiv0.ServerJSON, string, error)
	// Retrieve a single server by registry metadata ID
	GetByID(id string) (*apiv0.ServerJSON, error)
	// Publish a server
	Publish(req apiv0.ServerJSON) (*apiv0.ServerJSON, error)
	// Update an existing server
	EditServer(id string, req apiv0.ServerJSON) (*apiv0.ServerJSON, error)
}
