package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// Advisory lock key for database schema initialization
	// Using a fixed key ensures all test processes coordinate on the same lock
	testSchemaLockKey = 123456789
)

// NewTestDB creates a new PostgreSQL database connection for testing.
// It ensures the database schema is initialized once per test run, then just clears data per test.
// Requires PostgreSQL to be running on localhost:5432 (e.g., via docker-compose).
func NewTestDB(t *testing.T) Database {
	t.Helper()

	// Create context with timeout for database operations
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect to test database
	connectionURI := "postgres://mcpregistry:mcpregistry@localhost:5432/mcp-registry?sslmode=disable"
	db, err := NewPostgreSQL(ctx, connectionURI)
	require.NoError(t, err, "Failed to connect to test PostgreSQL database. Make sure PostgreSQL is running via: docker-compose up -d postgres")

	// Initialize schema once per test suite run using advisory locks for cross-process coordination
	err = initializeTestSchemaWithLock(db)
	require.NoError(t, err, "Failed to initialize test database schema")

	// Clear data for this specific test
	clearTestData(t, db)

	// Register cleanup function to close database connection
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("Warning: failed to close test database connection: %v", err)
		}
	})

	return db
}

// initializeTestSchemaWithLock sets up a fresh database schema with all migrations applied
// Uses PostgreSQL advisory locks to ensure only one process initializes the schema
func initializeTestSchemaWithLock(db Database) error {
	// Cast to PostgreSQL to access the connection pool
	pgDB, ok := db.(*PostgreSQL)
	if !ok {
		return fmt.Errorf("expected PostgreSQL database instance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Acquire advisory lock to coordinate schema initialization across processes
	_, err := pgDB.pool.Exec(ctx, "SELECT pg_advisory_lock($1)", testSchemaLockKey)
	if err != nil {
		return fmt.Errorf("failed to acquire advisory lock: %w", err)
	}
	defer func() {
		// Always release the advisory lock
		_, _ = pgDB.pool.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", testSchemaLockKey)
	}()

	// Check if schema already exists (another process may have initialized it)
	var tableCount int64
	err = pgDB.pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'servers'").Scan(&tableCount)
	if err != nil {
		return fmt.Errorf("failed to check if schema exists: %w", err)
	}

	if tableCount > 0 {
		// Schema already exists, nothing to do
		return nil
	}

	// Initialize the schema
	return initializeTestSchema(db)
}

// initializeTestSchema sets up a fresh database schema with all migrations applied
// This should only be called from initializeTestSchemaWithLock
func initializeTestSchema(db Database) error {
	// Cast to PostgreSQL to access the connection pool
	pgDB, ok := db.(*PostgreSQL)
	if !ok {
		return fmt.Errorf("expected PostgreSQL database instance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Drop and recreate schema completely fresh
	_, err := pgDB.pool.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public;")
	if err != nil {
		return fmt.Errorf("failed to reset database schema: %w", err)
	}

	// Apply all migrations from scratch
	conn, err := pgDB.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for migration: %w", err)
	}
	defer conn.Release()

	migrator := NewMigrator(conn.Conn())
	err = migrator.Migrate(ctx)
	if err != nil {
		return fmt.Errorf("failed to run database migrations: %w", err)
	}

	return nil
}

// clearTestData removes all data from test tables while preserving schema
// This runs before each individual test
func clearTestData(t *testing.T, db Database) {
	t.Helper()

	// Cast to PostgreSQL to access the connection pool
	pgDB, ok := db.(*PostgreSQL)
	require.True(t, ok, "Expected PostgreSQL database instance")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Clear all data but keep schema intact
	_, err := pgDB.pool.Exec(ctx, "TRUNCATE TABLE servers RESTART IDENTITY CASCADE")
	require.NoError(t, err, "Failed to clear test data")
}