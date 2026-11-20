package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestUpdatedListingIndexMigration(t *testing.T) {
	for _, mode := range []string{"fresh", "upgrade", "prebuilt", "prebuilt-reader", "prebuilt-writer", "wrong-definition", "wrong-name-opclass", "wrong-time-opclass", "wrong-name-collation", "wrong-time-collation"} {
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
     CREATE INDEX IF NOT EXISTS idx_servers_updated_page_cover ON servers(server_name,version) INCLUDE(updated_at,is_latest,status);
     CREATE INDEX IF NOT EXISTS idx_servers_updated_at ON servers(updated_at DESC);
     DELETE FROM schema_migrations WHERE version=20;
    `)
				require.NoError(t, err)
				switch mode {
				case "prebuilt", "prebuilt-reader", "prebuilt-writer":
					// Concurrent builds must be separate autocommit statements.
					_, err = conn.Exec(ctx, `CREATE INDEX CONCURRENTLY idx_servers_updated_name_cover ON servers(server_name,version,updated_at) INCLUDE(is_latest,status)`)
					require.NoError(t, err)
					_, err = conn.Exec(ctx, `CREATE INDEX CONCURRENTLY idx_servers_updated_time_cover ON servers(updated_at DESC,server_name,version)`)
					require.NoError(t, err)
				case "wrong-name-opclass", "wrong-time-opclass", "wrong-name-collation", "wrong-time-collation":
					definitions := map[string]string{
						"wrong-name-opclass":   `CREATE INDEX idx_servers_updated_name_cover ON servers(server_name varchar_pattern_ops,version,updated_at) INCLUDE(is_latest,status)`,
						"wrong-time-opclass":   `CREATE INDEX idx_servers_updated_time_cover ON servers(updated_at DESC,server_name,version varchar_pattern_ops)`,
						"wrong-name-collation": `CREATE INDEX idx_servers_updated_name_cover ON servers(server_name COLLATE "C",version,updated_at) INCLUDE(is_latest,status)`,
						"wrong-time-collation": `CREATE INDEX idx_servers_updated_time_cover ON servers(updated_at DESC,server_name,version COLLATE "C")`,
					}
					_, err = conn.Exec(ctx, definitions[mode])
					require.NoError(t, err)
				case "wrong-definition":
					// The old INCLUDE layout must not silently satisfy the new index name.
					_, err = conn.Exec(ctx, `CREATE INDEX idx_servers_updated_name_cover ON servers(server_name,version) INCLUDE(updated_at,is_latest,status)`)
					require.NoError(t, err)
				}
				if mode == "prebuilt-reader" || mode == "prebuilt-writer" {
					// Hold a real reader/writer open while startup applies 020.
					// A prebuilt migration must not request conflicting table locks.
					blocker, beginErr := db.pool.Begin(ctx)
					require.NoError(t, beginErr)
					defer func() { require.NoError(t, blocker.Rollback(ctx)) }()
					statement := "SELECT count(*) FROM servers"
					if mode == "prebuilt-writer" {
						statement = "UPDATE servers SET updated_at=updated_at WHERE false"
					}
					_, err = blocker.Exec(ctx, statement)
					require.NoError(t, err)
				}
				migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				defer cancel()
				err = migrator.applyMigration(migrationCtx, migration)
				if strings.HasPrefix(mode, "wrong-") {
					require.ErrorContains(t, err, "expected definition")
					assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_page_cover", true)
					assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_at", true)
					assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_time_cover", strings.HasPrefix(mode, "wrong-time-"))
					var applied bool
					require.NoError(t, conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=20)").Scan(&applied))
					require.False(t, applied)
					return
				}
				require.NoError(t, err)
			}
			assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_page_cover", true)
			assertUpdatedIndexExists(t, conn.Conn(), "idx_servers_updated_at", true)
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
