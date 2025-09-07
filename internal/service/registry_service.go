package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/config"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/validators"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const maxServerVersionsPerServer = 10000

// registryServiceImpl implements the RegistryService interface using our Database
type registryServiceImpl struct {
	db  database.Database
	cfg *config.Config
}

// NewRegistryService creates a new registry service with the provided database
func NewRegistryService(db database.Database, cfg *config.Config) RegistryService {
	return &registryServiceImpl{
		db:  db,
		cfg: cfg,
	}
}

// List returns registry entries with cursor-based pagination in flattened format
func (s *registryServiceImpl) List(cursor string, limit int) ([]apiv0.ServerJSON, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's ListServers method with pagination
	serverRecords, nextCursor, err := s.db.List(ctx, nil, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	// Return ServerJSONs directly
	result := make([]apiv0.ServerJSON, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server by its registry metadata ID in flattened format
func (s *registryServiceImpl) GetByID(id string) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecord, err := s.db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}

// Publish publishes a server with flattened _meta extensions
func (s *registryServiceImpl) Publish(req apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	publishTime := time.Now()
	serverJSON := req

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, serverJSON); err != nil {
		return nil, err
	}

	filter := &database.ServerFilter{Name: &serverJSON.Name}
	existingServerVersions, _, err := s.db.List(ctx, filter, "", maxServerVersionsPerServer)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	// Check we haven't exceeded the maximum versions allowed for a server
	if len(existingServerVersions) >= maxServerVersionsPerServer {
		return nil, database.ErrMaxServersReached
	}

	// Check this isn't a duplicate version
	for _, server := range existingServerVersions {
		existingVersion := server.VersionDetail.Version
		if existingVersion == serverJSON.VersionDetail.Version {
			return nil, database.ErrInvalidVersion
		}
	}

	// Determine if this version should be marked as latest
	existingLatest := s.getCurrentLatestVersion(existingServerVersions)
	isNewLatest := true
	if existingLatest != nil {
		var existingPublishedAt time.Time
		if existingLatest.Meta != nil && existingLatest.Meta.Official != nil {
			existingPublishedAt = existingLatest.Meta.Official.PublishedAt
		}
		isNewLatest = CompareVersions(
			serverJSON.VersionDetail.Version,
			existingLatest.VersionDetail.Version,
			publishTime,
			existingPublishedAt,
		) > 0
	}

	// Create complete server with metadata
	server := serverJSON // Copy the input

	// Initialize meta if not present
	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}

	// Set registry metadata
	server.Meta.Official = &apiv0.RegistryExtensions{
		ID:          uuid.New().String(),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
		ReleaseDate: publishTime.Format(time.RFC3339),
	}

	// Create server in database
	serverRecord, err := s.db.CreateServer(ctx, &server)
	if err != nil {
		return nil, err
	}

	// Mark previous latest as no longer latest
	if isNewLatest && existingLatest != nil {
		var existingLatestID string
		if existingLatest.Meta != nil && existingLatest.Meta.Official != nil {
			existingLatestID = existingLatest.Meta.Official.ID
		}
		if existingLatestID != "" {
			// Update the existing server to set is_latest = false
			existingLatest.Meta.Official.IsLatest = false
			existingLatest.Meta.Official.UpdatedAt = time.Now()
			if _, err := s.db.UpdateServer(ctx, existingLatestID, existingLatest); err != nil {
				return nil, err
			}
		}
	}

	// Return the server record directly
	return serverRecord, nil
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, serverDetail apiv0.ServerJSON) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := s.db.List(ctx, filter, "", 1000)
		if err != nil {
			return fmt.Errorf("failed to check remote URL conflict: %w", err)
		}

		// Check if any conflicting server has a different name
		for _, conflictingServer := range conflictingServers {
			if conflictingServer.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Name)
			}
		}
	}

	return nil
}

// getCurrentLatestVersion finds the current latest version from existing server versions
func (s *registryServiceImpl) getCurrentLatestVersion(existingServerVersions []*apiv0.ServerJSON) *apiv0.ServerJSON {
	for _, server := range existingServerVersions {
		if server.Meta != nil && server.Meta.Official != nil &&
			server.Meta.Official.IsLatest {
			return server
		}
	}
	return nil
}

// EditServer updates an existing server with new details (admin operation)
func (s *registryServiceImpl) EditServer(id string, req apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	serverJSON := req

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, serverJSON); err != nil {
		return nil, err
	}

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, id, &serverJSON)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}
