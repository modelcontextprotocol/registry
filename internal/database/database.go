package database

import (
	"context"
	"errors"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// Common database errors
var (
	ErrNotFound          = errors.New("record not found")
	ErrAlreadyExists     = errors.New("record already exists")
	ErrInvalidInput      = errors.New("invalid input")
	ErrDatabase          = errors.New("database error")
	ErrInvalidVersion    = errors.New("invalid version: cannot publish duplicate version")
	ErrMaxServersReached = errors.New("maximum number of versions for this server reached (10000): please reach out at https://github.com/modelcontextprotocol/registry to explain your use case")
)

// Database defines the interface for database operations with extension wrapper architecture
type Database interface {
	// List retrieves all ServerRecord entries with optional filtering
	List(ctx context.Context, filter map[string]any, cursor string, limit int) ([]*apiv0.ServerRecord, string, error)
	// GetByID retrieves a single ServerRecord by its ID
	GetByID(ctx context.Context, id string) (*apiv0.ServerRecord, error)
	// Publish adds a new server to the database with separated server.json and extensions
	// The registryMetadata contains metadata determined by the service layer (e.g., is_latest, timestamps)
	Publish(ctx context.Context, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}, registryMetadata apiv0.RegistryExtensions) (*apiv0.ServerRecord, error)
	// UpdateLatestFlag updates the is_latest flag for a specific server record
	UpdateLatestFlag(ctx context.Context, id string, isLatest bool) error
	// UpdateServer updates an existing server record with new server details
	UpdateServer(ctx context.Context, id string, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}) (*apiv0.ServerRecord, error)
	// ImportSeed imports initial data from a seed file
	ImportSeed(ctx context.Context, seedFilePath string) error
	// Close closes the database connection
	Close() error
}

// ConnectionType represents the type of database connection
type ConnectionType string

const (
	// ConnectionTypeMemory represents an in-memory database connection
	ConnectionTypeMemory ConnectionType = "memory"
	// ConnectionTypePostgreSQL represents a PostgreSQL database connection
	ConnectionTypePostgreSQL ConnectionType = "postgresql"
)

// ConnectionInfo provides information about the database connection
type ConnectionInfo struct {
	// Type indicates the type of database connection
	Type ConnectionType
	// IsConnected indicates whether the database is currently connected
	IsConnected bool
	// Raw provides access to the underlying connection object, which will vary by implementation
	// For PostgreSQL, this will be *pgx.Conn
	// For MemoryDB, this will be map[string]*model.MCPRegistry
	Raw any
}
