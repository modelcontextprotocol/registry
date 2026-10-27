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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/modelcontextprotocol/registry/internal/telemetry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	pool        *pgxpool.Pool
	poolMetrics metric.Registration
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

	// Configure pool for stability-focused defaults.
	// MaxConns was 30 per pod (60 total across 2 replicas) which saturated under
	// the 2026-04-28 scraper bursts (15 req/s on /v0/servers caused queue blowup
	// even though individual queries were fast). 60 per pod gives 120 total,
	// leaving 80 of PG max_connections=200 for autovacuum/admin/headroom.
	config.MaxConns = 60                      // Handle scraper-burst concurrent load
	config.MinConns = 10                      // Keep connections warm for fast response
	config.MaxConnIdleTime = 30 * time.Minute // Keep connections available for bursts
	config.MaxConnLifetime = 2 * time.Hour    // Refresh connections regularly for stability
	dbMetrics, err := telemetry.NewDatabaseMetrics(otel.Meter("mcp_registry.database"))
	if err != nil {
		return nil, fmt.Errorf("initialize database metrics: %w", err)
	}
	config.ConnConfig.Tracer = dbMetrics

	// Create connection pool with configured settings
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}
	initialized := false
	defer func() {
		if !initialized {
			pool.Close()
		}
	}()

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
	registration, err := dbMetrics.ObservePool(pool)
	if err != nil {
		return nil, fmt.Errorf("observe database pool: %w", err)
	}
	initialized = true

	return &PostgreSQL{
		pool:        pool,
		poolMetrics: registration,
	}, nil
}

// buildFilterConditions constructs WHERE clause conditions from a ServerFilter
//
//nolint:unparam // argIndex is always 1 currently but kept for API flexibility
func buildFilterConditions(filter *ServerFilter, argIndex int) ([]string, []any, int) {
	var conditions []string
	var args []any

	if filter == nil {
		return conditions, args, argIndex
	}

	if filter.Name != nil {
		conditions = append(conditions, fmt.Sprintf("server_name = $%d", argIndex))
		args = append(args, *filter.Name)
		argIndex++
	}
	if filter.RemoteURL != nil {
		// Use a JSONB containment predicate so the planner can use the GIN index
		// idx_servers_json_remotes on (value -> 'remotes'). The previously-used
		// EXISTS / jsonb_array_elements / ->> form is logically equivalent but
		// the planner can't translate the per-row array unfolding into a GIN
		// search — it falls back to scanning every row. validateNoDuplicateRemoteURLs
		// in the publish path ran this filter and was measured at ~10s on prod's
		// 21K-row table on 2026-04-28; the containment form is GIN-indexable and
		// completes in low single-digit ms.
		conditions = append(conditions, fmt.Sprintf("value -> 'remotes' @> jsonb_build_array(jsonb_build_object('url', $%d::text))", argIndex))
		args = append(args, *filter.RemoteURL)
		argIndex++
	}
	if filter.UpdatedSince != nil {
		conditions = append(conditions, fmt.Sprintf("updated_at > $%d", argIndex))
		args = append(args, *filter.UpdatedSince)
		argIndex++
	}
	if filter.SubstringName != nil {
		// Escape LIKE metacharacters so that user input cannot expand into
		// wildcard matches (e.g. `?search=_` matching every single-char name,
		// `?search=%` matching everything). Order matters: backslashes must be
		// escaped first so subsequent escape backslashes are not double-escaped.
		escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(*filter.SubstringName)
		conditions = append(conditions, fmt.Sprintf("server_name ILIKE $%d ESCAPE '\\'", argIndex))
		args = append(args, "%"+escaped+"%")
		argIndex++
		// check_server_name_format guarantees one namespace/path separator. A search
		// spanning it must match a suffix of the namespace. This narrows full
		// name searches before the original substring predicate is rechecked.
		if namespace, _, ok := strings.Cut(*filter.SubstringName, "/"); ok && len(namespace) >= 3 {
			reversed := []rune(namespace)
			for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
				reversed[i], reversed[j] = reversed[j], reversed[i]
			}
			prefix := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(string(reversed))
			conditions = append(conditions, fmt.Sprintf("lower(reverse(split_part(server_name, '/', 1))) LIKE lower($%d) ESCAPE '\\'", argIndex))
			args = append(args, prefix+"%")
			argIndex++
		}
	}
	if filter.Version != nil {
		conditions = append(conditions, fmt.Sprintf("version = $%d", argIndex))
		args = append(args, *filter.Version)
		argIndex++
	}
	if filter.IsLatest != nil {
		conditions = append(conditions, fmt.Sprintf("is_latest = $%d", argIndex))
		args = append(args, *filter.IsLatest)
		argIndex++
	}
	if filter.IncludeDeleted == nil || !*filter.IncludeDeleted {
		conditions = append(conditions, "status != 'deleted'")
	}

	return conditions, args, argIndex
}

