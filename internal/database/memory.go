package database

import (
	"context"
	"strings"
	"sync"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// MemoryDB is a minimal implementation for testing - simplified for now
type MemoryDB struct {
	entries map[string]*apiv0.ServerResponse // Key: version_id, Value: ServerResponse
	mu      sync.RWMutex
}

func NewMemoryDB() *MemoryDB {
	return &MemoryDB{
		entries: make(map[string]*apiv0.ServerResponse),
	}
}

func (db *MemoryDB) List(
	_ context.Context,
	filter *ServerFilter,
	_ string,
	_ int,
) ([]*apiv0.ServerResponse, string, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []*apiv0.ServerResponse

	for _, entry := range db.entries {
		if db.matchesFilter(entry, filter) {
			results = append(results, entry)
		}
	}

	return results, "", nil
}

// matchesFilter checks if a server entry matches the given filter criteria
func (db *MemoryDB) matchesFilter(entry *apiv0.ServerResponse, filter *ServerFilter) bool {
	if filter == nil {
		return true
	}

	// Apply RemoteURL filter for duplicate URL detection
	if filter.RemoteURL != nil {
		if !db.hasRemoteURL(entry, *filter.RemoteURL) {
			return false
		}
	}

	// Apply exact Name filter
	if filter.Name != nil && entry.Server.Name != *filter.Name {
		return false
	}

	// Apply SubstringName filter (for search functionality)
	if filter.SubstringName != nil {
		if !strings.Contains(strings.ToLower(entry.Server.Name), strings.ToLower(*filter.SubstringName)) {
			return false
		}
	}

	// Apply IsLatest filter (for version=latest)
	if filter.IsLatest != nil && *filter.IsLatest {
		if entry.Meta.Official == nil || !entry.Meta.Official.IsLatest {
			return false
		}
	}

	// Apply UpdatedSince filter (for incremental sync)
	if filter.UpdatedSince != nil {
		if entry.Meta.Official == nil || entry.Meta.Official.UpdatedAt.Before(*filter.UpdatedSince) {
			return false
		}
	}

	return true
}

// hasRemoteURL checks if the entry has a remote with the specified URL
func (db *MemoryDB) hasRemoteURL(entry *apiv0.ServerResponse, url string) bool {
	for _, remote := range entry.Server.Remotes {
		if remote.URL == url {
			return true
		}
	}
	return false
}

func (db *MemoryDB) GetByVersionID(_ context.Context, versionID string) (*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	if entry, exists := db.entries[versionID]; exists {
		return entry, nil
	}
	return nil, ErrNotFound
}

func (db *MemoryDB) GetByServerID(_ context.Context, serverID string) (*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find the latest version for this server
	var latest *apiv0.ServerResponse
	for _, entry := range db.entries {
		if entry.Meta.Official.ServerID == serverID && entry.Meta.Official.IsLatest {
			latest = entry
			break
		}
	}

	if latest != nil {
		return latest, nil
	}
	return nil, ErrNotFound
}

func (db *MemoryDB) GetByServerIDAndVersion(_ context.Context, serverID string, version string) (*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	// Find the specific server version
	for _, entry := range db.entries {
		if entry.Meta.Official.ServerID == serverID && entry.Server.Version == version {
			return entry, nil
		}
	}
	return nil, ErrNotFound
}

func (db *MemoryDB) GetAllVersionsByServerID(_ context.Context, serverID string) ([]*apiv0.ServerResponse, error) {
	db.mu.RLock()
	defer db.mu.RUnlock()

	var results []*apiv0.ServerResponse
	for _, entry := range db.entries {
		if entry.Meta.Official.ServerID == serverID {
			results = append(results, entry)
		}
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results, nil
}

func (db *MemoryDB) CreateServer(_ context.Context, server *apiv0.ServerJSON, serverID, versionID string, isLatest bool) (*apiv0.ServerResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Create the response with proper timestamps
	now := time.Now()
	response := &apiv0.ServerResponse{
		Server: *server,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    serverID,
				VersionID:   versionID,
				Status:      model.StatusActive, // Default status
				PublishedAt: now,
				UpdatedAt:   now,
				IsLatest:    isLatest,
			},
		},
	}

	// Store in entries
	db.entries[versionID] = response

	return response, nil
}

func (db *MemoryDB) UpdateServer(_ context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Find existing entry by versionID
	existing, exists := db.entries[id]
	if !exists {
		return nil, ErrNotFound
	}

	// Create updated response, preserving existing metadata but updating server content
	response := &apiv0.ServerResponse{
		Server: *server,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    existing.Meta.Official.ServerID,
				VersionID:   id,
				Status:      existing.Meta.Official.Status,
				PublishedAt: existing.Meta.Official.PublishedAt,
				UpdatedAt:   existing.Meta.Official.UpdatedAt,
				IsLatest:    existing.Meta.Official.IsLatest,
			},
		},
	}

	// Store the updated entry
	db.entries[id] = response

	return response, nil
}

func (db *MemoryDB) UpdateServerStatus(_ context.Context, versionID string, status string) (*apiv0.ServerResponse, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	// Find existing entry by versionID
	existing, exists := db.entries[versionID]
	if !exists {
		return nil, ErrNotFound
	}

	// Create updated response, preserving existing data but updating status and timestamp
	response := &apiv0.ServerResponse{
		Server: existing.Server, // Keep the server content unchanged
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    existing.Meta.Official.ServerID,
				VersionID:   versionID,
				Status:      model.Status(status), // Update the status
				PublishedAt: existing.Meta.Official.PublishedAt,
				UpdatedAt:   time.Now(), // Update the timestamp
				IsLatest:    existing.Meta.Official.IsLatest,
			},
		},
	}

	// Store the updated entry
	db.entries[versionID] = response

	return response, nil
}

func (db *MemoryDB) Close() error {
	return nil
}