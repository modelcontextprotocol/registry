package database

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// MemoryDB is an in-memory implementation of the Database interface
type MemoryDB struct {
	entries map[string]*apiv0.ServerRecord // maps registry metadata ID to ServerRecord
	mu      sync.RWMutex
}

// NewMemoryDB creates a new instance of the in-memory database
func NewMemoryDB(e map[string]*model.ServerJSON) *MemoryDB {
	// Convert ServerJSON entries to ServerRecord entries
	serverRecords := make(map[string]*apiv0.ServerRecord)
	for registryID, serverDetail := range e {
		// Create registry metadata
		now := time.Now()
		record := &apiv0.ServerRecord{
			Server: *serverDetail,
			XIOModelContextProtocolRegistry: apiv0.RegistryExtensions{
				ID:          registryID,
				PublishedAt: now,
				UpdatedAt:   now,
				IsLatest:    true,
				ReleaseDate: now.Format(time.RFC3339),
			},
			XPublisher: make(map[string]interface{}),
		}
		serverRecords[registryID] = record
	}
	return &MemoryDB{
		entries: serverRecords,
	}
}

//nolint:cyclop // Complexity from filtering logic is acceptable for memory implementation
func (db *MemoryDB) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*apiv0.ServerRecord, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Convert all entries to a slice for pagination, filter by is_latest
	var allEntries []*apiv0.ServerRecord
	for _, entry := range db.entries {
		if entry.XIOModelContextProtocolRegistry.IsLatest {
			allEntries = append(allEntries, entry)
		}
	}

	// Simple filtering implementation
	var filteredEntries []*apiv0.ServerRecord
	for _, entry := range allEntries {
		include := true

		// Apply filters if any
		for key, value := range filter {
			switch key {
			case "name":
				if entry.Server.Name != value.(string) {
					include = false
				}
			case "version":
				if entry.Server.VersionDetail.Version != value.(string) {
					include = false
				}
			case "status":
				if string(entry.Server.Status) != value.(string) {
					include = false
				}
			case "remote_url":
				found := false
				remoteURL := value.(string)
				for _, remote := range entry.Server.Remotes {
					if remote.URL == remoteURL {
						found = true
						break
					}
				}
				if !found {
					include = false
				}
			}
		}

		if include {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Sort filteredEntries by registry metadata ID for consistent pagination
	sort.Slice(filteredEntries, func(i, j int) bool {
		return filteredEntries[i].XIOModelContextProtocolRegistry.ID < filteredEntries[j].XIOModelContextProtocolRegistry.ID
	})

	// Find starting point for cursor-based pagination using registry metadata ID
	startIdx := 0
	if cursor != "" {
		for i, entry := range filteredEntries {
			if entry.XIOModelContextProtocolRegistry.ID == cursor {
				startIdx = i + 1 // Start after the cursor
				break
			}
		}
	}

	// Apply pagination
	endIdx := min(startIdx+limit, len(filteredEntries))

	var result []*apiv0.ServerRecord
	if startIdx < len(filteredEntries) {
		result = filteredEntries[startIdx:endIdx]
	} else {
		result = []*apiv0.ServerRecord{}
	}

	// Determine next cursor using registry metadata ID
	nextCursor := ""
	if endIdx < len(filteredEntries) {
		nextCursor = filteredEntries[endIdx-1].XIOModelContextProtocolRegistry.ID
	}

	return result, nextCursor, nil
}

// GetByID retrieves a single ServerRecord by its registry metadata ID
func (db *MemoryDB) GetByID(ctx context.Context, id string) (*apiv0.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find entry by registry metadata ID
	if entry, exists := db.entries[id]; exists {
		// Return a copy of the ServerRecord
		entryCopy := *entry
		return &entryCopy, nil
	}

	return nil, ErrNotFound
}

// Publish adds a new server to the database with separated server.json and extensions
func (db *MemoryDB) Publish(ctx context.Context, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}, registryMetadata apiv0.RegistryExtensions) (*apiv0.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Extract name and version for validation
	name := serverDetail.Name
	if name == "" {
		return nil, fmt.Errorf("name is required in server JSON")
	}

	version := serverDetail.VersionDetail.Version
	if version == "" {
		return nil, fmt.Errorf("version is required in version_detail")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Validate repository URL
	if serverDetail.Repository.URL == "" {
		return nil, ErrInvalidInput
	}

	// Create server record
	record := &apiv0.ServerRecord{
		Server:                          serverDetail,
		XIOModelContextProtocolRegistry: registryMetadata,
		XPublisher:                      publisherExtensions,
	}

	// Store the record using registry metadata ID
	db.entries[registryMetadata.ID] = record

	return record, nil
}

// ImportSeed imports initial data from a seed file into memory database
func (db *MemoryDB) ImportSeed(ctx context.Context, seedFilePath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// This will need to be updated to work with the new ServerRecord format
	// Read seed data using the shared ReadSeedFile function
	seedData, err := ReadSeedFile(ctx, seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	// Lock for concurrent access
	db.mu.Lock()
	defer db.mu.Unlock()

	// Import each server
	for _, record := range seedData {
		// Use the registry metadata ID as the map key
		db.entries[record.XIOModelContextProtocolRegistry.ID] = record
	}

	return nil
}

// UpdateLatestFlag updates the is_latest flag for a specific server record
func (db *MemoryDB) UpdateLatestFlag(ctx context.Context, id string, isLatest bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if entry, exists := db.entries[id]; exists {
		entry.XIOModelContextProtocolRegistry.IsLatest = isLatest
		entry.XIOModelContextProtocolRegistry.UpdatedAt = time.Now()
		return nil
	}

	return ErrNotFound
}

// UpdateServer updates an existing server record with new server details
func (db *MemoryDB) UpdateServer(ctx context.Context, id string, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}) (*apiv0.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	entry, exists := db.entries[id]
	if !exists {
		return nil, ErrNotFound
	}

	// Update the server details
	entry.Server = serverDetail
	entry.XPublisher = publisherExtensions
	entry.XIOModelContextProtocolRegistry.UpdatedAt = time.Now()

	// Return the updated record
	return entry, nil
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