// addCursorCondition adds pagination cursor condition to WHERE clause.
//
// The compound cursor uses a row-constructor comparison so PostgreSQL can seek
// directly into the (server_name, version) B-tree index. The OR-decomposed form
// `server_name > X OR (server_name = X AND version > Y)` is logically equivalent
// but PostgreSQL's planner cannot use it for an index seek — it scans the index
// from the start and filters everything before the cursor, making cost grow
// linearly with cursor depth (a 20K-row table at a deep cursor took ~760ms in
// prod). The row-constructor form `(server_name, version) > (X, Y)` is special-
// cased and stays constant-time regardless of cursor depth.
func addCursorCondition(cursor string, argIndex int) (string, []any, int) {
	if cursor == "" {
		return "", nil, argIndex
	}

	// Parse cursor format: "serverName:version"
	parts := strings.SplitN(cursor, ":", 2)
	if len(parts) == 2 {
		cursorServerName := parts[0]
		cursorVersion := parts[1]
		condition := fmt.Sprintf("(server_name, version) > ($%d, $%d)", argIndex, argIndex+1)
		return condition, []any{cursorServerName, cursorVersion}, argIndex + 2
	}

	// Fallback for malformed cursor - treat as server name only for backwards compatibility
	condition := fmt.Sprintf("server_name > $%d", argIndex)
	return condition, []any{cursor}, argIndex + 1
}

// searchPageQuery retains the ordinary query for listings without a search.
func searchPageQuery(filter *ServerFilter, ordinary, whereClause string, limitArg int) string {
	if filter == nil || filter.SubstringName == nil {
		return ordinary
	}
	// Find the page using only its keys before fetching the JSON payloads.
	// The covering index can reject nonmatching names without visiting the
	// heap. The materialized candidate query plans for a larger key batch:
	// a small LIMIT otherwise assumes rare matches occur early in name order
	// and can favor scanning most of the ordered index over a selective GIN
	// lookup. The outer LIMIT still defines the exact page before payload fetch.
	// PostgreSQL 17 can stop the ordered CTE scan after the requested rows.
	return fmt.Sprintf(`
        WITH candidates AS MATERIALIZED (
            SELECT server_name, version FROM servers
            %s
            ORDER BY server_name, version
            LIMIT GREATEST($%d, 8192)
        )
        SELECT s.server_name, s.version, s.status, s.status_changed_at, s.status_message,
               s.published_at, s.updated_at, s.is_latest, s.value
        FROM (
            SELECT server_name, version FROM candidates
            ORDER BY server_name, version
            LIMIT $%d
        ) page
        JOIN servers s USING (server_name, version)
        ORDER BY s.server_name, s.version
    `, whereClause, limitArg, limitArg)
}

func searchQueryArgs(filter *ServerFilter, args []any) []any {
	if filter != nil && filter.SubstringName != nil {
		// Search selectivity varies by orders of magnitude. Use an unnamed
		// extended-protocol query so PostgreSQL plans for these parameter values
		// instead of eventually reusing one generic plan for every search term.
		return append([]any{pgx.QueryExecModeExec}, args...)
	}
	return args
}

