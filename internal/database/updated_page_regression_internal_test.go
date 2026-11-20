package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestUpdatedPageRespectsConnectionExecutionMode(t *testing.T) {
	fixture := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := fixture.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: "com.example/mode", Version: "1.0.0", Description: "execution mode fixture"}, &apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: epoch, UpdatedAt: epoch.Add(time.Second), StatusChangedAt: epoch, IsLatest: true})
	require.NoError(t, err)
	baseURI := fixture.pool.Config().ConnString()
	for _, mode := range []string{"cache_statement", "describe_exec", "exec"} {
		t.Run(mode, func(t *testing.T) {
			uri := baseURI + "&default_query_exec_mode=" + mode
			if mode != "cache_statement" {
				uri += "&statement_cache_capacity=0&description_cache_capacity=0"
			}
			db, openErr := NewPostgreSQLWithPoolLimits(ctx, uri, PoolLimits{MaxConns: 1, MinConns: 0})
			require.NoError(t, openErr)
			defer db.Close()
			version := "1.0.0"
			for _, limit := range []int{1, 30, 100} {
				for _, filter := range []*ServerFilter{{UpdatedSince: &epoch}, {UpdatedSince: &epoch, Version: &version}} {
					for _, cursor := range []string{"", "com.example/aaa:1"} {
						wire, listErr := db.ListServersJSON(ctx, filter, cursor, limit)
						require.NoError(t, listErr)
						encoded, marshalErr := json.Marshal(wire)
						require.NoError(t, marshalErr)
						var response apiv0.ServerListResponse
						require.NoError(t, json.Unmarshal(encoded, &response))
						require.Len(t, response.Servers, 1)
						require.Equal(t, "com.example/mode", response.Servers[0].Server.Name)
						tx, beginErr := db.pool.Begin(ctx)
						require.NoError(t, beginErr)
						rows, _, listErr := db.ListServers(ctx, tx, filter, cursor, limit)
						rollbackErr := tx.Rollback(ctx)
						require.NoError(t, listErr)
						require.NoError(t, rollbackErr)
						require.Len(t, rows, 1)
					}
				}
			}
			if mode == "cache_statement" {
				// Find the actual prepared JSON query used by the default page size.
				// Re-executing it without a format override must use binary bytea.
				var query string
				require.NoError(t, db.pool.QueryRow(ctx, `SELECT statement FROM pg_prepared_statements
					WHERE statement LIKE 'WITH recent AS MATERIALIZED%'
					AND statement LIKE '%listing_value%'
					AND statement LIKE '%LIMIT $2%' AND statement NOT LIKE '%$3%'
					LIMIT 1`).Scan(&query))
				rows, queryErr := db.pool.Query(ctx, query, epoch, 30)
				require.NoError(t, queryErr)
				binaryPayload := false
				for _, field := range rows.FieldDescriptions() {
					if field.DataTypeOID == 17 { // bytea
						binaryPayload = field.Format == pgx.BinaryFormatCode
					}
				}
				rows.Close()
				require.NoError(t, rows.Err())
				require.True(t, binaryPayload, "default limit=30 must retain binary prepared JSON transfer")
			}
		})
	}
}

func TestUpdatedPageDeepCursorAvoidsConsumedTimestampPrefix(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Recent timestamps decrease as names increase: a time-ordered probe for
	// page 451 otherwise filters 45,000 already-consumed names on every call.
	_, err := db.pool.Exec(ctx, `INSERT INTO servers
		(server_name, version, value, status, status_changed_at, published_at, updated_at, is_latest)
		SELECT 'com.example/deep-' || lpad(n::text, 5, '0'), '1.0.0',
		jsonb_build_object('name', 'com.example/deep-' || lpad(n::text, 5, '0'), 'version', '1.0.0'),
		'active', $1::timestamptz, $1::timestamptz, $1::timestamptz + (50001-n)*interval '1 second', true
		FROM generate_series(1,50000) n`, epoch)
	require.NoError(t, err)
	_, err = db.pool.Exec(ctx, "VACUUM ANALYZE servers")
	require.NoError(t, err)
	conn, err := db.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	for _, planMode := range []string{"force_custom_plan", "force_generic_plan"} {
		_, err = conn.Exec(ctx, "SET plan_cache_mode="+planMode)
		require.NoError(t, err)
		for _, limit := range []int{1, 30, 100} {
			cursor := "com.example/deep-45000"
			strategy := db.chooseUpdatedPageStrategy(nil, &ServerFilter{UpdatedSince: &epoch}, limit, []any{cursor, "1.0.0"})
			limitExpression := fmt.Sprint(limit)
			executeArgs := "'2026-01-01T00:00:00Z', 'com.example/deep-45000', '1.0.0'"
			if limit == 30 {
				limitExpression = "$4"
				executeArgs += ", 30"
			}
			query := updatedPageQuery("WHERE updated_at > $1 AND (server_name, version) > ($2, $3)", limitExpression, "s.value", false, strategy)
			_, err = conn.Conn().Prepare(ctx, "deep_cursor", query)
			require.NoError(t, err)
			var raw []byte
			require.NoError(t, conn.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE deep_cursor("+executeArgs+")").Scan(&raw))
			require.NoError(t, conn.Conn().Deallocate(ctx, "deep_cursor"))
			var plans []struct {
				Plan map[string]any `json:"Plan"`
			}
			require.NoError(t, json.Unmarshal(raw, &plans))
			var inspect func(map[string]any)
			inspect = func(node map[string]any) {
				if loops, _ := node["Actual Loops"].(float64); loops > 0 {
					index, _ := node["Index Name"].(string)
					require.False(t, strings.Contains(index, "updated_time") || index == "idx_servers_updated_at", "%s limit=%d unexpectedly executed timestamp scan: %s", planMode, limit, raw)
					removed, _ := node["Rows Removed by Filter"].(float64)
					require.Less(t, removed, float64(1000), "must seek past the consumed prefix: %s", raw)
				}
				children, _ := node["Plans"].([]any)
				for _, child := range children {
					inspect(child.(map[string]any))
				}
			}
			inspect(plans[0].Plan)
			rows, _, listErr := db.ListServers(ctx, nil, &ServerFilter{UpdatedSince: &epoch}, cursor+":1.0.0", limit)
			require.NoError(t, listErr)
			require.Len(t, rows, limit)
			for i, row := range rows {
				require.Equal(t, fmt.Sprintf("com.example/deep-%05d", 45001+i), row.Server.Name)
			}
		}
	}
}
