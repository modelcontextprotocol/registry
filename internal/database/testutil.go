package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var (
	testSchemaInitOnce sync.Once
	errTestSchemaInit  error
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

	// Initialize schema once per test suite run
	testSchemaInitOnce.Do(func() {
		errTestSchemaInit = initializeTestSchema(db)
	})
	require.NoError(t, errTestSchemaInit, "Failed to initialize test database schema")

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

// initializeTestSchema sets up a fresh database schema with all migrations applied
// This runs only once per test suite execution
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