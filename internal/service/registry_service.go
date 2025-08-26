package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/database"
	"github.com/modelcontextprotocol/registry/internal/model"
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
func (s *registryServiceImpl) List(cursor string, limit int) ([]model.ServerResponse, string, error) {
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

	// Convert ServerRecord to ServerResponse format
	result := make([]model.ServerResponse, len(serverRecords))
	for i, record := range serverRecords {
		result[i] = record.ToServerResponse()
	}

	return result, nextCursor, nil
}

// GetByID retrieves a specific server by its registry metadata ID in extension wrapper format
func (s *registryServiceImpl) GetByID(id string) (*model.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Use the database's GetByID method to retrieve the server record
	serverRecord, err := s.db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Convert ServerRecord to ServerResponse format
	response := serverRecord.ToServerResponse()
	return &response, nil
}

// Publish publishes a server with separated extensions
func (s *registryServiceImpl) Publish(req model.PublishRequest) (*model.ServerResponse, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := model.ValidatePublisherExtensions(req); err != nil {
		return nil, err
	}

	// Validate server name exists
	if _, err := model.ParseServerName(req.Server); err != nil {
		return nil, err
	}

	// Get the new version's details
	newVersion := req.Server.VersionDetail.Version
	newName := req.Server.Name
	currentTime := time.Now()

	// Check for existing versions of this server
	existingServers, _, err := s.db.List(ctx, map[string]any{"name": newName}, "", maxServerVersionsPerServer)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	if len(existingServers) == maxServerVersionsPerServer {
		return nil, database.ErrMaxServersReached
	}

	// Determine if this version should be marked as latest
	isLatest := true
	var existingLatestID string

	// Check all existing versions for duplicates and determine if new version should be latest
	for _, server := range existingServers {
		existingVersion := server.ServerJSON.VersionDetail.Version

		// Early exit: check for duplicate version
		if existingVersion == newVersion {
			return nil, database.ErrInvalidVersion
		}

		if server.RegistryMetadata.IsLatest {
			existingLatestID = server.RegistryMetadata.ID
			existingTime, _ := time.Parse(time.RFC3339, server.RegistryMetadata.ReleaseDate)

			// Compare versions using the proper versioning strategy
			comparison := CompareVersions(newVersion, existingVersion, currentTime, existingTime)
			if comparison <= 0 {
				// New version is not greater than existing latest
				isLatest = false
			}
		}
	}

	// Extract publisher extensions from request
	publisherExtensions := model.ExtractPublisherExtensions(req)

	// Create registry metadata with service-determined values
	registryMetadata := model.RegistryMetadata{
		ID:          uuid.New().String(),
		PublishedAt: currentTime,
		UpdatedAt:   currentTime,
		IsLatest:    isLatest,
		ReleaseDate: currentTime.Format(time.RFC3339),
	}

	// If this will be the latest version, we need to update the existing latest
	if isLatest && existingLatestID != "" {
		// Update the existing latest to no longer be latest
		if err := s.db.UpdateLatestFlag(ctx, existingLatestID, false); err != nil {
			return nil, err
		}
	}

	// Publish to database with the registry metadata
	serverRecord, err := s.db.Publish(ctx, req.Server, publisherExtensions, registryMetadata)
	if err != nil {
		return nil, err
	}

	// Convert ServerRecord to ServerResponse format
	response := serverRecord.ToServerResponse()
	return &response, nil
}
