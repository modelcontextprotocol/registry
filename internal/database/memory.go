package database

import (
	"context"
	"sort"
	"sync"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// MemoryDB is an in-memory implementation of the Database interface
type MemoryDB struct {
	entries map[string]*model.Entry
	mu      sync.RWMutex
}

// NewMemoryDB creates a new instance of the in-memory database
func NewMemoryDB(e map[string]*model.Entry) *MemoryDB {
	return &MemoryDB{
		entries: e,
	}
}

// List retrieves all MCPRegistry entries with optional filtering and pagination
func (db *MemoryDB) List(ctx context.Context, filter map[string]interface{}, cursor string, limit int) ([]*model.Entry, string, error) {
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	if limit <= 0 {
		limit = 10 // Default limit
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Convert all entries to a slice for pagination
	var allEntries []*model.Entry
	for _, entry := range db.entries {
		entryCopy := *entry
		allEntries = append(allEntries, &entryCopy)
	}

	// Simple filtering implementation
	var filteredEntries []*model.Entry
	for _, entry := range allEntries {
		include := true

		// Apply filters if any
		if filter != nil {
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
	endIdx := startIdx + limit
	if endIdx > len(filteredEntries) {
		endIdx = len(filteredEntries)
	}

	var result []*model.Entry
	if startIdx < len(filteredEntries) {
		result = filteredEntries[startIdx:endIdx]
	} else {
		result = []*model.Entry{}
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
		return &model.ServerDetail{
			ID:            entry.ID,
			Name:          entry.Name,
			Description:   entry.Description,
			VersionDetail: entry.VersionDetail,
			Repository:    entry.Repository,
		}, nil
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

	// check that the name and the version are unique

	for _, entry := range db.entries {
		if entry.Name == serverDetail.Name && entry.VersionDetail.Version == serverDetail.VersionDetail.Version {
			return ErrAlreadyExists
		}
	}

	if serverDetail.Repository.URL == "" {
		return ErrInvalidInput
	}

	db.entries[serverDetail.ID] = &model.Entry{
		ID:            serverDetail.ID,
		Name:          serverDetail.Name,
		Description:   serverDetail.Description,
		VersionDetail: serverDetail.VersionDetail,
		Repository:    serverDetail.Repository,
	}

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
