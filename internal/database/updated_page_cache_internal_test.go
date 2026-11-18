package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/require"
)

func TestUpdatedPageCachePreservesChangingWatermarksAndBounds(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	for i := 1; i <= 140; i++ {
		stamp := epoch.Add(time.Duration(i) * time.Second)
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: fmt.Sprintf("com.example/cached-%03d", i), Version: "1.0.0", Description: "cached page fixture"}, &apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: epoch, UpdatedAt: stamp, StatusChangedAt: stamp, IsLatest: true})
		require.NoError(t, err)
	}
	// Keep one connection so each query shape crosses the prepared-plan threshold.
	conn, err := db.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()
	yes := true
	for round := 0; round < 3; round++ {
		for _, count := range []int{1, 128, 129, 140, 0, 21} {
			cutoff := epoch.Add(time.Duration(140-count) * time.Second)
			filter := &ServerFilter{UpdatedSince: &cutoff, IncludeDeleted: &yes}
			for _, limit := range []int{1, 100} {
				for _, after := range []int{0, 51} {
					cursor := ""
					if after > 0 {
						cursor = fmt.Sprintf("com.example/cached-%03d:1.0.0", after)
					}
					rows, _, err := db.ListServers(ctx, tx, filter, cursor, limit)
					require.NoError(t, err)
					var want []string
					for i := 1; i <= 140 && len(want) < limit; i++ {
						if i > 140-count && i > after {
							want = append(want, fmt.Sprintf("com.example/cached-%03d", i))
						}
					}
					var got []string
					for _, row := range rows {
						got = append(got, row.Server.Name)
					}
					require.Equal(t, want, got, "round=%d count=%d limit=%d after=%d", round, count, limit, after)
				}
			}
		}
	}
	var cached int
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM pg_prepared_statements WHERE statement ~ '^[[:space:]]*WITH recent AS MATERIALIZED'").Scan(&cached))
	require.Equal(t, 4, cached, "two page sizes, with and without a compound cursor")
	var parameterizedLimit int
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM pg_prepared_statements WHERE statement ~ '^[[:space:]]*WITH recent AS MATERIALIZED' AND statement LIKE '%LIMIT $%'").Scan(&parameterizedLimit))
	require.Zero(t, parameterizedLimit)

	// Changes are visible without reconnecting or clearing the cached statements.
	_, err = tx.Exec(ctx, "UPDATE servers SET updated_at=$1 WHERE server_name LIKE 'com.example/cached-%'", epoch)
	require.NoError(t, err)
	rows, _, err := db.ListServers(ctx, tx, &ServerFilter{UpdatedSince: &epoch, IncludeDeleted: &yes}, "", 100)
	require.NoError(t, err)
	require.Empty(t, rows)

	// Keep additional filters and uncommon sizes on their existing custom path.
	version, name, search, remote := "1.0.0", "com.example/cached-001", "cached", "https://example.invalid/mcp"
	for _, filter := range []*ServerFilter{
		{UpdatedSince: &epoch, Version: &version}, {UpdatedSince: &epoch, Name: &name},
		{UpdatedSince: &epoch, SubstringName: &search}, {UpdatedSince: &epoch, IsLatest: &yes},
		{UpdatedSince: &epoch, RemoteURL: &remote},
	} {
		_, _, err = db.ListServers(ctx, tx, filter, "", 100)
		require.NoError(t, err)
	}
	for _, limit := range []int{0, 10, 128, 1000} {
		_, _, err = db.ListServers(ctx, tx, &ServerFilter{UpdatedSince: &epoch}, "", limit)
		require.NoError(t, err)
	}
	var after int
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM pg_prepared_statements WHERE statement ~ '^[[:space:]]*WITH recent AS MATERIALIZED'").Scan(&after))
	require.Equal(t, cached, after)
}
