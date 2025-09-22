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

// List returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) List(filter *database.ServerFilter, cursor string, limit int) ([]apiv0.ServerJSON, string, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's ListServers method with pagination and filtering
	serverRecords, nextCursor, err := s.db.List(ctx, filter, cursor, limit)
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

// GetByVersionID retrieves a specific server by its registry metadata version ID
func (s *registryServiceImpl) GetByVersionID(versionID string) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecord, err := s.db.GetByVersionID(ctx, versionID)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}

// GetByServerID retrieves the latest version of a server by its server ID
func (s *registryServiceImpl) GetByServerID(serverID string) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecord, err := s.db.GetByServerID(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}

// GetByServerIDAndVersion retrieves a specific version of a server by server ID and version
func (s *registryServiceImpl) GetByServerIDAndVersion(serverID string, version string) (*apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecord, err := s.db.GetByServerIDAndVersion(ctx, serverID, version)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}

// GetAllVersionsByServerID retrieves all versions of a server by server ID
func (s *registryServiceImpl) GetAllVersionsByServerID(serverID string) ([]apiv0.ServerJSON, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecords, err := s.db.GetAllVersionsByServerID(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// Return ServerJSONs directly
	result := make([]apiv0.ServerJSON, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nil
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

	// Acquire advisory lock for this server name to prevent race conditions
	result, err := database.WithPublishLockT(ctx, s.db, req.Name, func(lockCtx context.Context) (*apiv0.ServerJSON, error) {
		publishTime := time.Now()
		serverJSON := req

		// Check for duplicate remote URLs
		if err := s.validateNoDuplicateRemoteURLs(lockCtx, serverJSON); err != nil {
			return nil, err
		}

		filter := &database.ServerFilter{Name: &serverJSON.Name}
		existingServerVersions, _, err := s.db.List(lockCtx, filter, "", maxServerVersionsPerServer)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}

		// Check we haven't exceeded the maximum versions allowed for a server
		if len(existingServerVersions) >= maxServerVersionsPerServer {
			return nil, database.ErrMaxServersReached
		}

		// Check this isn't a duplicate version
		for _, server := range existingServerVersions {
			existingVersion := server.Version
			if existingVersion == serverJSON.Version {
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
				serverJSON.Version,
				existingLatest.Version,
				publishTime,
				existingPublishedAt,
			) > 0
		}

		// Mark previous latest as no longer latest BEFORE creating new version
		// This prevents violating the unique constraint on isLatest
		if isNewLatest && existingLatest != nil {
			var existingLatestVersionID string
			if existingLatest.Meta != nil && existingLatest.Meta.Official != nil {
				existingLatestVersionID = existingLatest.Meta.Official.VersionID
			}
			if existingLatestVersionID != "" {
				// Update the existing server to set isLatest = false
				existingLatest.Meta.Official.IsLatest = false
				existingLatest.Meta.Official.UpdatedAt = time.Now()
				if _, err := s.db.UpdateServer(lockCtx, existingLatestVersionID, existingLatest); err != nil {
					return nil, err
				}
			}
		}

		// Create complete server with metadata
		server := s.createServerWithMetadata(serverJSON, existingServerVersions, publishTime, isNewLatest)

		// Create server in database
		serverRecord, err := s.db.CreateServer(lockCtx, &server)
		if err != nil {
			return nil, err
		}

		return serverRecord, nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// createServerWithMetadata creates a server with proper metadata including server_id and version_id
func (s *registryServiceImpl) createServerWithMetadata(
	serverJSON apiv0.ServerJSON,
	existingServerVersions []*apiv0.ServerJSON,
	publishTime time.Time,
	isNewLatest bool,
) apiv0.ServerJSON {
	server := serverJSON // Copy the input

	// Initialize meta if not present
	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}

	// Determine server_id - either from existing versions or generate new one
	var serverID string
	if len(existingServerVersions) > 0 {
		// Use existing server_id from any existing version
		firstExisting := existingServerVersions[0]
		if firstExisting.Meta != nil && firstExisting.Meta.Official != nil {
			serverID = firstExisting.Meta.Official.ServerID
		}
	}
	if serverID == "" {
		// This is the first version of a new server
		serverID = uuid.New().String()
	}

	// Set registry metadata
	server.Meta.Official = &apiv0.RegistryExtensions{
		ServerID:    serverID,
		VersionID:   uuid.New().String(),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return server
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
func (s *registryServiceImpl) EditServer(versionID string, req apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First get the current server to preserve metadata
	currentServer, err := s.db.GetByVersionID(ctx, versionID)
	if err != nil {
		return nil, err
	}

	// Validate the request
	if err := validators.ValidatePublishRequest(req, s.cfg); err != nil {
		return nil, err
	}

	// Merge the request with the current server, preserving metadata
	updatedServer := *currentServer // Copy the current server with all metadata

	// Update only the user-modifiable fields from the request
	updatedServer.Name = req.Name
	updatedServer.Description = req.Description
	updatedServer.Version = req.Version
	updatedServer.Status = req.Status
	updatedServer.Repository = req.Repository
	updatedServer.Remotes = req.Remotes
	updatedServer.Packages = req.Packages

	// Update the UpdatedAt timestamp in metadata
	if updatedServer.Meta != nil && updatedServer.Meta.Official != nil {
		updatedServer.Meta.Official.UpdatedAt = time.Now()
	}

	// Check for duplicate remote URLs using the updated server
	if err := s.validateNoDuplicateRemoteURLs(ctx, updatedServer); err != nil {
		return nil, err
	}

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, versionID, &updatedServer)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}
