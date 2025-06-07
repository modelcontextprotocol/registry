package database

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/registry/internal/model"
)

// Common database errors
var (
	ErrNotFound       = errors.New("record not found")
	ErrAlreadyExists  = errors.New("record already exists")
	ErrInvalidInput   = errors.New("invalid input")
	ErrDatabase       = errors.New("database error")
	ErrInvalidVersion = errors.New("invalid version: cannot publish older version after newer version")
)

// Database defines the interface for database operations on MCPRegistry entries
type Database interface {
	// List retrieves all MCPRegistry entries with optional filtering
	List(ctx context.Context, filter map[string]interface{}, cursor string, limit int) ([]*model.Server, string, error)
	// GetByID retrieves a single ServerDetail by it's ID
	GetByID(ctx context.Context, id string) (*model.ServerDetail, error)
	// Publish adds a new ServerDetail to the database
	Publish(ctx context.Context, serverDetail *model.ServerDetail) error
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
	// ConnectionTypeMongoDB represents a MongoDB database connection
	ConnectionTypeMongoDB ConnectionType = "mongodb"
)

// ConnectionInfo provides information about the database connection
type ConnectionInfo struct {
	// Type indicates the type of database connection
	Type ConnectionType
	// IsConnected indicates whether the database is currently connected
	IsConnected bool
	// Raw provides access to the underlying connection object, which will vary by implementation
	// For MongoDB, this will be *mongo.Client
	// For MemoryDB, this will be map[string]*model.MCPRegistry
	Raw any
}
