package database

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// MemoryDB is an in-memory implementation of the Database interface
type MemoryDB struct {
	entries map[string]*apiv0.ServerJSON // maps registry metadata ID to ServerJSON
	mu      sync.RWMutex
}

func NewMemoryDB() *MemoryDB {
	// Convert input ServerJSON entries to have proper metadata
	serverRecords := make(map[string]*apiv0.ServerJSON)
	return &MemoryDB{
		entries: serverRecords,
	}
}

func (db *MemoryDB) List(
	ctx context.Context,
	filter *ServerFilter,
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

	// Convert all entries to a slice for pagination
	var allEntries []*apiv0.ServerJSON
	for _, entry := range db.entries {
		allEntries = append(allEntries, entry)
	}

	// Apply filtering and sorting
	filteredEntries := db.filterAndSort(allEntries, filter)

	// Find starting point for cursor-based pagination
	startIdx := 0
	if cursor != "" {
		for i, entry := range filteredEntries {
			if db.getRegistryID(entry) == cursor {
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

	// Determine next cursor
	nextCursor := ""
	if endIdx < len(filteredEntries) && len(result) > 0 {
		nextCursor = db.getRegistryID(result[len(result)-1])
	}

	return result, nextCursor, nil
}

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

func (db *MemoryDB) CreateServer(ctx context.Context, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the ID from the registry metadata
	if server.Meta == nil || server.Meta.Official == nil {
		return nil, fmt.Errorf("server must have registry metadata with ID")
	}

	id := server.Meta.Official.ID

	db.mu.Lock()
	defer db.mu.Unlock()

	// Store the record using registry metadata ID
	db.entries[id] = server

	return server, nil
}

func (db *MemoryDB) UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	_, exists := db.entries[id]
	if !exists {
		return nil, ErrNotFound
	}

	// Update the server
	db.entries[id] = server

	// Return the updated record
	return server, nil
}

// For an in-memory database, this is a no-op
func (db *MemoryDB) Close() error {
	return nil
}

// filterAndSort applies filtering and sorting to the entries
func (db *MemoryDB) filterAndSort(allEntries []*apiv0.ServerJSON, filter *ServerFilter) []*apiv0.ServerJSON {
	// Apply filtering
	var filteredEntries []*apiv0.ServerJSON
	for _, entry := range allEntries {
		if db.matchesFilter(entry, filter) {
			filteredEntries = append(filteredEntries, entry)
		}
	}

	// Sort by registry metadata ID for consistent pagination
	sort.Slice(filteredEntries, func(i, j int) bool {
		iID := db.getRegistryID(filteredEntries[i])
		jID := db.getRegistryID(filteredEntries[j])
		return iID < jID
	})

	return filteredEntries
}

// matchesFilter checks if an entry matches the provided filter
//nolint:cyclop // Filter matching logic is inherently complex but clear
func (db *MemoryDB) matchesFilter(entry *apiv0.ServerJSON, filter *ServerFilter) bool {
	if filter == nil {
		return true
	}

	// Check name filter
	if filter.Name != nil && entry.Name != *filter.Name {
		return false
	}

	// Check remote URL filter
	if filter.RemoteURL != nil {
		found := false
		for _, remote := range entry.Remotes {
			if remote.URL == *filter.RemoteURL {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check updatedSince filter
	if filter.UpdatedSince != nil {
		if entry.Meta == nil || entry.Meta.Official == nil {
			return false
		}
		if entry.Meta.Official.UpdatedAt.Before(*filter.UpdatedSince) ||
			entry.Meta.Official.UpdatedAt.Equal(*filter.UpdatedSince) {
			return false
		}
	}

	// Check name search filter (substring match)
	if filter.SubstringName != nil {
		// Case-insensitive substring search
		searchLower := strings.ToLower(*filter.SubstringName)
		nameLower := strings.ToLower(entry.Name)
		if !strings.Contains(nameLower, searchLower) {
			return false
		}
	}

	// Check exact version filter
	if filter.Version != nil {
		if entry.VersionDetail.Version != *filter.Version {
			return false
		}
	}

	// Check is_latest filter
	if filter.IsLatest != nil {
		if entry.Meta == nil || entry.Meta.Official == nil {
			return false
		}
		if entry.Meta.Official.IsLatest != *filter.IsLatest {
			return false
		}
	}

	return true
}

// getRegistryID safely extracts the registry ID from an entry
func (db *MemoryDB) getRegistryID(entry *apiv0.ServerJSON) string {
	if entry.Meta != nil && entry.Meta.Official != nil {
		return entry.Meta.Official.ID
	}
	return ""
}
