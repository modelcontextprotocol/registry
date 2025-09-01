package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// fakeRegistryService implements RegistryService interface with an in-memory database
type fakeRegistryService struct {
	db *database.MemoryDB
}

// NewFakeRegistryService creates a new fake registry service with pre-populated data
func NewFakeRegistryService() RegistryService {
	// Sample registry entries with updated model structure using ServerJSON
	serverDetails := []*apiv0.ServerJSON{
		{
			Name:        "bluegreen/mcp-server",
			Description: "A dummy MCP registry for testing",
			Repository: model.Repository{
				URL:    "https://github.com/example/mcp-1",
				Source: "github",
				ID:     "example/mcp-1",
			},
			VersionDetail: model.VersionDetail{
				Version: "1.0.0",
			},
		},
		{
			Name:        "orangepurple/mcp-server",
			Description: "Another dummy MCP registry for testing",
			Repository: model.Repository{
				URL:    "https://github.com/example/mcp-2",
				Source: "github",
				ID:     "example/mcp-2",
			},
			VersionDetail: model.VersionDetail{
				Version: "0.9.0",
			},
		},
		{
			Name:        "greenyellow/mcp-server",
			Description: "Yet another dummy MCP registry for testing",
			Repository: model.Repository{
				URL:    "https://github.com/example/mcp-3",
				Source: "github",
				ID:     "example/mcp-3",
			},
			VersionDetail: model.VersionDetail{
				Version: "0.9.5",
			},
		},
	}

	// Create a new in-memory database using registry metadata IDs
	serverDetailMap := make(map[string]*apiv0.ServerJSON)
	for _, entry := range serverDetails {
		registryID := uuid.New().String() // Generate registry metadata ID
		serverDetailMap[registryID] = entry
	}
	memDB := database.NewMemoryDB(serverDetailMap)
	return &fakeRegistryService{
		db: memDB,
	}
}

// List retrieves servers with extension wrapper format
func (s *fakeRegistryService) List(cursor string, limit int) ([]apiv0.ServerJSON, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's List method to get ServerRecord entries
	serverRecords, nextCursor, err := s.db.List(ctx, nil, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	// Return ServerRecords directly (they're now the same as ServerResponse)
	result := make([]apiv0.ServerJSON, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server by its registry metadata ID in extension wrapper format
func (s *fakeRegistryService) GetByID(id string) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's GetByID method to retrieve the server record
	serverRecord, err := s.db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Return ServerRecord directly (it's now the same as ServerResponse)
	return serverRecord, nil
}

// Publish publishes a server with separated extensions
func (s *fakeRegistryService) Publish(req apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req); err != nil {
		return nil, err
	}

	// Extract publisher extensions from _meta.publisher
	publisherExtensions := make(map[string]interface{})
	if req.Meta != nil && req.Meta.Publisher != nil {
		publisherExtensions = req.Meta.Publisher
	}

	// Create registry metadata for fake service (always marks as latest)
	now := time.Now()
	registryMetadata := apiv0.RegistryExtensions{
		ID:          uuid.New().String(),
		PublishedAt: now,
		UpdatedAt:   now,
		IsLatest:    true,
		ReleaseDate: now.Format(time.RFC3339),
	}

	// Publish to database
	serverRecord, err := s.db.Publish(ctx, req, publisherExtensions, registryMetadata)
	if err != nil {
		return nil, err
	}

	// Return ServerRecord directly (it's now the same as ServerResponse)
	return serverRecord, nil
}

// EditServer updates an existing server with new details (admin operation)
func (s *fakeRegistryService) EditServer(id string, req apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req); err != nil {
		return nil, err
	}

	// Extract publisher extensions from _meta.publisher
	publisherExtensions := make(map[string]interface{})
	if req.Meta != nil && req.Meta.Publisher != nil {
		publisherExtensions = req.Meta.Publisher
	}

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, id, req, publisherExtensions)
	if err != nil {
		return nil, err
	}

	// Return ServerRecord directly (it's now the same as ServerResponse)
	return serverRecord, nil
}

// Close closes the in-memory database connection
func (s *fakeRegistryService) Close() error {
	return s.db.Close()
}
