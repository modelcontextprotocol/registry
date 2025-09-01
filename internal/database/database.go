package database

import (
	"context"
	"errors"

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
	Name      *string // for finding versions of same server
	RemoteURL *string // for duplicate URL detection
}

// Database defines the interface for database operations
type Database interface {
	// Retrieve server entries with optional filtering
	List(ctx context.Context, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerJSON, string, error)
	// Retrieve a single server by its ID
	GetByID(ctx context.Context, id string) (*apiv0.ServerJSON, error)
	// CreateServer adds a new server to the database
	CreateServer(ctx context.Context, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error)
	// UpdateServer updates an existing server record
	UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error)
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
