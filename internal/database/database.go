package database

import (
	"context"
	"errors"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
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

// ServerFilter defines filtering options for server queries
type ServerFilter struct {
	Name          *string    // for finding versions of same server
	RemoteURL     *string    // for duplicate URL detection
	UpdatedSince  *time.Time // for incremental sync filtering
	SubstringName *string    // for substring search on name
	Version       *string    // for exact version matching
	IsLatest      *bool      // for filtering latest versions only
}

// Database defines the interface for database operations
type Database interface {
	// Retrieve server entries with optional filtering
	List(ctx context.Context, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// Retrieve a single server by its version ID
	GetByVersionID(ctx context.Context, versionID string) (*apiv0.ServerResponse, error)
	// Retrieve latest version of a server by server ID
	GetByServerID(ctx context.Context, serverID string) (*apiv0.ServerResponse, error)
	// Retrieve specific version of a server by server ID and version
	GetByServerIDAndVersion(ctx context.Context, serverID string, version string) (*apiv0.ServerResponse, error)
	// Retrieve all versions of a server by server ID
	GetAllVersionsByServerID(ctx context.Context, serverID string) ([]*apiv0.ServerResponse, error)
	// CreateServer adds a new server to the database (input ServerJSON with metadata, output ServerResponse)
	CreateServer(ctx context.Context, server *apiv0.ServerJSON, serverID, versionID string, isLatest bool) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server record (input ServerJSON, output ServerResponse)
	UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// UpdateServerStatus updates only the status field of a server record
	UpdateServerStatus(ctx context.Context, versionID string, status string) (*apiv0.ServerResponse, error)
	// UpdateIsLatest updates only the isLatest flag of a server record
	UpdateIsLatest(ctx context.Context, versionID string, isLatest bool) (*apiv0.ServerResponse, error)
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
