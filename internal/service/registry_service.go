package service

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db database.Database
}

// NewRegistryServiceWithDB creates a new registry service with the provided database
//
//nolint:ireturn // Factory function intentionally returns interface for dependency injection
func NewRegistryServiceWithDB(db database.Database) RegistryService {
	return &registryServiceImpl{
		db: db,
	}
}

// GetAll returns all registry entries
func (s *registryServiceImpl) GetAll() ([]model.Server, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's List method with no filters to get all entries
	entries, _, err := s.db.List(ctx, nil, "", 30)
	if err != nil {
		return nil, err
	}

	// Convert from []*model.Server to []model.Server
	result := make([]model.Server, len(entries))
	for i, entry := range entries {
		result[i] = *entry
	}

	return result, nil
}

// List returns registry entries with cursor-based pagination
func (s *registryServiceImpl) List(cursor string, limit int) ([]model.Server, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's List method with pagination
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
func (s *registryServiceImpl) GetByID(id string) (*model.ServerDetail, error) {
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

// getVersionsByName retrieves all versions of a server by name
func (s *registryServiceImpl) getVersionsByName(ctx context.Context, name string) ([]*model.ServerDetail, error) {
	// Use the database's List method to find all servers with the same name
	entries, _, err := s.db.List(ctx, map[string]any{"name": name}, "", 1000) // Large limit to get all versions
	if err != nil {
		return nil, err
	}

	var serverDetails []*model.ServerDetail
	for _, entry := range entries {
		serverDetail, err := s.db.GetByID(ctx, entry.ID)
		if err != nil {
			continue // Skip if we can't get the detail
		}
		serverDetails = append(serverDetails, serverDetail)
	}

	return serverDetails, nil
}

// determineIsLatest determines if a new version should be marked as latest based on the versioning strategy
func (s *registryServiceImpl) determineIsLatest(newVersion string, newTimestamp time.Time, existingVersions []*model.ServerDetail) bool {
	if len(existingVersions) == 0 {
		return true
	}

	for _, existing := range existingVersions {
		existingTime, _ := time.Parse(time.RFC3339, existing.VersionDetail.ReleaseDate)
		comparison := CompareVersions(newVersion, existing.VersionDetail.Version, newTimestamp, existingTime)
		if comparison < 0 {
			// New version is lower than existing version
			return false
		}
	}

	return true
}

// markOtherVersionsAsNotLatest marks all other versions of the same server as not latest
func (s *registryServiceImpl) markOtherVersionsAsNotLatest(ctx context.Context, serverName string) error {
	existingVersions, err := s.getVersionsByName(ctx, serverName)
	if err != nil {
		return err
	}

	for _, existing := range existingVersions {
		if existing.VersionDetail.IsLatest {
			existing.VersionDetail.IsLatest = false
			// We need to update this in the database, but the current interface doesn't support updates
			// For now, this will be handled in the database layer during Publish
		}
	}

	return nil
}

// Publish adds a new server detail to the registry
func (s *registryServiceImpl) Publish(serverDetail *model.ServerDetail) error {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if serverDetail == nil {
		return database.ErrInvalidInput
	}

	// Get existing versions of this server to determine if this should be latest
	existingVersions, err := s.getVersionsByName(ctx, serverDetail.Name)
	if err != nil && err != database.ErrNotFound {
		return err
	}

	// Parse the current time for timestamp-based comparisons
	currentTime := time.Now()
	if serverDetail.VersionDetail.ReleaseDate == "" {
		serverDetail.VersionDetail.ReleaseDate = currentTime.Format(time.RFC3339)
	}

	// Determine if this version should be marked as latest
	isLatest := s.determineIsLatest(serverDetail.VersionDetail.Version, currentTime, existingVersions)
	serverDetail.VersionDetail.IsLatest = isLatest

	err = s.db.Publish(ctx, serverDetail)
	if err != nil {
		return err
	}

	return nil
}
