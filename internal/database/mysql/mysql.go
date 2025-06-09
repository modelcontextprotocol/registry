package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/registry/internal/model"
	"github.com/modelcontextprotocol/registry/internal/database"
	_ "github.com/go-sql-driver/mysql"
)

// MySQLDB implements the Database interface for MySQL
type MySQLDB struct {
	db *sql.DB
}

// NewMySQLDB creates a new MySQL database connection
func NewMySQLDB(ctx context.Context, dsn string) (*MySQLDB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MySQL: %w", err)
	}

	// Test the connection with context
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping MySQL: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	return &MySQLDB{db: db}, nil
}

// List retrieves all MCPRegistry entries with optional filtering
func (m *MySQLDB) List(ctx context.Context, filter map[string]interface{}, cursor string, limit int) ([]*model.Server, string, error) {
	if limit <= 0 {
		// Set default limit if not provided
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	query := "SELECT id, name, description, repository_url, repository_source, repository_id, version_detail_version, version_detail_release_date, version_detail_is_latest FROM servers WHERE version_detail_is_latest = true"
	args := []interface{}{}

	if len(filter) > 0 {
		query += " AND "
		conditions := []string{}
		for k, v := range filter {
			switch k {
			case "version":
				conditions = append(conditions, "version_detail_version = ?")
			case "name":
				conditions = append(conditions, "name = ?")
			default:
				conditions = append(conditions, fmt.Sprintf("%s = ?", k))
			}
			args = append(args, v)
		}
		query += strings.Join(conditions, " AND ")
	}

	if cursor != "" {
		// Validate that the cursor is a valid UUID
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}

		// Verify cursor exists
		var exists bool
		err := m.db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM servers WHERE id = ?)", cursor).Scan(&exists)
		if err != nil {
			return nil, "", err
		}
		if !exists {
			return nil, "", database.ErrNotFound
		}

		query += " AND id > ?"
		args = append(args, cursor)
	}

	// Add ordering and limit
	query += " ORDER BY id LIMIT ?"
	args = append(args, limit+1)  // Fetch one extra to determine if there are more results

	rows, err := m.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var servers []*model.Server
	for rows.Next() {
		var server model.Server
		if err := rows.Scan(
			&server.ID,
			&server.Name,
			&server.Description,
			&server.Repository.URL,
			&server.Repository.Source,
			&server.Repository.ID,
			&server.VersionDetail.Version,
			&server.VersionDetail.ReleaseDate,
			&server.VersionDetail.IsLatest,
		); err != nil {
			return nil, "", err
		}
		servers = append(servers, &server)
	}

	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	// Determine next cursor to match MongoDB behavior
	nextCursor := ""
	if len(servers) > limit {
		nextCursor = servers[limit-1].ID  // Use the last ID from the trimmed results
		servers = servers[:limit]  // Trim the extra result
	}

	return servers, nextCursor, nil
}

// GetByID retrieves a single ServerDetail by its ID
func (m *MySQLDB) GetByID(ctx context.Context, id string) (*model.ServerDetail, error) {
	query := `
		SELECT 
			id, name, description, 
			repository_url, repository_source, repository_id,
			version_detail_version, version_detail_release_date, version_detail_is_latest,
			packages, remotes
		FROM servers 
		WHERE id = ?
	`
	var server model.ServerDetail
	var packagesJSON, remotesJSON []byte

	err := m.db.QueryRowContext(ctx, query, id).Scan(
		&server.ID,
		&server.Name,
		&server.Description,
		&server.Repository.URL,
		&server.Repository.Source,
		&server.Repository.ID,
		&server.VersionDetail.Version,
		&server.VersionDetail.ReleaseDate,
		&server.VersionDetail.IsLatest,
		&packagesJSON,
		&remotesJSON,
	)
	if err == sql.ErrNoRows {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get server: %w", err)
	}

	if err := json.Unmarshal(packagesJSON, &server.Packages); err != nil {
		return nil, fmt.Errorf("failed to unmarshal packages: %w", err)
	}

	if err := json.Unmarshal(remotesJSON, &server.Remotes); err != nil {
		return nil, fmt.Errorf("failed to unmarshal remotes: %w", err)
	}

	return &server, nil
}

