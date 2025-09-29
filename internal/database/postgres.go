package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	pool *pgxpool.Pool
}

// Executor is an interface for executing queries (satisfied by both pgx.Tx and pgxpool.Pool)
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// getExecutor returns the appropriate executor (transaction or pool)
func (db *PostgreSQL) getExecutor(tx pgx.Tx) Executor {
	if tx != nil {
		return tx
	}
	return db.pool
}

// NewPostgreSQL creates a new instance of the PostgreSQL database
func NewPostgreSQL(ctx context.Context, connectionURI string) (*PostgreSQL, error) {
	// Parse connection config for pool settings
	config, err := pgxpool.ParseConfig(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL config: %w", err)
	}

	// Configure pool for stability-focused defaults
	config.MaxConns = 30                      // Handle good concurrent load
	config.MinConns = 5                       // Keep connections warm for fast response
	config.MaxConnIdleTime = 30 * time.Minute // Keep connections available for bursts
	config.MaxConnLifetime = 2 * time.Hour    // Refresh connections regularly for stability

	// Create connection pool with configured settings
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	// Test the connection
	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Run migrations using a single connection from the pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer conn.Release()

	migrator := NewMigrator(conn.Conn())
	if err := migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	return &PostgreSQL{
		pool: pool,
	}, nil
}

func (db *PostgreSQL) ListServers(
	ctx context.Context,
	tx pgx.Tx,
	filter *ServerFilter,
	cursor string,
	limit int,
) ([]*apiv0.ServerResponse, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause for filtering using dedicated columns
	var whereConditions []string
	args := []any{}
	argIndex := 1

	// Add filters using dedicated columns for better performance
	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("version = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("is_latest = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
	}

	// Add cursor pagination using compound serverName:version cursor
	if cursor != "" {
		// Parse cursor format: "serverName:version"
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) == 2 {
			cursorServerName := parts[0]
			cursorVersion := parts[1]

			// Use compound condition: (server_name > cursor_name) OR (server_name = cursor_name AND version > cursor_version)
			whereConditions = append(whereConditions, fmt.Sprintf("(server_name > $%d OR (server_name = $%d AND version > $%d))", argIndex, argIndex+1, argIndex+2))
			args = append(args, cursorServerName, cursorServerName, cursorVersion)
			argIndex += 3
		} else {
			// Fallback for malformed cursor - treat as server name only for backwards compatibility
			whereConditions = append(whereConditions, fmt.Sprintf("server_name > $%d", argIndex))
			args = append(args, cursor)
			argIndex++
		}
	}

	// Build the WHERE clause
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Query servers table with hybrid column/JSON data
	query := fmt.Sprintf(`
        SELECT server_name, version, status, published_at, updated_at, is_latest, value
        FROM servers
        %s
        ORDER BY server_name, version
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var serverName, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte

		err := rows.Scan(&serverName, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the ServerJSON from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Build ServerResponse with separated metadata
		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor using compound serverName:version format
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		nextCursor = lastResult.Server.Name + ":" + lastResult.Server.Version
	}

	return results, nextCursor, nil
}

// GetServerByName retrieves the latest version of a server by server name
func (db *PostgreSQL) GetServerByName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND is_latest = true
		ORDER BY published_at DESC
		LIMIT 1
	`

	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, serverName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name: %w", err)
	}

	// Parse the ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Build ServerResponse with separated metadata
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version
func (db *PostgreSQL) GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND version = $2
		LIMIT 1
	`

	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, serverName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name and version: %w", err)
	}

	// Parse the ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Build ServerResponse with separated metadata
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (db *PostgreSQL) GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1
		ORDER BY published_at DESC
	`

	rows, err := db.getExecutor(tx).Query(ctx, query, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte

		err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the ServerJSON from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Build ServerResponse with separated metadata
		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
}

