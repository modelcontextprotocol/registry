package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestUpdatedListingIndexMigration(t *testing.T) {
	for _, mode := range []string{"fresh", "upgrade", "prebuilt", "wrong-definition"} {
		t.Run(mode, func(t *testing.T) {
			db := NewTestDB(t).(*PostgreSQL)
			ctx := context.Background()
			conn, err := db.pool.Acquire(ctx)
			require.NoError(t, err)
			defer conn.Release()
			migrator := NewMigrator(conn.Conn())
			migrations, err := migrator.loadMigrations()
			require.NoError(t, err)
			var migration Migration
			for _, candidate := range migrations {
				if candidate.Version == 20 {
					migration = candidate
				}
			}
			require.Equal(t, 20, migration.Version)
			if mode != "fresh" {
				_, err = conn.Exec(ctx, `
     DROP INDEX idx_servers_updated_name_cover;
     DROP INDEX idx_servers_updated_time_cover;
     CREATE INDEX idx_servers_updated_page_cover ON servers(server_name,version) INCLUDE(updated_at,is_latest,status);
     CREATE INDEX idx_servers_updated_at ON servers(updated_at DESC);
     DELETE FROM schema_migrations WHERE version=20;
    `)
				require.NoError(t, err)
				switch mode {
				case "prebuilt":
					// Concurrent builds must be separate autocommit statements.
					_, err = conn.Exec(ctx, `CREATE INDEX CONCURRENTLY idx_servers_updated_name_cover ON servers(server_name,version,updated_at) INCLUDE(is_latest,status)`)
					require.NoError(t, err)
					_, err = conn.Exec(ctx, `CREATE INDEX CONCURRENTLY idx_servers_updated_time_cover ON servers(updated_at DESC,server_name,version)`)
					require.NoError(t, err)
				case "wrong-definition":
					// The old INCLUDE layout must not silently satisfy the new index name.
					_, err = conn.Exec(ctx, `CREATE INDEX idx_servers_updated_name_cover ON servers(server_name,version) INCLUDE(updated_at,is_latest,status)`)
					require.NoError(t, err)
				}
				err = migrator.applyMigration(ctx, migration)
				if mode == "wrong-definition" {
					require.ErrorContains(t, err, "expected definition")
					assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_page_cover", true)
					assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_at", true)
					assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_time_cover", false)
					var applied bool
					require.NoError(t, conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=20)").Scan(&applied))
					require.False(t, applied)
					return
				}
				require.NoError(t, err)
			}
			assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_page_cover", false)
			assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_at", false)
			for _, name := range []string{"idx_servers_updated_name_cover", "idx_servers_updated_time_cover"} {
				var usable bool
				require.NoError(t, conn.QueryRow(ctx, `SELECT indisvalid AND indisready AND indnkeyatts=3 FROM pg_index WHERE indexrelid=to_regclass($1)`, name).Scan(&usable))
				require.True(t, usable)
			}
			require.NoError(t, migrator.Migrate(ctx))
		})
	}
}

func assertUpdatedIndexExists(t *testing.T, conn *pgx.Conn, name string, expected bool) {
	t.Helper()
	var exists bool
	require.NoError(t, conn.QueryRow(context.Background(), "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists))
	require.Equal(t, expected, exists, name)
}
