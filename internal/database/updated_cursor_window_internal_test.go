package database

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdatedCursorWindowMatchesReferenceAcrossBoundaries(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := db.pool.Exec(ctx, `INSERT INTO servers
 (server_name,version,value,status,published_at,updated_at,status_changed_at,is_latest)
 SELECT 'com.example/window-'||lpad((n/2)::text,5,'0'), '1.0.'||(n%2)::text,
 jsonb_build_object('name','com.example/window-'||lpad((n/2)::text,5,'0'),'version','1.0.'||(n%2)::text),
 CASE WHEN n%11=0 THEN 'deleted' ELSE 'active' END, $1::timestamptz,
 $1::timestamptz + (n%17)*interval '1 second',$1::timestamptz,n%2=1
 FROM generate_series(0,2047) n`, epoch)
	require.NoError(t, err)
	check := func() {
		for _, deleted := range []bool{false, true} {
			for _, second := range []int{0, 15, 16, 17} {
				since := epoch.Add(time.Duration(second) * time.Second)
				for _, offset := range []int{0, 511, 512, 1535, 2047} {
					name, version := fmt.Sprintf("com.example/window-%05d", offset/2), fmt.Sprintf("1.0.%d", offset%2)
					for _, limit := range []int{1, 30, 100, 600} {
						var want []string
						rows, e := db.pool.Query(ctx, `SELECT server_name||':'||version FROM servers
        WHERE updated_at>$1 AND (server_name,version)>($2,$3) AND ($4 OR status!='deleted')
        ORDER BY server_name,version LIMIT $5`, since, name, version, deleted, limit)
						require.NoError(t, e)
						for rows.Next() {
							var key string
							require.NoError(t, rows.Scan(&key))
							want = append(want, key)
						}
						require.NoError(t, rows.Err())
						rows.Close()
						got, _, e := db.ListServers(ctx, nil, &ServerFilter{UpdatedSince: &since, IncludeDeleted: &deleted}, name+":"+version, limit)
						require.NoError(t, e)
						var keys []string
						for _, row := range got {
							keys = append(keys, row.Server.Name+":"+row.Server.Version)
						}
						require.Equal(t, want, keys, "offset=%d cutoff=%d limit=%d deleted=%v", offset, second, limit, deleted)
					}
				}
			}
		}
	}
	check()
	// Changing rows across the window boundary must not leave stale responses.
	_, err = db.pool.Exec(ctx, `UPDATE servers SET updated_at=$1 WHERE server_name IN ('com.example/window-00255','com.example/window-00256')`, epoch.Add(time.Minute))
	require.NoError(t, err)
	_, err = db.pool.Exec(ctx, `DELETE FROM servers WHERE server_name='com.example/window-00768'`)
	require.NoError(t, err)
	check()
}

func TestUpdatedCursorWindowReusesPlansForChangingBounds(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, err := db.pool.Exec(ctx, `INSERT INTO servers(server_name,version,value,status,published_at,updated_at,status_changed_at,is_latest)
 SELECT 'com.example/plans-'||lpad(n::text,5,'0'),'1.0.0',jsonb_build_object('name','com.example/plans-'||lpad(n::text,5,'0'),'version','1.0.0'),
 'active',$1::timestamptz,$1::timestamptz+(n%17)*interval '1 second',$1::timestamptz,true FROM generate_series(1,2048) n`, epoch)
	require.NoError(t, err)
	conn, err := db.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	for _, nameOnly := range []bool{false, true} {
		for _, limit := range []int{1, 100} {
			where := "WHERE updated_at > $1 AND (server_name, version) > ($2, $3)"
			if nameOnly {
				where = "WHERE updated_at > $1 AND server_name > $2"
			}
			query := updatedCursorPageQuery(where, fmt.Sprint(limit), "s.value")
			for i := 0; i < 64; i++ {
				name := fmt.Sprintf("com.example/plans-%05d", (i*73)%2048)
				args := []any{epoch.Add(time.Duration(i%18) * time.Second), name}
				if !nameOnly {
					args = append(args, "1.0.0")
				}
				rows, e := conn.Query(ctx, query, args...)
				require.NoError(t, e)
				rows.Close()
				require.NoError(t, rows.Err())
			}
			var generic, custom int
			require.NoError(t, conn.QueryRow(ctx, "SELECT generic_plans,custom_plans FROM pg_prepared_statements WHERE statement=$1", query).Scan(&generic, &custom))
			require.Positive(t, generic, "nameOnly=%v limit=%d", nameOnly, limit)
			require.Less(t, custom, 64)
		}
	}
}
