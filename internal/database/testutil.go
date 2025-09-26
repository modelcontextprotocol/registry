package database

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

const templateDBName = "mcp_registry_test_template"

// ensureTemplateDB creates a template database with migrations applied
// Multiple processes may call this, so we handle race conditions
func ensureTemplateDB(ctx context.Context, adminConn *pgx.Conn) error {
	// Check if template exists
	var exists bool
	err := adminConn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", templateDBName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check template database: %w", err)
	}

	if exists {
		// Template already exists
		return nil
	}

	// Create template database
	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateDBName))
	if err != nil {
		// Ignore duplicate database name error - another process created it concurrently
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "pg_database_datname_index" {
			return nil
		}
		return fmt.Errorf("failed to create template database: %w", err)
	}

	// Connect to template and run migrations
	templateURI := fmt.Sprintf("postgres://mcpregistry:mcpregistry@localhost:5432/%s?sslmode=disable", templateDBName)
	templateDB, err := NewPostgreSQL(ctx, templateURI)
	if err != nil {
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer templateDB.Close()

	// Migrations run automatically in NewPostgreSQL
	return nil
}

// NewTestDB creates an isolated PostgreSQL database for each test by copying a template.
// The template database has migrations pre-applied, so each test is fast.
// Requires PostgreSQL to be running on localhost:5432 (e.g., via docker-compose).
func NewTestDB(t *testing.T) Database {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to postgres database
	adminURI := "postgres://mcpregistry:mcpregistry@localhost:5432/postgres?sslmode=disable"
	adminConn, err := pgx.Connect(ctx, adminURI)
	require.NoError(t, err, "Failed to connect to PostgreSQL. Make sure PostgreSQL is running via: docker-compose up -d postgres")
	defer adminConn.Close(ctx)

	// Ensure template database exists with migrations
	err = ensureTemplateDB(ctx, adminConn)
	require.NoError(t, err, "Failed to initialize template database")

	// Generate unique database name for this test
	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err, "Failed to generate random database id")
	randomInt := binary.BigEndian.Uint64(randomBytes[:])
	dbName := fmt.Sprintf("test_%d", randomInt)

	// Create test database from template (fast - just copies files)
	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, templateDBName))
	require.NoError(t, err, "Failed to create test database from template")

	// Register cleanup to drop database
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		// Terminate any remaining connections
		_, _ = adminConn.Exec(cleanupCtx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()",
			dbName,
		))

		// Drop database
		_, _ = adminConn.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	// Connect to test database (no migrations needed - copied from template)
	testURI := fmt.Sprintf("postgres://mcpregistry:mcpregistry@localhost:5432/%s?sslmode=disable", dbName)

	db, err := NewPostgreSQL(ctx, testURI)
	require.NoError(t, err, "Failed to connect to test database")

	// Register cleanup to close connection
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("Warning: failed to close test database connection: %v", err)
		}
	})

	return db
}
