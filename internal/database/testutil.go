package database

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

// NewTestDB creates an isolated PostgreSQL database for each test.
// Each test gets its own database with a random name, eliminating all coordination issues.
// Requires PostgreSQL to be running on localhost:5432 (e.g., via docker-compose).
func NewTestDB(t *testing.T) Database {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Generate unique database name for this test
	var randomBytes [8]byte
	_, err := rand.Read(randomBytes[:])
	require.NoError(t, err, "Failed to generate random database id")
	randomInt := binary.BigEndian.Uint64(randomBytes[:])
	dbName := fmt.Sprintf("test_%d", randomInt)

	// Connect to postgres database to create test database
	adminURI := "postgres://mcpregistry:mcpregistry@localhost:5432/postgres?sslmode=disable"
	adminConn, err := pgx.Connect(ctx, adminURI)
	require.NoError(t, err, "Failed to connect to PostgreSQL. Make sure PostgreSQL is running via: docker-compose up -d postgres")
	defer adminConn.Close(ctx)

	// Create test database
	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName))
	require.NoError(t, err, "Failed to create test database")

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

	// Connect to test database
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
