package database

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
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
	List(ctx context.Context, tx pgx.Tx, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerJSON, string, error)
	// Retrieve a single server by its version ID
	GetByVersionID(ctx context.Context, tx pgx.Tx, versionID string) (*apiv0.ServerJSON, error)
	// Retrieve latest version of a server by server ID
	GetByServerID(ctx context.Context, tx pgx.Tx, serverID string) (*apiv0.ServerJSON, error)
	// Retrieve specific version of a server by server ID and version
	GetByServerIDAndVersion(ctx context.Context, tx pgx.Tx, serverID string, version string) (*apiv0.ServerJSON, error)
	// Retrieve all versions of a server by server ID
	GetAllVersionsByServerID(ctx context.Context, tx pgx.Tx, serverID string) ([]*apiv0.ServerJSON, error)
	// CreateServer inserts a new server version
	CreateServer(ctx context.Context, tx pgx.Tx, newServer *apiv0.ServerJSON) (*apiv0.ServerJSON, error)
	// UpdateServer updates an existing server record
	UpdateServer(ctx context.Context, tx pgx.Tx, id string, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error)
	// AcquirePublishLock acquires an exclusive advisory lock for publishing a server
	// This prevents race conditions when multiple versions are published concurrently
	AcquirePublishLock(ctx context.Context, tx pgx.Tx, serverName string) error
	// InTransaction executes a function within a database transaction
	InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error
	// Close closes the database connection
	Close() error
}

// InTransactionT is a generic helper that wraps InTransaction for functions returning a value
// This exists because Go does not support generic methods on interfaces - only the Database interface
// method InTransaction (without generics) can exist, so we provide this generic wrapper function.
// This is a common pattern in Go for working around this language limitation.
func InTransactionT[T any](ctx context.Context, db Database, fn func(ctx context.Context, tx pgx.Tx) (T, error)) (T, error) {
	var result T
	var fnErr error

	err := db.InTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		result, fnErr = fn(txCtx, tx)
		return fnErr
	})

	if err != nil {
		var zero T
		return zero, err
	}

	return result, nil
}