func (db *PostgreSQL) ListServers(
	ctx context.Context,
	tx pgx.Tx,
	filter *ServerFilter,
	cursor string,
	limit int,
) (_ []*apiv0.ServerResponse, _ string, resultErr error) {
	diagnostic := telemetry.ListingFromContext(ctx)
	defer func() { diagnostic.DatabaseDone(resultErr) }()
	operation := "list"
	switch {
	case filter != nil && filter.SubstringName != nil:
		operation = "list_search"
	case filter != nil && filter.UpdatedSince != nil:
		operation = "list_updated"
	case cursor != "":
		operation = "list_page"
	}
	ctx = telemetry.WithDatabaseOperation(ctx, operation)
	if limit <= 0 {
		limit = 10
	}

	shape := telemetry.ListingShape{Cursor: cursor != "", Limit: limit}
	if filter != nil {
		shape.Search = filter.SubstringName != nil
		shape.Updated = filter.UpdatedSince != nil
		shape.Latest = filter.IsLatest != nil && *filter.IsLatest
		shape.Version = filter.Version != nil
		shape.IncludeDeleted = filter.IncludeDeleted != nil && *filter.IncludeDeleted
	}
	diagnostic.SetShape(shape)
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause conditions
	argIndex := 1
	whereConditions, args, argIndex := buildFilterConditions(filter, argIndex)

	// Add cursor pagination
	cursorCondition, cursorArgs, argIndex := addCursorCondition(cursor, argIndex)
	if cursorCondition != "" {
		whereConditions = append(whereConditions, cursorCondition)
		args = append(args, cursorArgs...)
	}

	// Build the WHERE clause
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Query servers table with hybrid column/JSON data
	query := fmt.Sprintf(`
        SELECT server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest, value
        FROM servers
        %s
        ORDER BY server_name, version
        LIMIT $%d
    `, whereClause, argIndex)
	query = searchPageQuery(filter, query, whereClause, argIndex)
	args = append(args, limit)
	args = searchQueryArgs(filter, args)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	results, err := decodeListingRows(ctx, rows, diagnostic)
	if err != nil {
		return nil, "", err
	}

	// Determine next cursor using compound serverName:version format
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		nextCursor = lastResult.Server.Name + ":" + lastResult.Server.Version
	}

	return results, nextCursor, nil
}

func decodeListingRows(ctx context.Context, rows pgx.Rows, diagnostic *telemetry.ListingDiagnostic) ([]*apiv0.ServerResponse, error) {
	defer rows.Close()

	// Copy the bounded page before decoding JSON. Exhausting/closing pooled rows
	// releases the connection, so application CPU work cannot keep it occupied.
	// Within a caller-owned transaction, closing rows only frees the transaction
	// for its next query; the caller still owns the transaction's connection.
	type listingRow struct {
		metadata apiv0.RegistryExtensions
		value    []byte
	}
	var page []listingRow
	diagnostic.Phase("row_fetch")
	for rows.Next() {
		diagnostic.RowReceived()
		diagnostic.Phase("row_fetch")
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var serverName, version, status string
		var row listingRow
		meta := &row.metadata
		// pgx scans JSONB into an owned []byte, valid after Next and Close.
		if err := rows.Scan(&serverName, &version, &status, &meta.StatusChangedAt, &meta.StatusMessage,
			&meta.PublishedAt, &meta.UpdatedAt, &meta.IsLatest, &row.value); err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}
		meta.Status = model.Status(status)
		diagnostic.AddBytes(len(row.value))
		page = append(page, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	var results []*apiv0.ServerResponse
	diagnostic.Phase("row_decode")
	for i := range page {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(page[i].value, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}
		page[i].value = nil // Drop each copied payload as its decoded form replaces it.
		results = append(results, &apiv0.ServerResponse{
			Server: serverJSON,
			Meta:   apiv0.ResponseMeta{Official: &page[i].metadata},
		})
	}

	return results, nil
}

// GetServerByName retrieves the latest version of a server by server name
func (db *PostgreSQL) GetServerByName(ctx context.Context, tx pgx.Tx, serverName string, includeDeleted bool) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Build filter conditions
	isLatest := true
	filter := &ServerFilter{
		Name:           &serverName,
		IsLatest:       &isLatest,
		IncludeDeleted: &includeDeleted,
	}

	argIndex := 1
	whereConditions, args, _ := buildFilterConditions(filter, argIndex)

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest, value
		FROM servers
		%s
		ORDER BY published_at DESC
		LIMIT 1
	`, whereClause)

	var name, version, status string
	var statusChangedAt, publishedAt, updatedAt time.Time
	var statusMessage *string
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, args...).Scan(&name, &version, &status, &statusChangedAt, &statusMessage, &publishedAt, &updatedAt, &isLatest, &valueJSON)
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
				Status:          model.Status(status),
				StatusChangedAt: statusChangedAt,
				StatusMessage:   statusMessage,
				PublishedAt:     publishedAt,
				UpdatedAt:       updatedAt,
				IsLatest:        isLatest,
			},
		},
	}

	return serverResponse, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version
func (db *PostgreSQL) GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string, includeDeleted bool) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Build filter conditions
	filter := &ServerFilter{
		Name:           &serverName,
		Version:        &version,
		IncludeDeleted: &includeDeleted,
	}

	argIndex := 1
	whereConditions, args, _ := buildFilterConditions(filter, argIndex)

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest, value
		FROM servers
		%s
		LIMIT 1
	`, whereClause)

	var name, vers, status string
	var statusChangedAt, publishedAt, updatedAt time.Time
	var statusMessage *string
	var isLatest bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, args...).Scan(&name, &vers, &status, &statusChangedAt, &statusMessage, &publishedAt, &updatedAt, &isLatest, &valueJSON)
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
				Status:          model.Status(status),
				StatusChangedAt: statusChangedAt,
				StatusMessage:   statusMessage,
				PublishedAt:     publishedAt,
				UpdatedAt:       updatedAt,
				IsLatest:        isLatest,
			},
		},
	}

	return serverResponse, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
