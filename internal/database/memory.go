package database

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// MemoryDB is an in-memory implementation of the Database interface
type MemoryDB struct {
	entries map[string]*apiv0.ServerJSON // maps registry metadata ID to ServerJSON
	mu      sync.RWMutex
}

// NewMemoryDB creates a new instance of the in-memory database
func NewMemoryDB(e map[string]*apiv0.ServerJSON) *MemoryDB {
	// Convert input ServerJSON entries to have proper metadata
	serverRecords := make(map[string]*apiv0.ServerJSON)
	for registryID, serverDetail := range e {
		// Create a copy and add registry metadata
		now := time.Now()
		server := *serverDetail // Copy the server
		if server.Meta == nil {
			server.Meta = &apiv0.ServerMeta{}
		}
		server.Meta.IOModelContextProtocolRegistry = &apiv0.RegistryExtensions{
			ID:          registryID,
			PublishedAt: now,
			UpdatedAt:   now,
			IsLatest:    true,
			ReleaseDate: now.Format(time.RFC3339),
		}
		if server.Meta.Publisher == nil {
			server.Meta.Publisher = make(map[string]interface{})
		}
		serverRecords[registryID] = &server
	}
	return &MemoryDB{
		entries: serverRecords,
	}
}

//nolint:cyclop,gocyclo,gocognit // Complexity from filtering logic is acceptable for memory implementation
func (db *MemoryDB) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*apiv0.ServerJSON, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Convert all entries to a slice for pagination, filter by is_latest
	var allEntries []*apiv0.ServerJSON
	for _, entry := range db.entries {
		if entry.Meta != nil && entry.Meta.IOModelContextProtocolRegistry != nil && entry.Meta.IOModelContextProtocolRegistry.IsLatest {
			allEntries = append(allEntries, entry)
		}
	}

	// Simple filtering implementation
	var filteredEntries []*apiv0.ServerJSON
	for _, entry := range allEntries {
		include := true

		// Apply filters if any
		for key, value := range filter {
			switch key {
			case "name":
				if entry.Name != value.(string) {
					include = false
				}
			case "version":
				if entry.VersionDetail.Version != value.(string) {
					include = false
				}
			case "status":
				if string(entry.Status) != value.(string) {
					include = false
				}
			case "remote_url":
				found := false
				remoteURL := value.(string)
				for _, remote := range entry.Remotes {
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
		iID := ""
		jID := ""
		if filteredEntries[i].Meta != nil && filteredEntries[i].Meta.IOModelContextProtocolRegistry != nil {
			iID = filteredEntries[i].Meta.IOModelContextProtocolRegistry.ID
		}
		if filteredEntries[j].Meta != nil && filteredEntries[j].Meta.IOModelContextProtocolRegistry != nil {
			jID = filteredEntries[j].Meta.IOModelContextProtocolRegistry.ID
		}
		return iID < jID
	})

	// Find starting point for cursor-based pagination using registry metadata ID
	startIdx := 0
	if cursor != "" {
		for i, entry := range filteredEntries {
			if entry.Meta != nil && entry.Meta.IOModelContextProtocolRegistry != nil &&
				entry.Meta.IOModelContextProtocolRegistry.ID == cursor {
				startIdx = i + 1 // Start after the cursor
				break
			}
		}
	}

	// Apply pagination
	endIdx := min(startIdx+limit, len(filteredEntries))

	var result []*apiv0.ServerJSON
	if startIdx < len(filteredEntries) {
		result = filteredEntries[startIdx:endIdx]
	} else {
		result = []*apiv0.ServerJSON{}
	}

	// Determine next cursor using registry metadata ID
	nextCursor := ""
	if endIdx < len(filteredEntries) && len(result) > 0 {
		lastEntry := result[len(result)-1]
		if lastEntry.Meta != nil && lastEntry.Meta.IOModelContextProtocolRegistry != nil {
			nextCursor = lastEntry.Meta.IOModelContextProtocolRegistry.ID
		}
	}

	return result, nextCursor, nil
}

// GetByID retrieves a single ServerRecord by its registry metadata ID
func (db *MemoryDB) GetByID(ctx context.Context, id string) (*apiv0.ServerJSON, error) {
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
func (db *MemoryDB) Publish(ctx context.Context, serverDetail apiv0.ServerJSON, publisherExtensions map[string]interface{}, registryMetadata apiv0.RegistryExtensions) (*apiv0.ServerJSON, error) {
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

	// Create server record with metadata
	record := serverDetail // Copy the input

	// Initialize meta if not present
	if record.Meta == nil {
		record.Meta = &apiv0.ServerMeta{}
	}

	// Set registry metadata
	record.Meta.IOModelContextProtocolRegistry = &registryMetadata

	// Set publisher extensions if provided
	if len(publisherExtensions) > 0 {
		record.Meta.Publisher = publisherExtensions
	}

	// Store the record using registry metadata ID
	db.entries[registryMetadata.ID] = &record

	return &record, nil
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
		if record.Meta != nil && record.Meta.IOModelContextProtocolRegistry != nil {
			db.entries[record.Meta.IOModelContextProtocolRegistry.ID] = record
		}
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
		if entry.Meta != nil && entry.Meta.IOModelContextProtocolRegistry != nil {
			entry.Meta.IOModelContextProtocolRegistry.IsLatest = isLatest
			entry.Meta.IOModelContextProtocolRegistry.UpdatedAt = time.Now()
		}
		return nil
	}

	return ErrNotFound
}

// UpdateServer updates an existing server record with new server details
func (db *MemoryDB) UpdateServer(ctx context.Context, id string, serverDetail apiv0.ServerJSON, publisherExtensions map[string]interface{}) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	entry, exists := db.entries[id]
	if !exists {
		return nil, ErrNotFound
	}

	// Update the server details by copying fields from serverDetail
	*entry = serverDetail

	// Ensure meta exists and update it
	if entry.Meta == nil {
		entry.Meta = &apiv0.ServerMeta{}
	}
	if entry.Meta.IOModelContextProtocolRegistry != nil {
		entry.Meta.IOModelContextProtocolRegistry.UpdatedAt = time.Now()
	}
	if len(publisherExtensions) > 0 {
		entry.Meta.Publisher = publisherExtensions
	}

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
