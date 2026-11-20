package database

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/modelcontextprotocol/registry/internal/telemetry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestUpdatedPageHintsNeverCacheRows(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	yes := true
	for i := 0; i < 540; i++ {
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: fmt.Sprintf("com.example/hint-%03d", i), Version: "1.0.0", Description: "hint fixture"}, &apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: epoch, UpdatedAt: epoch.Add(time.Second), StatusChangedAt: epoch, IsLatest: true})
		require.NoError(t, err)
	}
	// The private observation marker relies on this database invariant, even
	// when a writer bypasses CreateServer's own name validation.
	_, invalidNameErr := db.pool.Exec(ctx, "UPDATE servers SET server_name='' WHERE server_name='com.example/hint-000'")
	require.ErrorContains(t, invalidNameErr, "check_server_name_format")
	f := &ServerFilter{UpdatedSince: &epoch, IncludeDeleted: &yes}
	read := func() ([]apiv0.ServerResponse, error) {
		raw, err := db.ListServersJSON(ctx, f, "", 100)
		if err != nil {
			return nil, err
		}
		b, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		var out apiv0.ServerListResponse
		err = json.Unmarshal(b, &out)
		return out.Servers, err
	}
	page, err := read()
	require.NoError(t, err)
	require.Len(t, page, 100)
	db.updatedPageHints.mu.Lock()
	require.Len(t, db.updatedPageHints.entries, 1)
	for k := range db.updatedPageHints.entries {
		h := db.updatedPageHints.entries[k]
		h.expires = time.Now().Add(time.Hour)
		db.updatedPageHints.entries[k] = h
	}
	db.updatedPageHints.mu.Unlock()
	_, err = db.pool.Exec(ctx, "UPDATE servers SET updated_at=$1", epoch)
	require.NoError(t, err)
	page, err = read()
	require.NoError(t, err)
	require.Empty(t, page)
	_, err = db.pool.Exec(ctx, "UPDATE servers SET updated_at=$1 WHERE server_name < 'com.example/hint-030'", epoch.Add(time.Second))
	require.NoError(t, err)
	page, err = read()
	require.NoError(t, err)
	require.Len(t, page, 30)
	rows, _, err := db.ListServers(ctx, nil, f, "", 100)
	require.NoError(t, err)
	require.Len(t, rows, 30)
	// Concurrent requests exercise the advisory map while all results stay current.
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				r, e := read()
				if e != nil {
					errs <- e
					return
				}
				if len(r) != 30 {
					errs <- fmt.Errorf("rows=%d", len(r))
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
	db.updatedPageHints.mu.Lock()
	for k := range db.updatedPageHints.entries {
		h := db.updatedPageHints.entries[k]
		h.expires = time.Now().Add(-time.Second)
		db.updatedPageHints.entries[k] = h
	}
	db.updatedPageHints.mu.Unlock()
	page, err = read()
	require.NoError(t, err)
	require.Len(t, page, 30)
	_, err = db.pool.Exec(ctx, "UPDATE servers SET updated_at=$1", epoch.Add(time.Second))
	require.NoError(t, err)
	page, err = read()
	require.NoError(t, err)
	require.Len(t, page, 100)
}

