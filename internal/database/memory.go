package database

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
)

// MemoryDB is an in-memory implementation of the Database interface
type MemoryDB struct {
	entries map[string]*model.ServerDetail
	mu      sync.RWMutex
}

// NewMemoryDB creates a new instance of the in-memory database
func NewMemoryDB(e map[string]*model.Server) *MemoryDB {
	// Convert Server entries to ServerDetail entries
	serverDetails := make(map[string]*model.ServerDetail)
	for k, v := range e {
		serverDetails[k] = &model.ServerDetail{
			Server: *v,
		}
	}
	return &MemoryDB{
		entries: serverDetails,
	}
}

// isSemanticVersion checks if a version string follows semantic versioning format
func isSemanticVersion(version string) bool {
	// Basic regex pattern for semantic versioning (simplified)
	// Allows: major.minor.patch with optional prerelease (e.g., 1.0.0-alpha.1)
	parts := strings.Split(version, "-")
	if len(parts) > 2 {
		return false
	}

	// Check main version part (major.minor.patch)
	versionParts := strings.Split(parts[0], ".")
	if len(versionParts) != 3 {
		return false
	}

	for _, part := range versionParts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}

	// If there's a prerelease part, it can contain alphanumeric characters and dots
	if len(parts) == 2 {
		prerelease := parts[1]
		if prerelease == "" {
			return false
		}
		// Basic validation for prerelease - allow letters, numbers, dots
		for _, r := range prerelease {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
				return false
			}
		}
	}

	return true
}

// compareSemanticVersions compares two semantic version strings
// Returns:
//
//	-1 if version1 < version2
//	 0 if version1 == version2
//	+1 if version1 > version2
func compareSemanticVersions(version1, version2 string) int {
	// Parse version parts (main version and prerelease)
	parts1 := strings.Split(version1, "-")
	parts2 := strings.Split(version2, "-")

	mainParts1 := strings.Split(parts1[0], ".")
	mainParts2 := strings.Split(parts2[0], ".")

	// Compare major, minor, patch
	for i := 0; i < 3; i++ {
		num1, _ := strconv.Atoi(mainParts1[i])
		num2, _ := strconv.Atoi(mainParts2[i])

		if num1 < num2 {
			return -1
		} else if num1 > num2 {
			return 1
		}
	}

	// If main versions are equal, compare prerelease
	hasPrerelease1 := len(parts1) > 1
	hasPrerelease2 := len(parts2) > 1

	// Version without prerelease is higher than with prerelease
	if !hasPrerelease1 && hasPrerelease2 {
		return 1
	}
	if hasPrerelease1 && !hasPrerelease2 {
		return -1
	}

	// Both have prerelease, compare lexicographically
	if hasPrerelease1 && hasPrerelease2 {
		if parts1[1] < parts2[1] {
			return -1
		} else if parts1[1] > parts2[1] {
			return 1
		}
	}

	return 0
}

// compareVersions implements the versioning strategy agreed upon in the discussion:
// 1. If both versions are valid semver, use semantic version comparison
// 2. If neither are valid semver, use publication timestamp (return 0 to indicate equal for sorting)
// 3. If one is semver and one is not, the semver version is always considered higher
func compareVersions(version1, version2 string, timestamp1, timestamp2 time.Time) int {
	isSemver1 := isSemanticVersion(version1)
	isSemver2 := isSemanticVersion(version2)

	if isSemver1 && isSemver2 {
		// Both are semver - use semantic comparison
		return compareSemanticVersions(version1, version2)
	}

	if !isSemver1 && !isSemver2 {
		// Neither are semver - use timestamp comparison
		if timestamp1.Before(timestamp2) {
			return -1
		} else if timestamp1.After(timestamp2) {
			return 1
		}
		return 0
	}

	// One is semver, one is not - semver is always higher
	if isSemver1 && !isSemver2 {
		return 1
	}
	return -1
}