// CreateServer inserts a new server version with official metadata
func (db *PostgreSQL) CreateServer(ctx context.Context, tx pgx.Tx, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate inputs
	if serverJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("serverJSON and officialMeta are required")
	}

	if serverJSON.Name == "" || serverJSON.Version == "" {
		return nil, fmt.Errorf("server name and version are required")
	}

	// Marshal the ServerJSON to JSONB
	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Insert the new server version using composite primary key
	insertQuery := `
		INSERT INTO servers (server_name, version, status, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = db.getExecutor(tx).Exec(ctx, insertQuery,
		serverJSON.Name,
		serverJSON.Version,
		string(officialMeta.Status),
		officialMeta.PublishedAt,
		officialMeta.UpdatedAt,
		officialMeta.IsLatest,
		valueJSON,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	// Return the complete ServerResponse
	serverResponse := &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: officialMeta,
		},
	}

	return serverResponse, nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, tx pgx.Tx, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate inputs
	if serverJSON == nil {
		return nil, fmt.Errorf("serverJSON is required")
	}

	// Ensure the serverJSON matches the provided serverName and version
	if serverJSON.Name != serverName || serverJSON.Version != version {
		return nil, fmt.Errorf("%w: server name and version in JSON must match parameters", ErrInvalidInput)
	}

	// Marshal updated ServerJSON
	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	// Update only the JSON data (keep existing metadata columns)
	query := `
		UPDATE servers
		SET value = $1, updated_at = NOW()
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, published_at, updated_at, is_latest
	`

	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool

	err = db.getExecutor(tx).QueryRow(ctx, query, valueJSON, serverName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	// Return the updated ServerResponse
	serverResponse := &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// SetServerStatus updates the status of a specific server version
func (db *PostgreSQL) SetServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Update the status column
	query := `
		UPDATE servers
		SET status = $1, updated_at = NOW()
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, value, published_at, updated_at, is_latest
	`

	var name, vers, currentStatus string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, status, serverName, version).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server status: %w", err)
	}

	// Unmarshal the JSON data
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Return the updated ServerResponse
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(currentStatus),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// InTransaction executes a function within a database transaction
func (db *PostgreSQL) InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:contextcheck // Intentionally using separate context for rollback to ensure cleanup even if request is cancelled
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			log.Printf("failed to rollback transaction: %v", rbErr)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// AcquirePublishLock acquires an exclusive advisory lock for publishing a server
// This prevents race conditions when multiple versions are published concurrently
// Using pg_advisory_xact_lock which auto-releases on transaction end
func (db *PostgreSQL) AcquirePublishLock(ctx context.Context, tx pgx.Tx, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	lockID := hashServerName(serverName)

	if _, err := db.getExecutor(tx).Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID); err != nil {
		return fmt.Errorf("failed to acquire publish lock: %w", err)
	}

	return nil
}

// hashServerName creates a consistent hash of the server name for advisory locking
// We use FNV-1a hash and mask to 63 bits to fit in PostgreSQL's bigint range
func hashServerName(name string) int64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	for i := 0; i < len(name); i++ {
		hash ^= uint64(name[i])
		hash *= prime64
	}
	//nolint:gosec // Intentional conversion with masking to 63 bits
	return int64(hash & 0x7FFFFFFFFFFFFFFF)
}

// GetCurrentLatestVersion retrieves the current latest version of a server by server name
func (db *PostgreSQL) GetCurrentLatestVersion(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `
		SELECT server_name, version, status, value, published_at, updated_at, is_latest
		FROM servers
		WHERE server_name = $1 AND is_latest = true
	`

	row := executor.QueryRow(ctx, query, serverName)

	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var jsonValue []byte

	err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan server row: %w", err)
	}

	// Parse the JSON value to get the server details
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(jsonValue, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Build ServerResponse with separated metadata
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// CountServerVersions counts the number of versions for a server
func (db *PostgreSQL) CountServerVersions(ctx context.Context, tx pgx.Tx, serverName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `SELECT COUNT(*) FROM servers WHERE server_name = $1`

	var count int
	err := executor.QueryRow(ctx, query, serverName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count server versions: %w", err)
	}

	return count, nil
}

// CheckVersionExists checks if a specific version exists for a server
func (db *PostgreSQL) CheckVersionExists(ctx context.Context, tx pgx.Tx, serverName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `SELECT EXISTS(SELECT 1 FROM servers WHERE server_name = $1 AND version = $2)`

	var exists bool
	err := executor.QueryRow(ctx, query, serverName, version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check version existence: %w", err)
	}

	return exists, nil
}

// UnmarkAsLatest marks the current latest version of a server as no longer latest
func (db *PostgreSQL) UnmarkAsLatest(ctx context.Context, tx pgx.Tx, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `UPDATE servers SET is_latest = false WHERE server_name = $1 AND is_latest = true`

	_, err := executor.Exec(ctx, query, serverName)
	if err != nil {
		return fmt.Errorf("failed to unmark latest version: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
