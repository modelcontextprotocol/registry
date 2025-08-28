package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxServerVersionsPerServer = 10000

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db database.Database
}

// NewRegistryServiceWithDB creates a new registry service with the provided database
func NewRegistryServiceWithDB(db database.Database) RegistryService {
	return &registryServiceImpl{
		db: db,
	}
}

// List returns registry entries with cursor-based pagination in extension wrapper format
func (s *registryServiceImpl) List(cursor string, limit int) ([]apiv0.ServerRecord, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's List method with pagination
	serverRecords, nextCursor, err := s.db.List(ctx, nil, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	// Return ServerRecords directly (they're now the same as ServerResponse)
	result := make([]apiv0.ServerRecord, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server by its registry metadata ID in extension wrapper format
func (s *registryServiceImpl) GetByID(id string) (*apiv0.ServerRecord, error) {
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
func (s *registryServiceImpl) Publish(req apiv0.PublishRequest) (*apiv0.ServerRecord, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req); err != nil {
		return nil, err
	}

	publishTime := time.Now()

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, req.Server); err != nil {
		return nil, err
	}

	existingServerVersions, _, err := s.db.List(ctx, map[string]any{"name": req.Server.Name}, "", maxServerVersionsPerServer)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	// Check we haven't exceeded the maximum versions allowed for a server
	if len(existingServerVersions) >= maxServerVersionsPerServer {
		return nil, database.ErrMaxServersReached
	}

	// Check this isn't a duplicate version
	for _, server := range existingServerVersions {
		existingVersion := server.Server.VersionDetail.Version
		if existingVersion == req.Server.VersionDetail.Version {
			return nil, database.ErrInvalidVersion
		}
	}

	// Determine if this version should be marked as latest
	existingLatest := s.getCurrentLatestVersion(existingServerVersions)
	isNewLatest := true
	if existingLatest != nil {
		isNewLatest = CompareVersions(
			req.Server.VersionDetail.Version,
			existingLatest.Server.VersionDetail.Version,
			publishTime,
			existingLatest.XIOModelContextProtocolRegistry.PublishedAt,
		) > 0
	}

	// Create registry metadata with service-determined values
	registryMetadata := apiv0.RegistryExtensions{
		ID:          uuid.New().String(),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
		ReleaseDate: publishTime.Format(time.RFC3339),
	}

	// Use publisher extensions directly from request
	publisherExtensions := req.XPublisher
	if publisherExtensions == nil {
		publisherExtensions = make(map[string]interface{})
	}

	// Publish to database with the registry metadata
	serverRecord, err := s.db.Publish(ctx, req.Server, publisherExtensions, registryMetadata)
	if err != nil {
		return nil, err
	}

	// Mark previous latest as no longer latest
	if isNewLatest && existingLatest != nil {
		if err := s.db.UpdateLatestFlag(ctx, existingLatest.XIOModelContextProtocolRegistry.ID, false); err != nil {
			return nil, err
		}
	}

	// Return ServerRecord directly (it's now the same as ServerResponse)
	return serverRecord, nil
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, serverDetail model.ServerJSON) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := map[string]any{
			"remote_url": remote.URL,
		}

		conflictingServers, _, err := s.db.List(ctx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		// Check if any conflicting server has a different name
		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Server.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Server.Name)
			}
		}
	}

	return nil
}

// getCurrentLatestVersion finds the current latest version from existing server versions
func (s *registryServiceImpl) getCurrentLatestVersion(existingServerVersions []*apiv0.ServerRecord) *apiv0.ServerRecord {
	for _, server := range existingServerVersions {
		if server.XIOModelContextProtocolRegistry.IsLatest {
			return server
		}
	}
	return nil
}

// EditServer updates an existing server with new details (admin operation)
func (s *registryServiceImpl) EditServer(id string, req apiv0.PublishRequest) (*apiv0.ServerRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req); err != nil {
		return nil, err
	}

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, req.Server); err != nil {
		return nil, err
	}

	// Use publisher extensions directly from request
	publisherExtensions := req.XPublisher
	if publisherExtensions == nil {
		publisherExtensions = make(map[string]interface{})
	}

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, id, req.Server, publisherExtensions)
	if err != nil {
		return nil, err
	}

	// Return ServerRecord directly (it's now the same as ServerResponse)
	return serverRecord, nil
}