// List retrieves all MCPRegistry entries with optional filtering and pagination
//
//gocognit:ignore
func (db *MemoryDB) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*model.Server, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Convert all entries to a slice for pagination
	var allEntries []*model.Server
	for _, entry := range db.entries {
		serverCopy := entry.Server
		allEntries = append(allEntries, &serverCopy)
	}

	// Simple filtering implementation
	var filteredEntries []*model.Server
	for _, entry := range allEntries {
		include := true

		// Apply filters if any
		for key, value := range filter {
			switch key {
			case "name":
				if entry.Name != value.(string) {
					include = false
				}
			case "repoUrl":
				if entry.Repository.URL != value.(string) {
					include = false
				}
			case "serverDetail.id":
				if entry.ID != value.(string) {
					include = false
				}
			case "version":
				if entry.VersionDetail.Version != value.(string) {
					include = false
				}
				// Add more filter options as needed
			}
		}

		if include {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Find starting point for cursor-based pagination
	startIdx := 0
	if cursor != "" {
		for i, entry := range filteredEntries {
			if entry.ID == cursor {
				startIdx = i + 1 // Start after the cursor
				break
			}
		}
	}

	// Sort filteredEntries by ID for consistent pagination
	sort.Slice(filteredEntries, func(i, j int) bool {
		return filteredEntries[i].ID < filteredEntries[j].ID
	})

	// Apply pagination
	endIdx := min(startIdx+limit, len(filteredEntries))

	var result []*model.Server
	if startIdx < len(filteredEntries) {
		result = filteredEntries[startIdx:endIdx]
	} else {
		result = []*model.Server{}
	}

	// Determine next cursor
	nextCursor := ""
	if endIdx < len(filteredEntries) {
		nextCursor = filteredEntries[endIdx-1].ID
	}

	return result, nextCursor, nil
}

// GetByID retrieves a single ServerDetail by its ID
func (db *MemoryDB) GetByID(ctx context.Context, id string) (*model.ServerDetail, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	if entry, exists := db.entries[id]; exists {
		// Return a copy of the ServerDetail
		serverDetailCopy := *entry
		return &serverDetailCopy, nil
	}

	return nil, ErrNotFound
}

// Publish adds a new ServerDetail to the database
func (db *MemoryDB) Publish(ctx context.Context, serverDetail *model.ServerDetail) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// check for name
	if serverDetail.Name == "" {
		return ErrInvalidInput
	}

	// Validate version string length (max 255 characters as per schema)
	if len(serverDetail.VersionDetail.Version) > 255 {
		return ErrInvalidInput
	}

	// Check that the name and version combination is unique
	var existingEntries []*model.ServerDetail
	for _, entry := range db.entries {
		if entry.Name == serverDetail.Name {
			if entry.VersionDetail.Version == serverDetail.VersionDetail.Version {
				return ErrAlreadyExists
			}
			existingEntries = append(existingEntries, entry)
		}
	}

	// Parse the current time for timestamp-based comparisons
	currentTime := time.Now()
	if serverDetail.VersionDetail.ReleaseDate == "" {
		serverDetail.VersionDetail.ReleaseDate = currentTime.Format(time.RFC3339)
	}

	if serverDetail.Repository.URL == "" {
		return ErrInvalidInput
	}

	// Always generate a new UUID for the ID
	serverDetail.ID = uuid.New().String()

	// Determine if this version should be marked as latest based on the versioning strategy
	isLatest := true
	if len(existingEntries) > 0 {
		// Compare with existing versions to determine if this should be latest
		for _, existing := range existingEntries {
			existingTime, _ := time.Parse(time.RFC3339, existing.VersionDetail.ReleaseDate)
			comparison := compareVersions(serverDetail.VersionDetail.Version, existing.VersionDetail.Version, currentTime, existingTime)
			if comparison < 0 {
				// New version is lower than existing version
				isLatest = false
				break
			}
		}

		// If this version will be latest, mark all existing versions as not latest
		if isLatest {
			for _, existing := range existingEntries {
				existing.VersionDetail.IsLatest = false
			}
		}
	}

	serverDetail.VersionDetail.IsLatest = isLatest

	// Store a copy of the entire ServerDetail
	serverDetailCopy := *serverDetail
	db.entries[serverDetail.ID] = &serverDetailCopy

	return nil
}

// ImportSeed imports initial data from a seed file into memory database
func (db *MemoryDB) ImportSeed(ctx context.Context, seedFilePath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Read the seed file
	seedData, err := ReadSeedFile(ctx, seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	log.Printf("Importing %d servers into memory database", len(seedData))

	db.mu.Lock()
	defer db.mu.Unlock()

	for i, server := range seedData {
		if server.ID == "" || server.Name == "" {
			log.Printf("Skipping server %d: ID or Name is empty", i+1)
			continue
		}

		// Set default version information if missing
		if server.VersionDetail.Version == "" {
			server.VersionDetail.Version = "0.0.1-seed"
			server.VersionDetail.ReleaseDate = time.Now().Format(time.RFC3339)
			server.VersionDetail.IsLatest = true
		}

		// Store a copy of the server detail
		serverDetailCopy := server
		db.entries[server.ID] = &serverDetailCopy

		log.Printf("[%d/%d] Imported server: %s", i+1, len(seedData), server.Name)
	}

	log.Println("Memory database import completed successfully")
	return nil
}

// Close closes the database connection
// For an in-memory database, this is a no-op
func (db *MemoryDB) Close() error {
	return nil
}

// Connection returns information about the database connection
func (db *MemoryDB) Connection() *ConnectionInfo {
	return &ConnectionInfo{
		Type:        ConnectionTypeMemory,
		IsConnected: true, // Memory DB is always connected
		Raw:         db.entries,
	}
}
