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
func (s *registryServiceImpl) List(filter *database.ServerFilter, cursor string, limit int) ([]apiv0.ServerResponse, string, error) {
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

	// Return ServerResponses directly from database
	result := make([]apiv0.ServerResponse, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nextCursor, nil
}

// GetByVersionID retrieves a specific server by its registry metadata version ID
func (s *registryServiceImpl) GetByVersionID(versionID string) (*apiv0.ServerResponse, error) {
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
func (s *registryServiceImpl) GetByServerID(serverID string) (*apiv0.ServerResponse, error) {
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
func (s *registryServiceImpl) GetByServerIDAndVersion(serverID string, version string) (*apiv0.ServerResponse, error) {
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
func (s *registryServiceImpl) GetAllVersionsByServerID(serverID string) ([]apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverRecords, err := s.db.GetAllVersionsByServerID(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// Return ServerResponses directly from database
	result := make([]apiv0.ServerResponse, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = *record
	}

	return result, nil
}

// Publish publishes a server with flattened _meta extensions
func (s *registryServiceImpl) Publish(req apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
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
		existingVersion := server.Server.Version
		if existingVersion == serverJSON.Version {
			return nil, database.ErrInvalidVersion
		}
	}

	// Convert slice to correct type
	serverVersionsSlice := make([]apiv0.ServerResponse, len(existingServerVersions))
	for i, server := range existingServerVersions {
		serverVersionsSlice[i] = *server
	}

	// Determine if this version should be marked as latest
	existingLatest := s.getCurrentLatestVersion(serverVersionsSlice)
	isNewLatest := true
	if existingLatest != nil {
		var existingPublishedAt time.Time
		if existingLatest.Meta.Official != nil {
			existingPublishedAt = existingLatest.Meta.Official.PublishedAt
		}
		isNewLatest = CompareVersions(
			serverJSON.Version,
			existingLatest.Server.Version,
			publishTime,
			existingPublishedAt,
		) > 0
	}

	// Determine server_id - either from existing versions or generate new one
	var serverID string
	if len(serverVersionsSlice) > 0 {
		// Use existing server_id from any existing version
		firstExisting := serverVersionsSlice[0]
		if firstExisting.Meta.Official != nil {
			serverID = firstExisting.Meta.Official.ServerID
		}
	}
	if serverID == "" {
		// This is the first version of a new server
		serverID = uuid.New().String()
	}

	versionID := uuid.New().String()

	// Create server in database
	serverRecord, err := s.db.CreateServer(ctx, &serverJSON, serverID, versionID, isNewLatest)
	if err != nil {
		return nil, err
	}

	// Mark previous latest as no longer latest
	if isNewLatest && existingLatest != nil {
		var existingLatestVersionID string
		if existingLatest.Meta.Official != nil {
			existingLatestVersionID = existingLatest.Meta.Official.VersionID
		}
		if existingLatestVersionID != "" {
			// Update the existing server to set isLatest = false
			existingLatest.Meta.Official.IsLatest = false
			existingLatest.Meta.Official.UpdatedAt = time.Now()
			if _, err := s.db.UpdateServer(ctx, existingLatestVersionID, &existingLatest.Server); err != nil {
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
			if conflictingServer.Server.Name != serverDetail.Name {
				return fmt.Errorf("remote URL %s is already used by server %s", remote.URL, conflictingServer.Server.Name)
			}
		}
	}

	return nil
}

// getCurrentLatestVersion finds the current latest version from existing server versions
func (s *registryServiceImpl) getCurrentLatestVersion(existingServerVersions []apiv0.ServerResponse) *apiv0.ServerResponse {
	for _, server := range existingServerVersions {
		if server.Meta.Official != nil && server.Meta.Official.IsLatest {
			return &server
		}
	}
	return nil
}

// EditServer updates an existing server with new details (admin operation)
func (s *registryServiceImpl) EditServer(versionID string, req apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
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
	updatedServer.Server.Name = req.Name
	updatedServer.Server.Description = req.Description
	updatedServer.Server.Version = req.Version
	updatedServer.Server.Repository = req.Repository
	updatedServer.Server.Remotes = req.Remotes
	updatedServer.Server.Packages = req.Packages

	// Update the UpdatedAt timestamp in metadata
	if updatedServer.Meta.Official != nil {
		updatedServer.Meta.Official.UpdatedAt = time.Now()
	}

	// Check for duplicate remote URLs using the updated server
	if err := s.validateNoDuplicateRemoteURLs(ctx, updatedServer.Server); err != nil {
		return nil, err
	}

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, versionID, &updatedServer.Server)
	if err != nil {
		return nil, err
	}

	// Return the server record directly
	return serverRecord, nil
}

// UpdateServerStatus updates the status of the latest version of a server
func (s *registryServiceImpl) UpdateServerStatus(serverID string, status string) (*apiv0.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Get the latest version of the server
	currentServer, err := s.db.GetByServerID(ctx, serverID)
	if err != nil {
		return nil, err
	}

	// Validate the status value
	validStatuses := []string{"active", "deprecated", "deleted"}
	isValid := false
	for _, validStatus := range validStatuses {
		if status == validStatus {
			isValid = true
			break
		}
	}
	if !isValid {
		return nil, fmt.Errorf("invalid status: %s. Valid statuses are: active, deprecated, deleted", status)
	}

	// Update the status in the database (we need to add this method to the database interface)
	updatedServer, err := s.db.UpdateServerStatus(ctx, currentServer.GetVersionID(), status)
	if err != nil {
		return nil, err
	}

	return updatedServer, nil
}