func (db *PostgreSQL) GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string, includeDeleted bool) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Build filter conditions
	filter := &ServerFilter{
		Name:           &serverName,
		IncludeDeleted: &includeDeleted,
	}

	argIndex := 1
	whereConditions, args, _ := buildFilterConditions(filter, argIndex)

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest, value
		FROM servers
		%s
		ORDER BY published_at DESC
	`, whereClause)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var name, version, status string
		var statusChangedAt, publishedAt, updatedAt time.Time
		var statusMessage *string
		var isLatest bool
		var valueJSON []byte

		err := rows.Scan(&name, &version, &status, &statusChangedAt, &statusMessage, &publishedAt, &updatedAt, &isLatest, &valueJSON)
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
					Status:          model.Status(status),
					StatusChangedAt: statusChangedAt,
					StatusMessage:   statusMessage,
					PublishedAt:     publishedAt,
					UpdatedAt:       updatedAt,
					IsLatest:        isLatest,
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
		INSERT INTO servers (server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err = db.getExecutor(tx).Exec(ctx, insertQuery,
		serverJSON.Name,
		serverJSON.Version,
		string(officialMeta.Status),
		officialMeta.StatusChangedAt,
		officialMeta.StatusMessage,
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
		RETURNING server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest
	`

	var name, vers, status string
	var statusChangedAt, publishedAt, updatedAt time.Time
	var statusMessage *string
	var isLatest bool

	err = db.getExecutor(tx).QueryRow(ctx, query, valueJSON, serverName, version).Scan(&name, &vers, &status, &statusChangedAt, &statusMessage, &publishedAt, &updatedAt, &isLatest)
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
				Status:          model.Status(status),
				StatusChangedAt: statusChangedAt,
				StatusMessage:   statusMessage,
				PublishedAt:     publishedAt,
				UpdatedAt:       updatedAt,
				IsLatest:        isLatest,
			},
		},
	}

	return serverResponse, nil
}

// SetServerStatus updates the status of a specific server version
func (db *PostgreSQL) SetServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status model.Status, statusMessage *string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Update the status and related fields
	// Only update status_changed_at when status actually changes
	query := `
		UPDATE servers
		SET
			status = $1,
			status_changed_at = CASE WHEN status != $1::varchar THEN NOW() ELSE status_changed_at END,
			updated_at = NOW(),
			status_message = $4
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, value, published_at, updated_at, is_latest, status_changed_at, status_message
	`

	var name, vers, currentStatus string
	var publishedAt, updatedAt, statusChangedAt time.Time
	var isLatest bool
	var valueJSON []byte
	var resultStatusMessage *string

	err := db.getExecutor(tx).QueryRow(ctx, query, string(status), serverName, version, statusMessage).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest, &statusChangedAt, &resultStatusMessage)
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
				Status:          model.Status(currentStatus),
				StatusChangedAt: statusChangedAt,
				StatusMessage:   resultStatusMessage,
				PublishedAt:     publishedAt,
				UpdatedAt:       updatedAt,
				IsLatest:        isLatest,
			},
		},
	}

	return serverResponse, nil
}

