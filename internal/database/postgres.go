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
	"github.com/modelcontextprotocol/registry/internal/model"
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

// List retrieves MCPRegistry entries with optional filtering and pagination
func (db *PostgreSQL) List(
	ctx context.Context,
	filter map[string]any,
	cursor string,
	limit int,
) ([]*model.Server, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause
	whereClause := "WHERE is_latest = true"
	args := []any{}
	argIndex := 1

	// Add filters
	for k, v := range filter {
		switch k {
		case "name":
			whereClause += fmt.Sprintf(" AND name = $%d", argIndex)
			args = append(args, v)
			argIndex++
		case "version":
			whereClause += fmt.Sprintf(" AND version = $%d", argIndex)
			args = append(args, v)
			argIndex++
		case "status":
			whereClause += fmt.Sprintf(" AND status = $%d", argIndex)
			args = append(args, v)
			argIndex++
		}
	}

	// Add cursor pagination
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}
		whereClause += fmt.Sprintf(" AND id > $%d", argIndex)
		args = append(args, cursor)
		argIndex++
	}

	// Build query
	query := fmt.Sprintf(`
		SELECT id, name, description, status, repository, version, release_date, is_latest
		FROM servers
		%s
		ORDER BY id
		LIMIT $%d
	`, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.conn.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*model.Server
	for rows.Next() {
		var server model.Server
		var repositoryJSON []byte
		var releaseDate time.Time

		err := rows.Scan(
			&server.ID,
			&server.Name,
			&server.Description,
			&server.Status,
			&repositoryJSON,
			&server.VersionDetail.Version,
			&releaseDate,
			&server.VersionDetail.IsLatest,
		)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse repository JSON
		if len(repositoryJSON) > 0 {
			if err := json.Unmarshal(repositoryJSON, &server.Repository); err != nil {
				return nil, "", fmt.Errorf("failed to unmarshal repository: %w", err)
			}
		}

		server.VersionDetail.ReleaseDate = releaseDate.Format(time.RFC3339)
		results = append(results, &server)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		nextCursor = results[len(results)-1].ID
	}

	return results, nextCursor, nil
}

