package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
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

//nolint:cyclop // Database filtering logic is inherently complex but clear
func (db *PostgreSQL) List(
	ctx context.Context,
	tx pgx.Tx,
	filter *ServerFilter,
	cursor string,
	limit int,
) ([]*apiv0.ServerJSON, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause for filtering
	var whereConditions []string
	args := []any{}
	argIndex := 1

	// Add filters using JSON operators
	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("value->>'name' = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("(value->'_meta'->'io.modelcontextprotocol.registry/official'->>'updatedAt')::timestamp > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("value->>'name' ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("(value->'version_detail'->>'version') = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("(value->'_meta'->'io.modelcontextprotocol.registry/official'->>'isLatest')::boolean = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
	}

	// Add cursor pagination using primary key version_id
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("version_id > $%d", argIndex))
		args = append(args, cursor)
		argIndex++
	}

	// Build the WHERE clause
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Simple query on servers table
	query := fmt.Sprintf(`
        SELECT value
        FROM servers
        %s
        ORDER BY version_id
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerJSON
	for rows.Next() {
		var valueJSON []byte

		err := rows.Scan(&valueJSON)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the complete ServerJSON from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		results = append(results, &serverJSON)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor using registry metadata VersionID
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		if lastResult.Meta != nil && lastResult.Meta.Official != nil {
			nextCursor = lastResult.Meta.Official.VersionID
		}
	}

	return results, nextCursor, nil
}

func (db *PostgreSQL) GetByVersionID(ctx context.Context, tx pgx.Tx, versionID string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT value
		FROM servers
		WHERE version_id = $1
	`

	var valueJSON []byte
	err := db.getExecutor(tx).QueryRow(ctx, query, versionID).Scan(&valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by ID: %w", err)
	}

	// Parse the complete ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	return &serverJSON, nil
}

// GetByServerID retrieves the latest version of a server by server ID
func (db *PostgreSQL) GetByServerID(ctx context.Context, tx pgx.Tx, serverID string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT value
		FROM servers
		WHERE (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'serverId') = $1 AND (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'isLatest')::boolean = true
		ORDER BY (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'publishedAt')::timestamp DESC
		LIMIT 1
	`

	var valueJSON []byte
	err := db.getExecutor(tx).QueryRow(ctx, query, serverID).Scan(&valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by server ID: %w", err)
	}

	// Parse the complete ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	return &serverJSON, nil
}

// GetByServerIDAndVersion retrieves a specific version of a server by server ID and version
func (db *PostgreSQL) GetByServerIDAndVersion(ctx context.Context, tx pgx.Tx, serverID string, version string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT value
		FROM servers
		WHERE (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'serverId') = $1 AND value->>'version' = $2
		LIMIT 1
	`

	var valueJSON []byte
	err := db.getExecutor(tx).QueryRow(ctx, query, serverID, version).Scan(&valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by server ID and version: %w", err)
	}

	// Parse the complete ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	return &serverJSON, nil
}

// GetAllVersionsByServerID retrieves all versions of a server by server ID
func (db *PostgreSQL) GetAllVersionsByServerID(ctx context.Context, tx pgx.Tx, serverID string) ([]*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT value
		FROM servers
		WHERE (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'serverId') = $1
		ORDER BY (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'publishedAt')::timestamp DESC
	`

	rows, err := db.getExecutor(tx).Query(ctx, query, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerJSON
	for rows.Next() {
		var valueJSON []byte

		err := rows.Scan(&valueJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the complete ServerJSON from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		results = append(results, &serverJSON)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
}

// CreateServer inserts a new server version
func (db *PostgreSQL) CreateServer(ctx context.Context, tx pgx.Tx, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the IDs from the registry metadata
	if server.Meta == nil || server.Meta.Official == nil {
		return nil, fmt.Errorf("server must have registry metadata with ServerID and VersionID")
	}

	versionID := server.Meta.Official.VersionID
	if versionID == "" {
		return nil, fmt.Errorf("server must have VersionID in registry metadata")
	}

	// Marshal the complete server to JSONB
	valueJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Insert the new version
	insertQuery := `
		INSERT INTO servers (version_id, value)
		VALUES ($1, $2)
	`

	_, err = db.getExecutor(tx).Exec(ctx, insertQuery, versionID, valueJSON)

	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	return server, nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, tx pgx.Tx, id string, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate that meta structure exists and VersionID matches path
	if server.Meta == nil || server.Meta.Official == nil || server.Meta.Official.VersionID != id {
		return nil, fmt.Errorf("%w: io.modelcontextprotocol.registry/official.version_id must match path id (%s)", ErrInvalidInput, id)
	}

	// Marshal updated server
	valueJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	// Update the complete server record using version_id
	query := `
		UPDATE servers 
		SET value = $1
		WHERE version_id = $2
	`

	result, err := db.getExecutor(tx).Exec(ctx, query, valueJSON, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	if result.RowsAffected() == 0 {
		return nil, ErrNotFound
	}

	return server, nil
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

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
