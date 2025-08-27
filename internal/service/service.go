package service

import "github.com/modelcontextprotocol/registry/internal/model"

// RegistryService defines the interface for registry operations with extension wrapper architecture
type RegistryService interface {
	// List retrieves servers with extension wrapper format
	List(cursor string, limit int) ([]model.ServerResponse, string, error)
	// GetByID retrieves a single server by registry metadata ID with extension wrapper format
	GetByID(id string) (*model.ServerResponse, error)
	// Publish publishes a server with separated extensions
	Publish(req model.PublishRequest) (*model.ServerResponse, error)
	// EditServer updates an existing server with new details
	EditServer(id string, req model.PublishRequest) (*model.ServerResponse, error)
}
