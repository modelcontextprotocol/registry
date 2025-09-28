package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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

// ListServers returns registry entries with cursor-based pagination and optional filtering
func (s *registryServiceImpl) ListServers(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	// If limit is not set or negative, use a default limit
	if limit <= 0 {
		limit = 30
	}

	// Use the database's ListServers method with pagination and filtering
	serverRecords, nextCursor, err := s.db.ListServers(ctx, tx, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}

	return serverRecords, nextCursor, nil
}


// GetServerByName retrieves the latest version of a server by its server name
func (s *registryServiceImpl) GetServerByName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByName(ctx, tx, serverName)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version
func (s *registryServiceImpl) GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*apiv0.ServerResponse, error) {
	serverRecord, err := s.db.GetServerByNameAndVersion(ctx, tx, serverName, version)
	if err != nil {
		return nil, err
	}

	return serverRecord, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (s *registryServiceImpl) GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string) ([]*apiv0.ServerResponse, error) {
	serverRecords, err := s.db.GetAllVersionsByServerName(ctx, tx, serverName)
	if err != nil {
		return nil, err
	}

	return serverRecords, nil
}

// CreateServer creates a new server version
func (s *registryServiceImpl) CreateServer(ctx context.Context, tx pgx.Tx, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	// Validate the request
	if err := validators.ValidatePublishRequest(*req, s.cfg); err != nil {
		return nil, err
	}

	publishTime := time.Now()
	serverJSON := *req

	// Acquire advisory lock to prevent concurrent publishes of the same server
	if err := s.db.AcquirePublishLock(ctx, tx, serverJSON.Name); err != nil {
		return nil, err
	}

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, tx, serverJSON); err != nil {
		return nil, err
	}

	// Check we haven't exceeded the maximum versions allowed for a server
	versionCount, err := s.db.CountServerVersions(ctx, tx, serverJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxServersReached
	}

	// Check this isn't a duplicate version
	versionExists, err := s.db.CheckVersionExists(ctx, tx, serverJSON.Name, serverJSON.Version)
	if err != nil {
		return nil, err
	}
	if versionExists {
		return nil, database.ErrInvalidVersion
	}

	// Get current latest version to determine if new version should be latest
	currentLatest, err := s.db.GetCurrentLatestVersion(ctx, tx, serverJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	// Determine if this version should be marked as latest
	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		isNewLatest = CompareVersions(
			serverJSON.Version,
			currentLatest.Server.Version,
			publishTime,
			existingPublishedAt,
		) > 0
	}

	// Unmark old latest version if needed
	if isNewLatest && currentLatest != nil {
		if err := s.db.UnmarkAsLatest(ctx, tx, serverJSON.Name); err != nil {
			return nil, err
		}
	}

	// Create metadata for the new server
	officialMeta := &apiv0.RegistryExtensions{
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	// Insert new server version
	return s.db.CreateServer(ctx, tx, &serverJSON, officialMeta)
}

// validateNoDuplicateRemoteURLs checks that no other server is using the same remote URLs
func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, tx pgx.Tx, serverDetail apiv0.ServerJSON) error {
	// Check each remote URL in the new server for conflicts
	for _, remote := range serverDetail.Remotes {
		// Use filter to find servers with this remote URL
		filter := &database.ServerFilter{RemoteURL: &remote.URL}

		conflictingServers, _, err := s.db.ListServers(ctx, tx, filter, "", 1000)
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


// UpdateServer updates an existing server with new details
func (s *registryServiceImpl) UpdateServer(ctx context.Context, tx pgx.Tx, serverName, version string, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	// Validate the request
	if err := validators.ValidatePublishRequest(*req, s.cfg); err != nil {
		return nil, err
	}

	// Get the current server to preserve metadata
	currentServer, err := s.db.GetServerByNameAndVersion(ctx, tx, serverName, version)
	if err != nil {
		return nil, err
	}

	// Acquire advisory lock to prevent concurrent edits of servers with same name
	if err := s.db.AcquirePublishLock(ctx, tx, serverName); err != nil {
		return nil, err
	}

	// Merge the request with the current server, preserving metadata
	updatedServer := *req

	// Update the UpdatedAt timestamp
	// Note: Since we're moving away from ServerID/VersionID, we just update the timestamp
	// The IsLatest and other metadata will be preserved by the database layer

	// Check for duplicate remote URLs using the updated server
	if err := s.validateNoDuplicateRemoteURLs(ctx, tx, updatedServer); err != nil {
		return nil, err
	}

	// Update server in database
	return s.db.UpdateServer(ctx, tx, serverName, version, &updatedServer)
}
