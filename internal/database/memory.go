package database

import (
	"context"
	"fmt"
	"log"
	"sort"
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
	for _, entry := range db.entries {
		if entry.Name == serverDetail.Name {
			if entry.VersionDetail.Version == serverDetail.VersionDetail.Version {
				return ErrAlreadyExists
			}
		}
	}

	if serverDetail.Repository.URL == "" {
		return ErrInvalidInput
	}

	// Always generate a new UUID for the ID
	serverDetail.ID = uuid.New().String()

	// If this version will be latest, mark all existing versions of the same server as not latest
	if serverDetail.VersionDetail.IsLatest {
		for _, entry := range db.entries {
			if entry.Name == serverDetail.Name {
				entry.VersionDetail.IsLatest = false
			}
		}
	}

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