// SetAllVersionsStatus updates the status of all versions of a server in a single query
func (db *PostgreSQL) SetAllVersionsStatus(ctx context.Context, tx pgx.Tx, serverName string, status model.Status, statusMessage *string) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Update the status and related fields for all versions
	// Only update rows where status or status_message actually changes
	// Only update status_changed_at when status actually changes
	query := `
		UPDATE servers
		SET
			status = $1,
			status_changed_at = CASE WHEN status != $1::varchar THEN NOW() ELSE status_changed_at END,
			updated_at = NOW(),
			status_message = $2
		WHERE server_name = $3
			AND (status != $1::varchar OR status_message IS DISTINCT FROM $2)
		RETURNING server_name, version, status, value, published_at, updated_at, is_latest, status_changed_at, status_message
	`

	rows, err := db.getExecutor(tx).Query(ctx, query, string(status), statusMessage, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to update all server versions status: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var name, vers, currentStatus string
		var publishedAt, updatedAt, statusChangedAt time.Time
		var isLatest bool
		var valueJSON []byte
		var resultStatusMessage *string

		if err := rows.Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest, &statusChangedAt, &resultStatusMessage); err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		// Unmarshal the JSON data
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:          model.Status(currentStatus),
					StatusChangedAt: statusChangedAt,
					StatusMessage:   resultStatusMessage,
					PublishedAt:     publishedAt,
					UpdatedAt:       updatedAt,
					IsLatest:        isLatest,
				},
			},
		}
		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating server rows: %w", err)
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
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
	return int64(hash & 0x7FFFFFFFFFFFFFFF)
}

// GetCurrentLatestVersion retrieves the current latest version of a server by server name
func (db *PostgreSQL) GetCurrentLatestVersion(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `
		SELECT server_name, version, status, status_changed_at, status_message, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND is_latest = true
	`

	row := executor.QueryRow(ctx, query, serverName)

	var name, version, status string
	var statusChangedAt, publishedAt, updatedAt time.Time
	var statusMessage *string
	var isLatest bool
	var jsonValue []byte

	err := row.Scan(&name, &version, &status, &statusChangedAt, &statusMessage, &publishedAt, &updatedAt, &isLatest, &jsonValue)
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
				Status:          model.Status(status),
				StatusChangedAt: statusChangedAt,
				StatusMessage:   statusMessage,
				PublishedAt:     publishedAt,
				UpdatedAt:       updatedAt,
				IsLatest:        isLatest,
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

// SetLatestVersion sets is_latest=true on the given version and false on all other versions
// of the same server. Passing an empty version clears is_latest for all rows.
//
// The clear and set are issued as separate statements because the unique partial index
// idx_unique_latest_per_server is non-deferrable and Postgres checks it row-by-row within
// a single UPDATE, which would trip when flipping one row's flag off and another's on.
func (db *PostgreSQL) SetLatestVersion(ctx context.Context, tx pgx.Tx, serverName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)

	if _, err := executor.Exec(ctx,
		`UPDATE servers SET is_latest = false WHERE server_name = $1 AND is_latest = true AND version <> $2`,
		serverName, version,
	); err != nil {
		return fmt.Errorf("failed to clear previous latest version: %w", err)
	}

	if version == "" {
		return nil
	}

	if _, err := executor.Exec(ctx,
		`UPDATE servers SET is_latest = true WHERE server_name = $1 AND version = $2 AND is_latest = false`,
		serverName, version,
	); err != nil {
		return fmt.Errorf("failed to set latest version: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	var err error
	if db.poolMetrics != nil {
		err = db.poolMetrics.Unregister()
	}
	db.pool.Close()
	return err
}
