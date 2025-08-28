package service

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
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

// validateMCPBPackage validates MCPB packages
func validateMCPBPackage(host string) error {
	allowedHosts := []string{
		"github.com",
		"www.github.com",
		"gitlab.com",
		"www.gitlab.com",
	}

	isAllowed := false
	for _, allowed := range allowedHosts {
		if host == allowed {
			isAllowed = true
			break
		}
	}

	if !isAllowed {
		return fmt.Errorf("MCPB packages must be hosted on allowlisted providers (GitHub or GitLab). Host '%s' is not allowed", host)
	}

	return nil
}

// validatePackage validates packages to ensure they meet requirements
func validatePackage(pkg *model.Package) error {
	registryType := strings.ToLower(pkg.RegistryType)

	// For direct download packages (mcpb or direct URLs)
	if registryType == "mcpb" ||
		strings.HasPrefix(pkg.Identifier, "http://") || strings.HasPrefix(pkg.Identifier, "https://") {
		parsedURL, err := url.Parse(pkg.Identifier)
		if err != nil {
			return fmt.Errorf("invalid package URL: %w", err)
		}

		host := strings.ToLower(parsedURL.Host)

		// For MCPB packages, validate they're from allowed hosts
		if registryType == "mcpb" || strings.HasSuffix(strings.ToLower(pkg.Identifier), ".mcpb") {
			return validateMCPBPackage(host)
		}

		// For other URL-based packages, just ensure it's valid
		if parsedURL.Scheme == "" || parsedURL.Host == "" {
			return fmt.Errorf("package URL must be a valid absolute URL")
		}
		return nil
	}

	// For registry-based packages, no special validation needed
	// Registry types like "npm", "pypi", "docker-hub", "nuget" are all valid
	return nil
}

// Publish publishes a server with separated extensions
func (s *registryServiceImpl) Publish(req apiv0.PublishRequest) (*apiv0.ServerRecord, error) {
	// Create a timeout context for the database operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublisherExtensions(req); err != nil {
		return nil, err
	}

	// Validate server name exists and format
	if _, err := validators.ParseServerName(req.Server); err != nil {
		return nil, err
	}

	// Validate all packages
	for _, pkg := range req.Server.Packages {
		if err := validatePackage(&pkg); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	// Validate reverse-DNS namespace matching for remote URLs
	if err := validators.ValidateRemoteNamespaceMatch(req.Server); err != nil {
		return nil, err
	}

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, req.Server); err != nil {
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
		existingVersion := server.Server.VersionDetail.Version

		// Early exit: check for duplicate version
		if existingVersion == newVersion {
			return nil, database.ErrInvalidVersion
		}

		if server.XIOModelContextProtocolRegistry.IsLatest {
			existingLatestID = server.XIOModelContextProtocolRegistry.ID
			existingTime, _ := time.Parse(time.RFC3339, server.XIOModelContextProtocolRegistry.ReleaseDate)

			// Compare versions using the proper versioning strategy
			comparison := CompareVersions(newVersion, existingVersion, currentTime, existingTime)
			if comparison <= 0 {
				// New version is not greater than existing latest
				isLatest = false
			}
		}
	}

	// Extract publisher extensions from request
	publisherExtensions := validators.ExtractPublisherExtensions(req)

	// Create registry metadata with service-determined values
	registryMetadata := apiv0.RegistryExtensions{
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

// EditServer updates an existing server with new details (admin operation)
func (s *registryServiceImpl) EditServer(id string, req apiv0.PublishRequest) (*apiv0.ServerRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Validate the request
	if err := validators.ValidatePublisherExtensions(req); err != nil {
		return nil, err
	}

	// Validate server name exists and format
	if _, err := validators.ParseServerName(req.Server); err != nil {
		return nil, err
	}

	// Validate all packages
	for _, pkg := range req.Server.Packages {
		if err := validatePackage(&pkg); err != nil {
			return nil, fmt.Errorf("validation failed: %w", err)
		}
	}

	// Validate reverse-DNS namespace matching for remote URLs
	if err := validators.ValidateRemoteNamespaceMatch(req.Server); err != nil {
		return nil, err
	}

	// Check for duplicate remote URLs
	if err := s.validateNoDuplicateRemoteURLs(ctx, req.Server); err != nil {
		return nil, err
	}

	// Extract publisher extensions from request
	publisherExtensions := validators.ExtractPublisherExtensions(req)

	// Update server in database
	serverRecord, err := s.db.UpdateServer(ctx, id, req.Server, publisherExtensions)
	if err != nil {
		return nil, err
	}

	// Return ServerRecord directly (it's now the same as ServerResponse)
	return serverRecord, nil
}
