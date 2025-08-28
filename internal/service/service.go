package service

import apiv1 "github.com/modelcontextprotocol/registry/pkg/api/v1"

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// List retrieves servers with unified format
	List(cursor string, limit int) ([]apiv1.ServerRecord, string, error)
	// GetByID retrieves a single server by registry metadata ID with unified format
	GetByID(id string) (*apiv1.ServerRecord, error)
	// Publish publishes a server with separated extensions
	Publish(req apiv1.PublishRequest) (*apiv1.ServerRecord, error)
	// EditServer updates an existing server with new details
	EditServer(id string, req apiv1.PublishRequest) (*apiv1.ServerRecord, error)
}
