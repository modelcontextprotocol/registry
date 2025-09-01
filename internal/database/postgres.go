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
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	conn *pgx.Conn
}

// NewPostgreSQL creates a new instance of the PostgreSQL database
func NewPostgreSQL(ctx context.Context, connectionURI string) (*PostgreSQL, error) {
	conn, err := pgx.Connect(ctx, connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Test the connection
	if err = conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Run migrations
	migrator := NewMigrator(conn)
	if err := migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	return &PostgreSQL{
		conn: conn,
	}, nil
}

// List retrieves ServerJSON entries with optional filtering and pagination
func (db *PostgreSQL) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*apiv0.ServerJSON, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause for filtering - always filter by is_latest = true by default
	var whereConditions []string
	whereConditions = append(whereConditions, "(value->'_meta'->'io.modelcontextprotocol.registry'->>'is_latest')::boolean = true")
	args := []any{}
	argIndex := 1

	// Add filters using JSON operators
	for k, v := range filter {
		switch k {
		case "name":
			whereConditions = append(whereConditions, fmt.Sprintf("value->>'name' = $%d", argIndex))
			args = append(args, v)
			argIndex++
		case "version":
			whereConditions = append(whereConditions, fmt.Sprintf("value->'version_detail'->>'version' = $%d", argIndex))
			args = append(args, v)
			argIndex++
		case "status":
			whereConditions = append(whereConditions, fmt.Sprintf("value->>'status' = $%d", argIndex))
			args = append(args, v)
			argIndex++
		case "remote_url":
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, v)
			argIndex++
		}
	}

	// Add cursor pagination using registry metadata ID
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("(value->'_meta'->'io.modelcontextprotocol.registry'->>'id') > $%d", argIndex))
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
		ORDER BY (value->'_meta'->'io.modelcontextprotocol.registry'->>'id')
		LIMIT $%d
	`, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.conn.Query(ctx, query, args...)
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

	// Determine next cursor using registry metadata ID
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		if lastResult.Meta != nil && lastResult.Meta.IOModelContextProtocolRegistry != nil {
			nextCursor = lastResult.Meta.IOModelContextProtocolRegistry.ID
		}
	}

	return results, nextCursor, nil
}

// GetByID retrieves a single ServerJSON by its registry metadata ID
func (db *PostgreSQL) GetByID(ctx context.Context, id string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT value
		FROM servers
		WHERE (value->'_meta'->'io.modelcontextprotocol.registry'->>'id') = $1
	`

	var valueJSON []byte

	err := db.conn.QueryRow(ctx, query, id).Scan(&valueJSON)

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

// Publish adds a new server to the simple servers table
func (db *PostgreSQL) Publish(ctx context.Context, serverDetail apiv0.ServerJSON, publisherExtensions map[string]interface{}, registryMetadata apiv0.RegistryExtensions) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Create complete server with metadata
	server := serverDetail // Copy the input

	// Initialize meta if not present
	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}

	// Set registry metadata
	server.Meta.IOModelContextProtocolRegistry = &registryMetadata

	// Set publisher extensions if provided
	if len(publisherExtensions) > 0 {
		server.Meta.Publisher = publisherExtensions
	}

	// Marshal the complete server to JSONB
	valueJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Insert into simple servers table
	query := `
		INSERT INTO servers (id, value)
		VALUES ($1, $2)
	`

	_, err = db.conn.Exec(ctx, query, registryMetadata.ID, valueJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	return &server, nil
}

// ImportSeed imports initial data from a seed file into PostgreSQL
func (db *PostgreSQL) ImportSeed(ctx context.Context, seedFilePath string) error {
	// Read seed data using the shared ReadSeedFile function
	seedData, err := ReadSeedFile(ctx, seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	// Start a transaction for batch import
	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			log.Printf("Failed to rollback transaction: %v", rollbackErr)
		}
	}()

	// Import each server
	for _, record := range seedData {
		// Marshal the server to JSONB
		valueJSON, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("failed to marshal server JSON for import: %w", err)
		}

		// Get registry ID from metadata
		var registryID string
		if record.Meta != nil && record.Meta.IOModelContextProtocolRegistry != nil {
			registryID = record.Meta.IOModelContextProtocolRegistry.ID
		} else {
			registryID = uuid.New().String() // Generate ID if not present
		}

		// Insert into simple servers table
		_, err = tx.Exec(ctx, "INSERT INTO servers (id, value) VALUES ($1, $2)", registryID, valueJSON)
		if err != nil {
			return fmt.Errorf("failed to import server %s: %w", record.Name, err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit seed import transaction: %w", err)
	}

	return nil
}

// UpdateLatestFlag updates the is_latest flag for a specific server record
func (db *PostgreSQL) UpdateLatestFlag(ctx context.Context, id string, isLatest bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	query := `
		UPDATE servers 
		SET value = jsonb_set(
			jsonb_set(
				value, 
				'{_meta,io.modelcontextprotocol.registry,is_latest}', 
				$1::jsonb
			),
			'{_meta,io.modelcontextprotocol.registry,updated_at}',
			$2::jsonb
		)
		WHERE (value->'_meta'->'io.modelcontextprotocol.registry'->>'id') = $3
	`

	result, err := db.conn.Exec(ctx, query,
		fmt.Sprintf("%t", isLatest),
		fmt.Sprintf("\"%s\"", time.Now().Format(time.RFC3339)),
		id)
	if err != nil {
		return fmt.Errorf("failed to update latest flag: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, id string, serverDetail apiv0.ServerJSON, publisherExtensions map[string]interface{}) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the existing server to preserve registry metadata
	existingServer, err := db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update the server details while preserving registry metadata
	updatedServer := serverDetail
	if updatedServer.Meta == nil {
		updatedServer.Meta = &apiv0.ServerMeta{}
	}

	// Preserve existing registry metadata but update timestamp
	if existingServer.Meta != nil && existingServer.Meta.IOModelContextProtocolRegistry != nil {
		updatedServer.Meta.IOModelContextProtocolRegistry = existingServer.Meta.IOModelContextProtocolRegistry
		updatedServer.Meta.IOModelContextProtocolRegistry.UpdatedAt = time.Now()
	}

	// Update publisher extensions if provided
	if len(publisherExtensions) > 0 {
		updatedServer.Meta.Publisher = publisherExtensions
	}

	// Marshal updated server
	valueJSON, err := json.Marshal(updatedServer)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	// Update the complete server record in simple table
	query := `
		UPDATE servers 
		SET value = $1
		WHERE (value->'_meta'->'io.modelcontextprotocol.registry'->>'id') = $2
	`

	result, err := db.conn.Exec(ctx, query, valueJSON, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	if result.RowsAffected() == 0 {
		return nil, ErrNotFound
	}

	return &updatedServer, nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	return db.conn.Close(context.Background())
}

// Connection returns information about the database connection
func (db *PostgreSQL) Connection() *ConnectionInfo {
	isConnected := false
	if db.conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := db.conn.Ping(ctx)
		isConnected = (err == nil)
	}

	return &ConnectionInfo{
		Type:        ConnectionTypePostgreSQL,
		IsConnected: isConnected,
		Raw:         db.conn,
	}
}
