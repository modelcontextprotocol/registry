package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1 "github.com/modelcontextprotocol/registry/pkg/api/v1"
	"github.com/modelcontextprotocol/registry/pkg/model"
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

// List retrieves ServerRecord entries with optional filtering and pagination
func (db *PostgreSQL) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*apiv1.ServerRecord, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause for server_extensions filtering
	whereClause := "WHERE se.is_latest = true"
	args := []any{}
	argIndex := 1

	// Add filters
	for k, v := range filter {
		switch k {
		case "name":
			whereClause += fmt.Sprintf(" AND s.name = $%d", argIndex)
			args = append(args, v)
			argIndex++
		case "version":
			whereClause += fmt.Sprintf(" AND s.version = $%d", argIndex)
			args = append(args, v)
			argIndex++
		case "status":
			whereClause += fmt.Sprintf(" AND s.status = $%d", argIndex)
			args = append(args, v)
			argIndex++
		case "remote_url":
			whereClause += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM jsonb_array_elements(CASE WHEN jsonb_typeof(s.remotes) = 'array' THEN s.remotes ELSE '[]'::jsonb END) AS remote WHERE remote->>'url' = $%d)", argIndex)
			args = append(args, v)
			argIndex++
		}
	}

	// Add cursor pagination using registry metadata ID
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}
		whereClause += fmt.Sprintf(" AND se.id > $%d", argIndex)
		args = append(args, cursor)
		argIndex++
	}

	// Build JOIN query between servers and server_extensions
	query := fmt.Sprintf(`
		SELECT 
			s.name, s.description, s.status, s.repository, s.version, s.packages, s.remotes,
			se.id, se.published_at, se.updated_at, se.is_latest, se.release_date, se.publisher_extensions
		FROM servers s
		JOIN server_extensions se ON s.id = se.server_id
		%s
		ORDER BY se.id
		LIMIT $%d
	`, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers with extensions: %w", err)
	}
	defer rows.Close()

	var results []*apiv1.ServerRecord
	for rows.Next() {
		var record apiv1.ServerRecord
		var repositoryJSON, packagesJSON, remotesJSON, publisherExtensionsJSON []byte
		var publishedAt, updatedAt, releaseDate time.Time

		err := rows.Scan(
			// Server fields
			&record.Server.Name,
			&record.Server.Description,
			&record.Server.Status,
			&repositoryJSON,
			&record.Server.VersionDetail.Version,
			&packagesJSON,
			&remotesJSON,
			// Registry metadata fields
			&record.XIOModelContextProtocolRegistry.ID,
			&publishedAt,
			&updatedAt,
			&record.XIOModelContextProtocolRegistry.IsLatest,
			&releaseDate,
			&publisherExtensionsJSON,
		)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server record row: %w", err)
		}

		// Parse JSON fields
		if err := parseJSONFields(&record, repositoryJSON, packagesJSON, remotesJSON, publisherExtensionsJSON); err != nil {
			return nil, "", err
		}

		// Set registry metadata timestamps
		record.XIOModelContextProtocolRegistry.PublishedAt = publishedAt
		record.XIOModelContextProtocolRegistry.UpdatedAt = updatedAt
		record.XIOModelContextProtocolRegistry.ReleaseDate = releaseDate.Format(time.RFC3339)

		results = append(results, &record)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor using registry metadata ID
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		nextCursor = results[len(results)-1].XIOModelContextProtocolRegistry.ID
	}

	return results, nextCursor, nil
}

// parseJSONFields parses JSON fields for a server record
func parseJSONFields(record *apiv1.ServerRecord, repositoryJSON, packagesJSON, remotesJSON, publisherExtensionsJSON []byte) error {
	if len(repositoryJSON) > 0 {
		if err := json.Unmarshal(repositoryJSON, &record.Server.Repository); err != nil {
			return fmt.Errorf("failed to unmarshal repository: %w", err)
		}
	}

	if len(packagesJSON) > 0 {
		if err := json.Unmarshal(packagesJSON, &record.Server.Packages); err != nil {
			return fmt.Errorf("failed to unmarshal packages: %w", err)
		}
	}

	if len(remotesJSON) > 0 {
		if err := json.Unmarshal(remotesJSON, &record.Server.Remotes); err != nil {
			return fmt.Errorf("failed to unmarshal remotes: %w", err)
		}
	}

	if len(publisherExtensionsJSON) > 0 {
		if err := json.Unmarshal(publisherExtensionsJSON, &record.XPublisher); err != nil {
			return fmt.Errorf("failed to unmarshal publisher extensions: %w", err)
		}
	} else {
		record.XPublisher = make(map[string]interface{})
	}

	return nil
}

