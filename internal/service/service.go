package service

import apiv1 "github.com/modelcontextprotocol/registry/pkg/api/v1"

// RegistryService defines the interface for registry operations with extension wrapper architecture
type RegistryService interface {
	// List retrieves servers with extension wrapper format
	List(cursor string, limit int) ([]apiv1.ServerResponse, string, error)
	// GetByID retrieves a single server by registry metadata ID with extension wrapper format
	GetByID(id string) (*apiv1.ServerResponse, error)
	// Publish publishes a server with separated extensions
	Publish(req apiv1.PublishRequest) (*apiv1.ServerResponse, error)
	// EditServer updates an existing server with new details
	EditServer(id string, req apiv1.PublishRequest) (*apiv1.ServerResponse, error)
}
