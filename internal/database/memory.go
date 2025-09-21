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
	entries     map[string]*apiv0.ServerJSON // maps registry metadata version_id to ServerJSON
	mu          sync.RWMutex
	publishLocks map[string]*sync.Mutex       // per-server-name locks for publish operations
	locksMu      sync.Mutex                   // protects publishLocks map
}

func NewMemoryDB() *MemoryDB {
	// Convert input ServerJSON entries to have proper metadata
	serverRecords := make(map[string]*apiv0.ServerJSON)
	return &MemoryDB{
		entries:      serverRecords,
		publishLocks: make(map[string]*sync.Mutex),
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

func (db *MemoryDB) GetByVersionID(ctx context.Context, versionID string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find entry by registry metadata version_id
	if entry, exists := db.entries[versionID]; exists {
		// Return a copy of the ServerRecord
		entryCopy := *entry
		return &entryCopy, nil
	}

	return nil, ErrNotFound
}

// GetByServerID retrieves the latest version of a server by server ID
func (db *MemoryDB) GetByServerID(ctx context.Context, serverID string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find the latest version with the given server ID
	var latestEntry *apiv0.ServerJSON
	for _, entry := range db.entries {
		if entry.Meta != nil && entry.Meta.Official != nil &&
			entry.Meta.Official.ServerID == serverID &&
			entry.Meta.Official.IsLatest {
			latestEntry = entry
			break
		}
	}

	if latestEntry == nil {
		return nil, ErrNotFound
	}

	// Return a copy
	entryCopy := *latestEntry
	return &entryCopy, nil
}

// GetByServerIDAndVersion retrieves a specific version of a server by server ID and version
func (db *MemoryDB) GetByServerIDAndVersion(ctx context.Context, serverID string, version string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find the entry with matching server ID and version
	for _, entry := range db.entries {
		if entry.Meta != nil && entry.Meta.Official != nil &&
			entry.Meta.Official.ServerID == serverID &&
			entry.Version == version {
			// Return a copy
			entryCopy := *entry
			return &entryCopy, nil
		}
	}

	return nil, ErrNotFound
}

// GetAllVersionsByServerID retrieves all versions of a server by server ID
func (db *MemoryDB) GetAllVersionsByServerID(ctx context.Context, serverID string) ([]*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []*apiv0.ServerJSON
	for _, entry := range db.entries {
		if entry.Meta != nil && entry.Meta.Official != nil &&
			entry.Meta.Official.ServerID == serverID {
			// Add a copy
			entryCopy := *entry
			results = append(results, &entryCopy)
		}
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	// Sort by published date, latest first
	sort.Slice(results, func(i, j int) bool {
		iTime := results[i].Meta.Official.PublishedAt
		jTime := results[j].Meta.Official.PublishedAt
		return iTime.After(jTime)
	})

	return results, nil
}

func (db *MemoryDB) CreateServer(ctx context.Context, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the VersionID from the registry metadata
	if server.Meta == nil || server.Meta.Official == nil {
		return nil, fmt.Errorf("server must have registry metadata with ServerID and VersionID")
	}

	versionID := server.Meta.Official.VersionID

	if versionID == "" {
		return nil, fmt.Errorf("server must have VersionID in registry metadata")
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	// Store the record using registry metadata VersionID
	db.entries[versionID] = server

	return server, nil
}

func (db *MemoryDB) UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate that meta structure exists and VersionID matches path (consistent with PostgreSQL implementation)
	if server.Meta == nil || server.Meta.Official == nil || server.Meta.Official.VersionID != id {
		return nil, fmt.Errorf("%w: io.modelcontextprotocol.registry/official.version_id must match path id (%s)", ErrInvalidInput, id)
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

func (db *MemoryDB) WithPublishLock(ctx context.Context, serverName string, fn func(ctx context.Context) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Get or create a lock for this specific server name
	db.locksMu.Lock()
	lock, exists := db.publishLocks[serverName]
	if !exists {
		lock = &sync.Mutex{}
		db.publishLocks[serverName] = lock
	}
	db.locksMu.Unlock()

	// Acquire the server-specific lock
	lock.Lock()
	defer lock.Unlock()

	return fn(ctx)
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
//
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
		if entry.Version != *filter.Version {
			return false
		}
	}

	// Check isLatest filter
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

// getRegistryID safely extracts the registry version ID from an entry
func (db *MemoryDB) getRegistryID(entry *apiv0.ServerJSON) string {
	if entry.Meta != nil && entry.Meta.Official != nil {
		return entry.Meta.Official.VersionID
	}
	return ""
}
