package service

import apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// List retrieves servers with unified format
	List(cursor string, limit int) ([]apiv0.ServerRecord, string, error)
	// GetByID retrieves a single server by registry metadata ID with unified format
	GetByID(id string) (*apiv0.ServerRecord, error)
	// Publish publishes a server with separated extensions
	Publish(req apiv0.PublishRequest) (*apiv0.ServerRecord, error)
	// EditServer updates an existing server with new details
	EditServer(id string, req apiv0.PublishRequest) (*apiv0.ServerRecord, error)
}
