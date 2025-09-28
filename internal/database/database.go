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
	// CreateServer inserts a new server version with official metadata
	CreateServer(ctx context.Context, tx pgx.Tx, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server record
	UpdateServer(ctx context.Context, tx pgx.Tx, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// SetServerStatus updates the status of a specific server version
	SetServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status string) (*apiv0.ServerResponse, error)
	// ListServers retrieve server entries with optional filtering
	ListServers(ctx context.Context, tx pgx.Tx, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// GetServerByName retrieve a single server by its name
	GetServerByName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error)
	// GetServerByNameAndVersion retrieve specific version of a server by server name and version
	GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*apiv0.ServerResponse, error)
	// GetAllVersionsByServerName retrieve all versions of a server by server name
	GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string) ([]*apiv0.ServerResponse, error)
	// GetCurrentLatestVersion retrieve the current latest version of a server by server name
	GetCurrentLatestVersion(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error)
	// CountServerVersions count the number of versions for a server
	CountServerVersions(ctx context.Context, tx pgx.Tx, serverName string) (int, error)
	// CheckVersionExists check if a specific version exists for a server
	CheckVersionExists(ctx context.Context, tx pgx.Tx, serverName, version string) (bool, error)
	// UnmarkAsLatest marks the current latest version of a server as no longer latest
	UnmarkAsLatest(ctx context.Context, tx pgx.Tx, serverName string) error
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
