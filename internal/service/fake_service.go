package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// fakeRegistryService implements RegistryService interface with an in-memory database
type fakeRegistryService struct {
	db *database.MemoryDB
}

// NewFakeRegistryService creates a new fake registry service with pre-populated data
//
//nolint:ireturn // Factory function intentionally returns interface for dependency injection
func NewFakeRegistryService() RegistryService {
	// Sample registry entries with updated model structure
	registries := []*model.Server{
		{
			ID:          uuid.New().String(),
			Name:        "bluegreen/mcp-server",
			Description: "A dummy MCP registry for testing",
			Repository: model.Repository{
				URL:    "https://github.com/example/mcp-1",
				Source: "github",
				ID:     "example/mcp-1",
			},
			VersionDetail: model.VersionDetail{
				Version:     "1.0.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    true,
			},
		},
		{
			ID:          uuid.New().String(),
			Name:        "orangepurple/mcp-server",
			Description: "Another dummy MCP registry for testing",
			Repository: model.Repository{
				URL:    "https://github.com/example/mcp-2",
				Source: "github",
				ID:     "example/mcp-2",
			},
			VersionDetail: model.VersionDetail{
				Version:     "0.9.0",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    false,
			},
		},
		{
			ID:          uuid.New().String(),
			Name:        "greenyellow/mcp-server",
			Description: "Yet another dummy MCP registry for testing",
			Repository: model.Repository{
				URL:    "https://github.com/example/mcp-3",
				Source: "github",
				ID:     "example/mcp-3",
			},
			VersionDetail: model.VersionDetail{
				Version:     "0.9.5",
				ReleaseDate: time.Now().Format(time.RFC3339),
				IsLatest:    false,
			},
		},
	}

	// Create a new in-memory database
	registryMap := make(map[string]*model.Server)
	for _, entry := range registries {
		registryMap[entry.ID] = entry
	}
	memDB := database.NewMemoryDB(registryMap)
	return &fakeRegistryService{
		db: memDB,
	}
}

// List retrieves MCPRegistry entries with optional filtering and pagination
func (s *fakeRegistryService) List(cursor string, limit int) ([]model.Server, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's List method with no filters to get all entries
	entries, nextCursor, err := s.db.List(ctx, nil, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	// Convert from []*model.Server to []model.Server
	result := make([]model.Server, len(entries))
	for i, entry := range entries {
		result[i] = *entry
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server detail by its ID
func (s *fakeRegistryService) GetByID(id string) (*model.ServerDetail, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's GetByID method to retrieve the server detail
	serverDetail, err := s.db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	return serverDetail, nil
}

// Publish adds a new server detail to the in-memory database
func (s *fakeRegistryService) Publish(serverDetail *model.ServerDetail) error {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's Publish method to add the server detail
	return s.db.Publish(ctx, serverDetail)
}

// Close closes the in-memory database connection
func (s *fakeRegistryService) Close() error {
	return s.db.Close()
}
