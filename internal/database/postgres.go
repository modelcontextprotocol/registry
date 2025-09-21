package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	pool *pgxpool.Pool
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

func (db *PostgreSQL) List(
	ctx context.Context,
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

	// Build WHERE clause for filtering
	var whereConditions []string
	args := []any{}
	argIndex := 1

	// Add filters using new schema columns
	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_json->>'name' = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(server_json->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_json->>'name' ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_json->>'version' = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("is_latest = $%d", argIndex))
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

	// Query using new separated schema
	query := fmt.Sprintf(`
        SELECT version_id, server_id, status, published_at, updated_at, is_latest, server_json
        FROM servers
        %s
        ORDER BY version_id
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var versionID, serverID, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var serverJSONBytes []byte

		err := rows.Scan(&versionID, &serverID, &status, &publishedAt, &updatedAt, &isLatest, &serverJSONBytes)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the immutable server.json
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Construct the response with separated concerns
		response := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					ServerID:    serverID,
					VersionID:   versionID,
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, response)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor using version_id
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		nextCursor = lastResult.Meta.Official.VersionID
	}

	return results, nextCursor, nil
}

func (db *PostgreSQL) GetByVersionID(ctx context.Context, versionID string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT version_id, server_id, status, published_at, updated_at, is_latest, server_json
		FROM servers
		WHERE version_id = $1
	`

	var serverID, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var serverJSONBytes []byte

	err := db.pool.QueryRow(ctx, query, versionID).Scan(&versionID, &serverID, &status, &publishedAt, &updatedAt, &isLatest, &serverJSONBytes)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by ID: %w", err)
	}

	// Parse the immutable server.json
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Construct the response with separated concerns
	response := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    serverID,
				VersionID:   versionID,
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return response, nil
}

// GetByServerID retrieves the latest version of a server by server ID
func (db *PostgreSQL) GetByServerID(ctx context.Context, serverID string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT version_id, server_id, status, published_at, updated_at, is_latest, server_json
		FROM servers
		WHERE server_id = $1 AND is_latest = true
		ORDER BY published_at DESC
		LIMIT 1
	`

	var versionID, returnedServerID, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var serverJSONBytes []byte

	err := db.pool.QueryRow(ctx, query, serverID).Scan(&versionID, &returnedServerID, &status, &publishedAt, &updatedAt, &isLatest, &serverJSONBytes)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by server ID: %w", err)
	}

	// Parse the immutable server.json
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Construct the response with separated concerns
	response := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    returnedServerID,
				VersionID:   versionID,
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return response, nil
}

// GetByServerIDAndVersion retrieves a specific version of a server by server ID and version
func (db *PostgreSQL) GetByServerIDAndVersion(ctx context.Context, serverID string, version string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT version_id, server_id, status, published_at, updated_at, is_latest, server_json
		FROM servers
		WHERE server_id = $1 AND server_json->>'version' = $2
		LIMIT 1
	`

	var versionID, returnedServerID, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var serverJSONBytes []byte

	err := db.pool.QueryRow(ctx, query, serverID, version).Scan(&versionID, &returnedServerID, &status, &publishedAt, &updatedAt, &isLatest, &serverJSONBytes)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by server ID and version: %w", err)
	}

	// Parse the immutable server.json
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Construct the response with separated concerns
	response := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    returnedServerID,
				VersionID:   versionID,
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return response, nil
}

// GetAllVersionsByServerID retrieves all versions of a server by server ID
func (db *PostgreSQL) GetAllVersionsByServerID(ctx context.Context, serverID string) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT version_id, server_id, status, published_at, updated_at, is_latest, server_json
		FROM servers
		WHERE server_id = $1
		ORDER BY published_at DESC
	`

	rows, err := db.pool.Query(ctx, query, serverID)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var versionID, returnedServerID, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var serverJSONBytes []byte

		err := rows.Scan(&versionID, &returnedServerID, &status, &publishedAt, &updatedAt, &isLatest, &serverJSONBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the immutable server.json
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Construct the response with separated concerns
		response := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					ServerID:    returnedServerID,
					VersionID:   versionID,
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, response)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
}

// CreateServer adds a new server to the database with new separated schema
func (db *PostgreSQL) CreateServer(ctx context.Context, server *apiv0.ServerJSON, serverID, versionID string, isLatest bool) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Marshal only the immutable server.json
	serverJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Insert into servers table with new separated schema
	query := `
		INSERT INTO servers (version_id, server_id, status, published_at, updated_at, is_latest, server_json)
		VALUES ($1, $2, $3, NOW(), NOW(), $4, $5)
		RETURNING published_at, updated_at
	`

	var publishedAt, updatedAt time.Time
	err = db.pool.QueryRow(ctx, query, versionID, serverID, "active", isLatest, serverJSON).Scan(&publishedAt, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	// Return the ServerResponse format
	response := &apiv0.ServerResponse{
		Server: *server,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    serverID,
				VersionID:   versionID,
				Status:      model.StatusActive,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return response, nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Marshal updated server.json
	serverJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	// Update using new separated schema
	query := `
		UPDATE servers
		SET server_json = $1, updated_at = NOW()
		WHERE version_id = $2
		RETURNING server_id, status, published_at, updated_at, is_latest
	`

	var serverID, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool

	err = db.pool.QueryRow(ctx, query, serverJSON, id).Scan(&serverID, &status, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	// Return the ServerResponse format
	response := &apiv0.ServerResponse{
		Server: *server,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    serverID,
				VersionID:   id,
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return response, nil
}

// UpdateServerStatus updates only the status field of a server record
func (db *PostgreSQL) UpdateServerStatus(ctx context.Context, versionID string, status string) (*apiv0.ServerResponse, error) {
	query := `
		UPDATE servers
		SET status = $1, updated_at = NOW()
		WHERE version_id = $2
		RETURNING version_id, server_id, status, published_at, updated_at, is_latest, server_json
	`

	var serverID, returnedStatus string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var serverJSONBytes []byte

	err := db.pool.QueryRow(ctx, query, status, versionID).Scan(
		&versionID, &serverID, &returnedStatus, &publishedAt, &updatedAt, &isLatest, &serverJSONBytes,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server status: %w", err)
	}

	// Parse the server JSON
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Return the ServerResponse format
	response := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    serverID,
				VersionID:   versionID,
				Status:      model.Status(returnedStatus),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return response, nil
}

func (db *PostgreSQL) UpdateIsLatest(ctx context.Context, versionID string, isLatest bool) (*apiv0.ServerResponse, error) {
	query := `
		UPDATE servers
		SET is_latest = $1, updated_at = NOW()
		WHERE version_id = $2
		RETURNING version_id, server_id, status, published_at, updated_at, is_latest, server_json
	`
	var serverID, returnedStatus string
	var publishedAt, updatedAt time.Time
	var returnedIsLatest bool
	var serverJSONBytes []byte

	err := db.pool.QueryRow(ctx, query, isLatest, versionID).Scan(
		&versionID, &serverID, &returnedStatus, &publishedAt, &updatedAt, &returnedIsLatest, &serverJSONBytes,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update isLatest flag: %w", err)
	}

	// Parse the server JSON
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(serverJSONBytes, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Construct the response with updated metadata
	response := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				ServerID:    serverID,
				VersionID:   versionID,
				Status:      model.Status(returnedStatus),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    returnedIsLatest,
			},
		},
	}

	return response, nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