// Publish adds a new ServerDetail to the database
func (m *MySQLDB) Publish(ctx context.Context, serverDetail *model.ServerDetail) error {
	// find a server detail with the same name and check that the current version is greater than the existing one
	var existingEntry model.ServerDetail
	err := m.db.QueryRowContext(ctx, 
		"SELECT id, version_detail_version FROM servers WHERE name = ? AND version_detail_is_latest = true",
		serverDetail.Name,
	).Scan(&existingEntry.ID, &existingEntry.VersionDetail.Version)
	
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("error checking existing entry: %w", err)
	}

	// check that the current version is greater than the existing one
	if existingEntry.ID != "" && serverDetail.VersionDetail.Version <= existingEntry.VersionDetail.Version {
		return database.ErrInvalidVersion
	}

	packagesJSON, err := json.Marshal(serverDetail.Packages)
	if err != nil {
		return fmt.Errorf("failed to marshal packages: %w", err)
	}

	remotesJSON, err := json.Marshal(serverDetail.Remotes)
	if err != nil {
		return fmt.Errorf("failed to marshal remotes: %w", err)
	}

	// Generate a new UUID for the server
	serverDetail.ID = uuid.New().String()
	serverDetail.VersionDetail.IsLatest = true
	serverDetail.VersionDetail.ReleaseDate = time.Now().Format(time.RFC3339)

	// Begin transaction to ensure atomicity
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert the new entry
	query := `
		INSERT INTO servers (
			id, name, description,
			repository_url, repository_source, repository_id,
			version_detail_version, version_detail_release_date, version_detail_is_latest,
			packages, remotes
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err = tx.ExecContext(ctx, query,
		serverDetail.ID,
		serverDetail.Name,
		serverDetail.Description,
		serverDetail.Repository.URL,
		serverDetail.Repository.Source,
		serverDetail.Repository.ID,
		serverDetail.VersionDetail.Version,
		serverDetail.VersionDetail.ReleaseDate,
		serverDetail.VersionDetail.IsLatest,
		packagesJSON,
		remotesJSON,
	)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") {
			return database.ErrAlreadyExists
		}
		return fmt.Errorf("error inserting entry: %w", err)
	}

	// update the existing entry to not be the latest version
	if existingEntry.ID != "" {
		_, err = tx.ExecContext(ctx,
			"UPDATE servers SET version_detail_is_latest = false WHERE id = ?",
			existingEntry.ID)
		if err != nil {
			return fmt.Errorf("error updating existing entry: %w", err)
		}
	}

	// Commit the transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ImportSeed imports initial data from a seed file
func (m *MySQLDB) ImportSeed(ctx context.Context, seedFilePath string) error {
	// Read and parse the seed file
	data, err := os.ReadFile(seedFilePath)
	if err != nil {
		return fmt.Errorf("failed to read seed file: %w", err)
	}

	var servers []*model.ServerDetail
	if err := json.Unmarshal(data, &servers); err != nil {
		return fmt.Errorf("failed to unmarshal seed data: %w", err)
	}

	// Begin transaction
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	log.Printf("Importing %d servers into database", len(servers))

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

		// Check if server exists and get its current version
		var existingVersion string
		err = tx.QueryRowContext(ctx, 
			"SELECT version_detail_version FROM servers WHERE id = ?",
			server.ID,
		).Scan(&existingVersion)

		query := `
			INSERT INTO servers (
				id, name, description,
				repository_url, repository_source, repository_id,
				version_detail_version, version_detail_release_date, version_detail_is_latest,
				packages, remotes
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON DUPLICATE KEY UPDATE
				description = VALUES(description),
				repository_url = VALUES(repository_url),
				repository_source = VALUES(repository_source),
				repository_id = VALUES(repository_id),
				version_detail_version = VALUES(version_detail_version),
				version_detail_release_date = VALUES(version_detail_release_date),
				version_detail_is_latest = VALUES(version_detail_is_latest),
				packages = VALUES(packages),
				remotes = VALUES(remotes)
		`

		result, err := tx.ExecContext(ctx, query,
			server.ID,
			server.Name,
			server.Description,
			server.Repository.URL,
			server.Repository.Source,
			server.Repository.ID,
			server.VersionDetail.Version,
			server.VersionDetail.ReleaseDate,
			server.VersionDetail.IsLatest,
			packagesJSON,
			remotesJSON,
		)
		if err != nil {
			log.Printf("Error importing server %s: %v", server.ID, err)
			continue
		}

		// Get the number of rows affected
		rowsAffected, err := result.RowsAffected()
		if err != nil {
			log.Printf("Error getting rows affected for server %s: %v", server.ID, err)
			continue
		}

		// Log the operation result
		switch {
		case err == sql.ErrNoRows || existingVersion == "":
			log.Printf("[%d/%d] Created server: %s", i+1, len(servers), server.Name)
		case rowsAffected > 0:
			log.Printf("[%d/%d] Updated server: %s", i+1, len(servers), server.Name)
		default:
			log.Printf("[%d/%d] Server already up to date: %s", i+1, len(servers), server.Name)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Println("MySQL database import completed successfully")
	return nil
}

// Close closes the database connection
func (m *MySQLDB) Close() error {
	return m.db.Close()
}

// Connection returns information about the database connection
func (m *MySQLDB) Connection() *database.ConnectionInfo {
	return &database.ConnectionInfo{
		Type:        database.ConnectionTypeMySQL,
		IsConnected: m.db.Ping() == nil,
		Raw:         m.db,
	}
} 