// A moving sync watermark and cursor must not turn an algorithm hint into cached data.
func TestUpdatedPageHintsMovingWatermarkAndBoundedKeys(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	stamp := epoch.Add(time.Second)
	yes := true
	for i := 0; i < 540; i++ {
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: fmt.Sprintf("com.example/range-%03d", i), Version: "1.0.0", Description: "range fixture"}, &apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: epoch, UpdatedAt: stamp, StatusChangedAt: epoch, IsLatest: true})
		require.NoError(t, err)
	}
	read := func(since time.Time, cursor string, include bool) int {
		f := &ServerFilter{UpdatedSince: &since, IncludeDeleted: &include}
		raw, err := db.ListServersJSON(ctx, f, cursor, 100)
		require.NoError(t, err)
		b, err := json.Marshal(raw)
		require.NoError(t, err)
		var response apiv0.ServerListResponse
		require.NoError(t, json.Unmarshal(b, &response))
		return len(response.Servers)
	}
	require.Equal(t, 100, read(epoch, "", yes))
	require.Equal(t, 100, read(epoch.Add(time.Millisecond), "", yes))
	require.Equal(t, 100, read(epoch, "com.example/aaa", yes), "legacy name-only cursor")
	require.Equal(t, 39, read(epoch, "com.example/range-500", yes))
	require.Equal(t, 0, read(stamp, "", yes))
	require.Equal(t, 0, read(stamp.Add(time.Second), "", yes))
	require.Equal(t, 39, read(epoch, "com.example/range-500:1.0.0", yes))
	_, err := db.pool.Exec(ctx, "UPDATE servers SET updated_at=$1 WHERE server_name < 'com.example/range-040'", stamp.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, 40, read(stamp, "", yes))
	_, err = db.pool.Exec(ctx, "UPDATE servers SET status='deleted' WHERE server_name < 'com.example/range-520'")
	require.NoError(t, err)
	require.Equal(t, 20, read(epoch, "", false))
	require.Equal(t, 100, read(epoch, "", yes))
	// Arbitrary valid cursor parameters cannot grow the advisory map without bound.
	for i := 0; i < 540; i++ {
		require.Equal(t, 100, read(epoch, fmt.Sprintf("com.example/aaa:%06d", i), yes))
	}
	db.updatedPageHints.mu.Lock()
	require.LessOrEqual(t, len(db.updatedPageHints.entries), 32)
	for k, h := range db.updatedPageHints.entries {
		h.expires = time.Now().Add(-time.Second)
		db.updatedPageHints.entries[k] = h
	}
	db.updatedPageHints.mu.Unlock()
	for i := 0; i < 16; i++ {
		require.Equal(t, 100, read(epoch, "", yes))
	}
	db.updatedPageHints.mu.Lock()
	require.Len(t, db.updatedPageHints.entries, 1)
	db.updatedPageHints.mu.Unlock()
}

// Cursor seeks must still reuse plans and apply changing watermarks/cursors.
func TestUpdatedPageCursorReusesPlanAndChecksPredicate(t *testing.T) {
	db := NewTestDB(t).(*PostgreSQL)
	ctx := context.Background()
	epoch := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 540; i++ {
		_, err := db.CreateServer(ctx, nil, &apiv0.ServerJSON{Name: fmt.Sprintf("com.example/guard-%03d", i), Version: "1.0.0", Description: "guard fixture"}, &apiv0.RegistryExtensions{Status: model.StatusActive, PublishedAt: epoch, UpdatedAt: epoch.Add(time.Second), StatusChangedAt: epoch, IsLatest: true})
		require.NoError(t, err)
	}
	conn, err := db.pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	strategy := updatedPageStrategy{cursorProbe: true}
	query := updatedPageQuery("WHERE updated_at > $1 AND (server_name, version) > ($2, $3)", "100", "s.value", false, strategy)
	for i := 0; i < 24; i++ {
		rows, err := conn.Query(ctx, query, pgx.QueryExecModeCacheStatement, epoch.Add(time.Duration(i)*time.Millisecond), "com.example/aaa", fmt.Sprint(i))
		require.NoError(t, err)
		page, err := readListingRows(ctx, rows, telemetry.ListingFromContext(ctx), false, nil)
		require.NoError(t, err)
		require.Len(t, page, 100)
		require.Equal(t, "com.example/guard-000", page[0].name)
	}
	var generic, custom int
	require.NoError(t, conn.QueryRow(ctx, "SELECT generic_plans, custom_plans FROM pg_prepared_statements WHERE statement = $1", query).Scan(&generic, &custom))
	require.Positive(t, generic, "cursor seek must permit generic-plan reuse")
	require.Less(t, custom, 24)
	// Later cursor pages still return the complete remaining result.
	rows, err := conn.Query(ctx, query, pgx.QueryExecModeCacheStatement, epoch, "com.example/guard-530", "1.0.0")
	require.NoError(t, err)
	page, err := readListingRows(ctx, rows, telemetry.ListingFromContext(ctx), false, nil)
	require.NoError(t, err)
	require.Len(t, page, 9)
	require.Equal(t, "com.example/guard-531", page[0].name)
	// An explicit transaction bypasses hints even when the cache is populated.
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	defer func() { require.NoError(t, tx.Rollback(ctx)) }()
	db.updatedPageHints.remember(time.Now(), updatedPageObservation{large: true, before: epoch.Add(time.Second), nameBefore: "com.example/guard-000"}, false)
	before := db.updatedPageHints.requests
	_, _, err = db.ListServers(ctx, tx, &ServerFilter{UpdatedSince: &epoch}, "", 100)
	require.NoError(t, err)
	require.Equal(t, before, db.updatedPageHints.requests)
}