// GetByID retrieves a single ServerDetail by its ID
func (db *PostgreSQL) GetByID(ctx context.Context, id string) (*model.ServerDetail, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT id, name, description, status, repository, version, release_date, is_latest, packages, remotes
		FROM servers
		WHERE id = $1
	`

	var serverDetail model.ServerDetail
	var repositoryJSON, packagesJSON, remotesJSON []byte
	var releaseDate time.Time

	err := db.conn.QueryRow(ctx, query, id).Scan(
		&serverDetail.ID,
		&serverDetail.Name,
		&serverDetail.Description,
		&serverDetail.Status,
		&repositoryJSON,
		&serverDetail.VersionDetail.Version,
		&releaseDate,
		&serverDetail.VersionDetail.IsLatest,
		&packagesJSON,
		&remotesJSON,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by ID: %w", err)
	}

	// Parse JSON fields
	if len(repositoryJSON) > 0 {
		if err := json.Unmarshal(repositoryJSON, &serverDetail.Repository); err != nil {
			return nil, fmt.Errorf("failed to unmarshal repository: %w", err)
		}
	}

	if len(packagesJSON) > 0 {
		if err := json.Unmarshal(packagesJSON, &serverDetail.Packages); err != nil {
			return nil, fmt.Errorf("failed to unmarshal packages: %w", err)
		}
	}

	if len(remotesJSON) > 0 {
		if err := json.Unmarshal(remotesJSON, &serverDetail.Remotes); err != nil {
			return nil, fmt.Errorf("failed to unmarshal remotes: %w", err)
		}
	}

	serverDetail.VersionDetail.ReleaseDate = releaseDate.Format(time.RFC3339)

	return &serverDetail, nil
}

// Publish adds a new ServerDetail to the database
func (db *PostgreSQL) Publish(ctx context.Context, serverDetail *model.ServerDetail) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(ctx); err != nil {
			log.Printf("Failed to rollback transaction: %v", err)
		}
	}()

	// Check if there's an existing latest version for this server
	var existingVersion string
	checkQuery := `
		SELECT version 
		FROM servers 
		WHERE name = $1 AND is_latest = true
	`
	err = tx.QueryRow(ctx, checkQuery, serverDetail.Name).Scan(&existingVersion)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("failed to check existing version: %w", err)
	}

	// Validate version ordering
	if existingVersion != "" && serverDetail.VersionDetail.Version <= existingVersion {
		return ErrInvalidVersion
	}

	// Prepare JSON data
	repositoryJSON, err := json.Marshal(serverDetail.Repository)
	if err != nil {
		return fmt.Errorf("failed to marshal repository: %w", err)
	}

	packagesJSON, err := json.Marshal(serverDetail.Packages)
	if err != nil {
		return fmt.Errorf("failed to marshal packages: %w", err)
	}

	remotesJSON, err := json.Marshal(serverDetail.Remotes)
	if err != nil {
		return fmt.Errorf("failed to marshal remotes: %w", err)
	}

	// Generate ID and set metadata
	if serverDetail.ID == "" {
		serverDetail.ID = uuid.New().String()
	}
	serverDetail.VersionDetail.IsLatest = true
	if serverDetail.VersionDetail.ReleaseDate == "" {
		serverDetail.VersionDetail.ReleaseDate = time.Now().Format(time.RFC3339)
	}

	releaseDate, err := time.Parse(time.RFC3339, serverDetail.VersionDetail.ReleaseDate)
	if err != nil {
		return fmt.Errorf("failed to parse release date: %w", err)
	}

	// Update existing latest version to not be latest
	if existingVersion != "" {
		updateQuery := `
			UPDATE servers 
			SET is_latest = false 
			WHERE name = $1 AND is_latest = true
		`
		_, err = tx.Exec(ctx, updateQuery, serverDetail.Name)
		if err != nil {
			return fmt.Errorf("failed to update existing latest version: %w", err)
		}
	}

	// Insert new server version
	insertQuery := `
		INSERT INTO servers (id, name, description, status, repository, version, release_date, is_latest, packages, remotes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = tx.Exec(ctx, insertQuery,
		serverDetail.ID,
		serverDetail.Name,
		serverDetail.Description,
		serverDetail.Status,
		repositoryJSON,
		serverDetail.VersionDetail.Version,
		releaseDate,
		serverDetail.VersionDetail.IsLatest,
		packagesJSON,
		remotesJSON,
	)

	if err != nil {
		return fmt.Errorf("failed to insert server: %w", err)
	}

	return tx.Commit(ctx)
}

// ImportSeed imports initial data from a seed file into PostgreSQL
func (db *PostgreSQL) ImportSeed(ctx context.Context, seedFilePath string) error {
	servers, err := ReadSeedFile(ctx, seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	log.Printf("Importing %d servers into PostgreSQL", len(servers))

	for i, server := range servers {
		if server.ID == "" || server.Name == "" {
			log.Printf("Skipping server %d: ID or Name is empty", i+1)
			continue
		}

		if server.VersionDetail.Version == "" {
			server.VersionDetail.Version = "0.0.1-seed"
			server.VersionDetail.ReleaseDate = time.Now().Format(time.RFC3339)
			server.VersionDetail.IsLatest = true
		}

		// Prepare JSON data
		repositoryJSON, err := json.Marshal(server.Repository)
		if err != nil {
			log.Printf("Error marshaling repository for server %s: %v", server.ID, err)
			continue
		}

		packagesJSON, err := json.Marshal(server.Packages)
		if err != nil {
			log.Printf("Error marshaling packages for server %s: %v", server.ID, err)
			continue
		}

		remotesJSON, err := json.Marshal(server.Remotes)
		if err != nil {
			log.Printf("Error marshaling remotes for server %s: %v", server.ID, err)
			continue
		}

		releaseDate, err := time.Parse(time.RFC3339, server.VersionDetail.ReleaseDate)
		if err != nil {
			log.Printf("Error parsing release date for server %s: %v", server.ID, err)
			releaseDate = time.Now()
		}

		// Use UPSERT (INSERT ... ON CONFLICT DO UPDATE)
		upsertQuery := `
			INSERT INTO servers (id, name, description, status, repository, version, release_date, is_latest, packages, remotes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
			ON CONFLICT (id) DO UPDATE SET
				name = EXCLUDED.name,
				description = EXCLUDED.description,
				status = EXCLUDED.status,
				repository = EXCLUDED.repository,
				version = EXCLUDED.version,
				release_date = EXCLUDED.release_date,
				is_latest = EXCLUDED.is_latest,
				packages = EXCLUDED.packages,
				remotes = EXCLUDED.remotes,
				updated_at = NOW()
		`

		_, err = db.conn.Exec(ctx, upsertQuery,
			server.ID,
			server.Name,
			server.Description,
			server.Status,
			repositoryJSON,
			server.VersionDetail.Version,
			releaseDate,
			server.VersionDetail.IsLatest,
			packagesJSON,
			remotesJSON,
		)

		if err != nil {
			log.Printf("Error importing server %s: %v", server.ID, err)
			continue
		}

		log.Printf("[%d/%d] Imported server: %s", i+1, len(servers), server.Name)
	}

	log.Println("PostgreSQL database import completed successfully")
	return nil
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