// GetByID retrieves a single ServerRecord by its registry metadata ID
func (db *PostgreSQL) GetByID(ctx context.Context, id string) (*apiv1.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT 
			s.name, s.description, s.status, s.repository, s.version, s.packages, s.remotes,
			se.id, se.published_at, se.updated_at, se.is_latest, se.release_date, se.publisher_extensions
		FROM servers s
		JOIN server_extensions se ON s.id = se.server_id
		WHERE se.id = $1
	`

	var record apiv1.ServerRecord
	var repositoryJSON, packagesJSON, remotesJSON, publisherExtensionsJSON []byte
	var publishedAt, updatedAt, releaseDate time.Time

	err := db.conn.QueryRow(ctx, query, id).Scan(
		// Server fields
		&record.Server.Name,
		&record.Server.Description,
		&record.Server.Status,
		&repositoryJSON,
		&record.Server.VersionDetail.Version,
		&packagesJSON,
		&remotesJSON,
		// Registry metadata fields
		&record.XIOModelContextProtocolRegistry.ID,
		&publishedAt,
		&updatedAt,
		&record.XIOModelContextProtocolRegistry.IsLatest,
		&releaseDate,
		&publisherExtensionsJSON,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server record by ID: %w", err)
	}

	// Parse JSON fields
	if len(repositoryJSON) > 0 {
		if err := json.Unmarshal(repositoryJSON, &record.Server.Repository); err != nil {
			return nil, fmt.Errorf("failed to unmarshal repository: %w", err)
		}
	}

	if len(packagesJSON) > 0 {
		if err := json.Unmarshal(packagesJSON, &record.Server.Packages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal packages: %w", err)
		}
	}

	if len(remotesJSON) > 0 {
		if err := json.Unmarshal(remotesJSON, &record.Server.Remotes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal remotes: %w", err)
		}
	}

	// Parse publisher extensions
	if len(publisherExtensionsJSON) > 0 {
		if err := json.Unmarshal(publisherExtensionsJSON, &record.XPublisher); err != nil {
			return nil, fmt.Errorf("failed to unmarshal publisher extensions: %w", err)
		}
	} else {
		record.XPublisher = make(map[string]interface{})
	}

	// Set registry metadata timestamps
	record.XIOModelContextProtocolRegistry.PublishedAt = publishedAt
	record.XIOModelContextProtocolRegistry.UpdatedAt = updatedAt
	record.XIOModelContextProtocolRegistry.ReleaseDate = releaseDate.Format(time.RFC3339)

	return &record, nil
}

// Publish adds a new server to the database with separated server.json and extensions
func (db *PostgreSQL) Publish(ctx context.Context, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}, registryMetadata apiv1.RegistryExtensions) (*apiv1.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			log.Printf("Failed to rollback transaction: %v", err)
		}
	}()

	// Prepare JSON data for server table
	repositoryJSON, err := json.Marshal(serverDetail.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal repository: %w", err)
	}

	packagesJSON, err := json.Marshal(serverDetail.Packages)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal packages: %w", err)
	}

	remotesJSON, err := json.Marshal(serverDetail.Remotes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal remotes: %w", err)
	}

	publisherExtensionsJSON, err := json.Marshal(publisherExtensions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal publisher extensions: %w", err)
	}

	// Use the same ID for both server and server_extensions records (1:1 relationship)
	serverID := registryMetadata.ID

	// Insert new server record
	insertServerQuery := `
		INSERT INTO servers (id, name, description, status, repository, version, packages, remotes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = tx.Exec(ctx, insertServerQuery,
		serverID,
		serverDetail.Name,
		serverDetail.Description,
		serverDetail.Status,
		repositoryJSON,
		serverDetail.VersionDetail.Version,
		packagesJSON,
		remotesJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	// Insert server extensions record
	insertExtensionsQuery := `
		INSERT INTO server_extensions (id, server_id, published_at, updated_at, is_latest, release_date, publisher_extensions)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = tx.Exec(ctx, insertExtensionsQuery,
		registryMetadata.ID,
		serverID,
		registryMetadata.PublishedAt,
		registryMetadata.UpdatedAt,
		registryMetadata.IsLatest,
		registryMetadata.PublishedAt, // release_date
		publisherExtensionsJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert server extensions: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Create and return the ServerRecord
	record := &apiv1.ServerRecord{
		Server:          serverDetail,
		XIOModelContextProtocolRegistry:  registryMetadata,
		XPublisher: publisherExtensions,
	}

	return record, nil
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
		// Convert record to the format expected by Publish
		serverDetail := record.Server
		publisherExtensions := record.XPublisher

		// Use the existing Publish logic but with specific ID from seed data
		if err := db.publishWithTransaction(ctx, tx, serverDetail, publisherExtensions, &record.XIOModelContextProtocolRegistry); err != nil {
			return fmt.Errorf("failed to import server %s: %w", serverDetail.Name, err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit seed import transaction: %w", err)
	}

	return nil
}

// publishWithTransaction handles publishing within an existing transaction, optionally with predefined metadata
func (db *PostgreSQL) publishWithTransaction(ctx context.Context, tx pgx.Tx, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}, existingMetadata *apiv1.RegistryExtensions) error {
	// Use the same ID for both server and server_extensions (1:1 relationship)
	var id string
	if existingMetadata != nil && existingMetadata.ID != "" {
		// Use predefined ID from seed data
		id = existingMetadata.ID
	} else {
		// This shouldn't happen as service layer should always provide an ID
		// But keeping as fallback for safety
		id = uuid.New().String()
	}

	// Marshal packages and remotes to JSONB
	packagesJSON, err := json.Marshal(serverDetail.Packages)
	if err != nil {
		return fmt.Errorf("failed to marshal packages: %w", err)
	}

	remotesJSON, err := json.Marshal(serverDetail.Remotes)
	if err != nil {
		return fmt.Errorf("failed to marshal remotes: %w", err)
	}

	repositoryJSON, err := json.Marshal(serverDetail.Repository)
	if err != nil {
		return fmt.Errorf("failed to marshal repository: %w", err)
	}

	publisherExtensionsJSON, err := json.Marshal(publisherExtensions)
	if err != nil {
		return fmt.Errorf("failed to marshal publisher extensions: %w", err)
	}

	// Insert or update server record
	serverQuery := `
		INSERT INTO servers (id, name, description, status, repository, version, packages, remotes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
		ON CONFLICT (name, version) 
		DO UPDATE SET
			description = EXCLUDED.description,
			status = EXCLUDED.status,
			repository = EXCLUDED.repository,
			packages = EXCLUDED.packages,
			remotes = EXCLUDED.remotes,
			updated_at = NOW()
		RETURNING id`

	var returnedServerID string
	err = tx.QueryRow(ctx, serverQuery,
		id,
		serverDetail.Name,
		serverDetail.Description,
		string(serverDetail.Status),
		repositoryJSON,
		serverDetail.VersionDetail.Version,
		packagesJSON,
		remotesJSON,
	).Scan(&returnedServerID)
	if err != nil {
		return fmt.Errorf("failed to insert/update server: %w", err)
	}

	// Insert or update server extensions
	extensionQuery := `
		INSERT INTO server_extensions (id, server_id, published_at, updated_at, is_latest, release_date, publisher_extensions)
		VALUES ($1, $2, $3, NOW(), true, $4, $5)
		ON CONFLICT (server_id)
		DO UPDATE SET
			updated_at = NOW(),
			is_latest = true,
			release_date = EXCLUDED.release_date,
			publisher_extensions = EXCLUDED.publisher_extensions`

	var publishedAt, releaseDate string
	if existingMetadata != nil {
		publishedAt = existingMetadata.PublishedAt.Format(time.RFC3339)
		releaseDate = existingMetadata.ReleaseDate
	} else {
		now := time.Now().Format(time.RFC3339)
		publishedAt = now
		releaseDate = now
	}

	_, err = tx.Exec(ctx, extensionQuery,
		id,
		returnedServerID,
		publishedAt,
		releaseDate,
		publisherExtensionsJSON,
	)
	if err != nil {
		return fmt.Errorf("failed to insert/update server extensions: %w", err)
	}

	return nil
}

// UpdateLatestFlag updates the is_latest flag for a specific server record
func (db *PostgreSQL) UpdateLatestFlag(ctx context.Context, id string, isLatest bool) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	query := `
		UPDATE server_extensions 
		SET is_latest = $1, updated_at = $2
		WHERE id = $3
	`

	result, err := db.conn.Exec(ctx, query, isLatest, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to update latest flag: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, id string, serverDetail model.ServerJSON, publisherExtensions map[string]interface{}) (*apiv1.ServerRecord, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// First check if the server exists
	_, err := db.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Start a transaction for atomic updates
	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			log.Printf("Failed to rollback transaction: %v", rollbackErr)
		}
	}()

	// Prepare JSON data
	repositoryJSON, err := json.Marshal(serverDetail.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal repository: %w", err)
	}

	packagesJSON, err := json.Marshal(serverDetail.Packages)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal packages: %w", err)
	}

	remotesJSON, err := json.Marshal(serverDetail.Remotes)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal remotes: %w", err)
	}

	publisherExtensionsJSON, err := json.Marshal(publisherExtensions)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal publisher extensions: %w", err)
	}

	now := time.Now()

	// Update servers table
	serverQuery := `
		UPDATE servers 
		SET name = $1, description = $2, status = $3, repository = $4, 
		    version = $5, packages = $6, remotes = $7, updated_at = $8
		WHERE id = $9
	`

	_, err = tx.Exec(ctx, serverQuery,
		serverDetail.Name,
		serverDetail.Description,
		serverDetail.Status,
		repositoryJSON,
		serverDetail.VersionDetail.Version,
		packagesJSON,
		remotesJSON,
		now,
		id,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	// Update server_extensions table
	extQuery := `
		UPDATE server_extensions 
		SET updated_at = $1, publisher_extensions = $2
		WHERE server_id = $3
	`

	_, err = tx.Exec(ctx, extQuery, now, publisherExtensionsJSON, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update server extensions: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Return the updated record
	return db.GetByID(ctx, id)
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
