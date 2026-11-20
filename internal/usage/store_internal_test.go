package usage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pointer[T any](value T) *T {
	return &value
}

// exampleDay is a made-up day. Its figures are arbitrary; several have more
// digits than a float prints by default, to show that they are stored exactly.
func exampleDay(day time.Time, requests int64) (Daily, []Share) {
	daily := Daily{
		Day: day, Requests: requests, GiB: 12.345678901234567, CostUSD: 1.2345678901234568, UserAgents: pointer(41), NamedOrgs: 23,
		NamedRequestsPct: pointer(0.30000000000000004), NamedDataPct: pointer(71.5), ListingsCompressedPct: pointer(33.25),
		CacheHitPct: pointer(42.5), RecordsSampled: 1234, LogRequestsRatio: pointer(1.0000000000000002), LogDataRatio: pointer(0.9876),
		SampleFraction: 0.125, ListingWeightMin: 8.5, ListingWeightMax: 11.25, BackgroundCount: 17, TotalsSource: totalsFromCounters,
		Caveats: []string{"One caveat."},
	}
	shares := []Share{
		{Dimension: "organization", Name: "Litware", Kind: rowNamed, Rank: 1, RequestsPct: pointer(40.123456789012345),
			DataPct: pointer(55.000000000000014), CompressedPct: pointer(0.0), Addresses: pointer(12), IdentifiedBy: pointer("User agent")},
		{Dimension: "organization", Name: "Other named", Kind: rowOther, Rank: 2, RequestsPct: pointer(3.3333333333333335),
			DataPct: pointer(1.0000000000000002), Addresses: pointer(30), RollupCount: pointer(7)},
		{Dimension: "server", Name: "io.github.example/alpha", Kind: rowNamed, Rank: 1, RequestsPct: pointer(0.07),
			DataPct: pointer(0.0025), Addresses: pointer(9), IdentifiedBy: pointer("relecloud-host"), Category: pointer("npm"),
			NotFoundPct: pointer(0.0), TopClientPct: pointer(61.111111111111114)},
	}
	return daily, shares
}

func storedShares(t *testing.T, store *Store, day time.Time) []Share {
	t.Helper()
	rows, err := store.pool.Query(t.Context(), `SELECT dimension, name, kind, rank, requests_pct::float8, data_pct::float8,
		compressed_pct::float8, addresses, identified_by, category, rollup_count, not_found_pct::float8, top_client_pct::float8
		FROM usage_share WHERE day = $1 ORDER BY dimension, rank`, day)
	require.NoError(t, err)
	shares, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Share, error) {
		var out Share
		err := row.Scan(&out.Dimension, &out.Name, &out.Kind, &out.Rank, &out.RequestsPct, &out.DataPct, &out.CompressedPct,
			&out.Addresses, &out.IdentifiedBy, &out.Category, &out.RollupCount, &out.NotFoundPct, &out.TopClientPct)
		return out, err
	})
	require.NoError(t, err)
	return shares
}

func TestSchemaIsAdditiveAndRepeatable(t *testing.T) {
	url := newDatabase(t)
	pool := newPool(t, url)

	// A database that already holds the first version of the tables, with data.
	_, err := pool.Exec(t.Context(), `
		CREATE TABLE usage_daily (day timestamptz PRIMARY KEY, requests bigint, gib numeric, cost_usd numeric, user_agents integer,
			named_orgs integer, named_requests_pct numeric, named_data_pct numeric, listings_compressed_pct numeric,
			listings_compressed_data_pct numeric, cache_hit_pct numeric, records_sampled integer);
		INSERT INTO usage_daily (day, requests, gib) VALUES ('2026-09-01T00:00:00Z', 5000, 0.5)`)
	require.NoError(t, err)

	store := newStore(t, url)
	var requests int64
	var ratio *float64
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT requests, log_requests_ratio::float8 FROM usage_daily").Scan(&requests, &ratio))
	assert.Equal(t, int64(5000), requests, "existing rows survive")
	assert.Nil(t, ratio, "new columns are added empty")

	day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	daily, shares := exampleDay(day, 7000)
	require.NoError(t, store.ReplaceDay(t.Context(), daily, shares, "test", day.Add(26*time.Hour)))

	// Opening the store again, and even running every schema statement again, changes nothing.
	again := newStore(t, url)
	files, err := loadSchema()
	require.NoError(t, err)
	for _, file := range files {
		_, err := pool.Exec(t.Context(), file.sql)
		require.NoError(t, err, file.name)
	}
	var applied, days, rows int
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM usage_schema_migrations), (SELECT count(*) FROM usage_daily),
		(SELECT count(*) FROM usage_share)`).Scan(&applied, &days, &rows))
	assert.Equal(t, len(files), applied)
	assert.Equal(t, 2, days)
	assert.Equal(t, len(shares), rows)
	assert.Equal(t, shares, storedShares(t, again, day))
}

func TestReplaceDay(t *testing.T) {
	store := newStore(t, newDatabase(t))
	first := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	second := first.AddDate(0, 0, 1)
	collected := second.Add(26 * time.Hour)

	daily, shares := exampleDay(first, 100)
	require.NoError(t, store.ReplaceDay(t.Context(), daily, shares, "v1", collected))
	other, otherShares := exampleDay(second, 200)
	require.NoError(t, store.ReplaceDay(t.Context(), other, otherShares, "v1", collected))
	assert.Equal(t, shares, storedShares(t, store, first), "figures are stored exactly")

	var stored struct {
		requests              int64
		gib, ratio, fraction  float64
		agents                *int
		compressedData        *float64
		source, version       string
		caveats               []string
		collectedAt, storedAt time.Time
	}
	read := func(day time.Time) {
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT requests, gib::float8, log_requests_ratio::float8, sample_fraction::float8,
			user_agents, listings_compressed_data_pct::float8, totals_source, collector_version, caveats, collected_at, day
			FROM usage_daily WHERE day = $1`, day).Scan(&stored.requests, &stored.gib, &stored.ratio, &stored.fraction, &stored.agents,
			&stored.compressedData, &stored.source, &stored.version, &stored.caveats, &stored.collectedAt, &stored.storedAt))
	}
	read(first)
	assert.Equal(t, int64(100), stored.requests)
	assert.Equal(t, 12.345678901234567, stored.gib)
	assert.Equal(t, 1.0000000000000002, stored.ratio)
	assert.Equal(t, 0.125, stored.fraction)
	assert.Equal(t, pointer(41), stored.agents)
	assert.Nil(t, stored.compressedData, "a figure that could not be worked out is stored as unknown")
	assert.Equal(t, totalsFromCounters, stored.source)
	assert.Equal(t, []string{"One caveat."}, stored.caveats)
	assert.True(t, stored.collectedAt.Equal(collected))

	t.Run("collecting a day again replaces all of its rows", func(t *testing.T) {
		daily.Requests, daily.UserAgents = 150, nil
		replacement := []Share{{Dimension: "organization", Name: "Someone else", Kind: rowNamed, Rank: 1, RequestsPct: pointer(100.0)}}
		require.NoError(t, store.ReplaceDay(t.Context(), daily, replacement, "v2", collected))
		read(first)
		assert.Equal(t, int64(150), stored.requests)
		assert.Nil(t, stored.agents)
		assert.Equal(t, "v2", stored.version)
		assert.Equal(t, replacement, storedShares(t, store, first))
		assert.Equal(t, otherShares, storedShares(t, store, second), "other days are untouched")
	})

	t.Run("a failed replacement leaves the day as it was", func(t *testing.T) {
		before := storedShares(t, store, second)
		broken := append([]Share{}, otherShares...)
		broken = append(broken, broken[0]) // the same rank twice
		other.Requests = 999
		require.Error(t, store.ReplaceDay(t.Context(), other, broken, "v3", collected))
		read(second)
		assert.Equal(t, int64(200), stored.requests)
		assert.Equal(t, "v1", stored.version)
		assert.Equal(t, before, storedShares(t, store, second))
	})

	t.Run("days without a row are missing", func(t *testing.T) {
		missing, err := store.MissingDays(t.Context(), first.AddDate(0, 0, -2), second.AddDate(0, 0, 2))
		require.NoError(t, err)
		assert.Equal(t, []time.Time{first.AddDate(0, 0, -2), first.AddDate(0, 0, -1), second.AddDate(0, 0, 1)}, missing)
	})
}

func TestOnlyOneRunAtATime(t *testing.T) {
	url := newDatabase(t)
	store, other := newStore(t, url), newStore(t, url)
	release, err := store.Lock(t.Context())
	require.NoError(t, err)
	_, err = other.Lock(t.Context())
	require.ErrorIs(t, err, ErrBusy)
	release()
	release, err = other.Lock(t.Context())
	require.NoError(t, err)
	release()
}

// TestDashboardQueries checks that the view and functions the dashboards call
// work on what the collector stores.
func TestDashboardQueries(t *testing.T) {
	store := newStore(t, newDatabase(t))
	day := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	for offset, requests := range []int64{1000, 3000} {
		daily, shares := exampleDay(day.AddDate(0, 0, offset), requests)
		daily.GiB = float64(requests) / 10
		require.NoError(t, store.ReplaceDay(t.Context(), daily, shares, "test", day.AddDate(0, 0, 3)))
	}
	from, to := day.AddDate(0, 0, -1), day.AddDate(0, 0, 5)

	var days int
	var total float64
	require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT days, requests::float8 FROM usage_buckets('month', $1, $2)", from, to).Scan(&days, &total))
	assert.Equal(t, 2, days)
	assert.InDelta(t, 4000, total, 1e-9)

	var name, kind string
	var requests, bucket float64
	require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT name, kind, requests::float8, bucket_requests::float8
		FROM usage_series('organization', 'month', $1, $2) WHERE pos = 1`, from, to).Scan(&name, &kind, &requests, &bucket))
	assert.Equal(t, "Litware", name)
	assert.InDelta(t, 4000*0.40123456789012345, requests, 1e-6)
	assert.InDelta(t, 4000, bucket, 1e-9)

	rows, err := store.pool.Query(t.Context(), `SELECT name, kind, requests_pct::float8 FROM usage_leaderboard('organization', $1, $2, 10) ORDER BY pos`, from, to)
	require.NoError(t, err)
	type line struct {
		Name, Kind string
		Pct        float64
	}
	board, err := pgx.CollectRows(rows, pgx.RowToStructByPos[line])
	require.NoError(t, err)
	require.Len(t, board, 2)
	assert.Equal(t, "Litware", board[0].Name)
	assert.InDelta(t, 40.123456789012345, board[0].Pct, 1e-9)
	assert.Equal(t, line{Name: "Other", Kind: rowOther, Pct: board[1].Pct}, board[1])
	assert.InDelta(t, 3.3333333333333335, board[1].Pct, 1e-9)
}

// newReader creates a login role with no privileges of its own, as the
// deployment does for the dashboards, and returns its name.
func newReader(t *testing.T, databaseURL string) string {
	t.Helper()
	var suffix [6]byte
	_, err := rand.Read(suffix[:])
	require.NoError(t, err)
	role := "usage reader " + hex.EncodeToString(suffix[:]) // a name that needs quoting
	admin := newPool(t, databaseURL)
	_, err = admin.Exec(t.Context(), "CREATE ROLE "+pgx.Identifier{role}.Sanitize()+" LOGIN PASSWORD 'reader'")
	require.NoError(t, err)
	cleanup := context.WithoutCancel(t.Context())
	t.Cleanup(func() {
		// A role cannot be dropped while it holds privileges in a database.
		_, _ = admin.Exec(cleanup, "DROP OWNED BY "+pgx.Identifier{role}.Sanitize())
		_, _ = admin.Exec(cleanup, "DROP ROLE "+pgx.Identifier{role}.Sanitize())
	})
	return role
}

func asRole(t *testing.T, databaseURL, role string) *pgxpool.Pool {
	t.Helper()
	config, err := pgxpool.ParseConfig(databaseURL)
	require.NoError(t, err)
	config.ConnConfig.User, config.ConnConfig.Password = role, "reader"
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// TestGrantReader checks that the reader role can read everything the
// dashboards query and can change nothing.
func TestGrantReader(t *testing.T) {
	for name, parameters := range map[string]string{"the public schema": "", "a dedicated schema": "&search_path=usage_data"} {
		t.Run(name, func(t *testing.T) {
			base := newDatabase(t)
			_, err := newPool(t, base).Exec(t.Context(), "CREATE SCHEMA usage_data")
			require.NoError(t, err)
			url := base + parameters
			store := newStore(t, url)
			day := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
			daily, shares := exampleDay(day, 7000)
			require.NoError(t, store.ReplaceDay(t.Context(), daily, shares, "test", day.AddDate(0, 0, 1)))

			role := newReader(t, url)
			reader := asRole(t, url, role)
			var count int
			require.Error(t, reader.QueryRow(t.Context(), "SELECT count(*) FROM usage_daily").Scan(&count), "nothing is readable before the grant")

			require.NoError(t, store.GrantReader(t.Context(), role))
			require.NoError(t, store.GrantReader(t.Context(), role), "granting again changes nothing")
			for _, query := range []string{
				"SELECT count(*) FROM usage_daily",
				"SELECT count(*) FROM usage_share",
				"SELECT count(*) FROM publish_version",
				"SELECT count(*) FROM usage_detail",
				"SELECT count(*) FROM usage_buckets('day', '2026-10-01', '2026-10-31')",
				"SELECT count(*) FROM usage_series('organization', 'day', '2026-10-01', '2026-10-31')",
				"SELECT count(*) FROM usage_leaderboard('organization', '2026-10-01', '2026-10-31', 10)",
			} {
				assert.NoError(t, reader.QueryRow(t.Context(), query).Scan(&count), query)
			}
			for _, statement := range []string{
				"INSERT INTO usage_daily (day) VALUES ('2026-10-07')",
				"UPDATE usage_daily SET requests = 0",
				"DELETE FROM usage_share",
				"TRUNCATE publish_version",
				"CREATE TABLE usage_data.scratch (id integer)",
				"SELECT count(*) FROM usage_collector_state",
				"SELECT count(*) FROM usage_schema_migrations",
			} {
				_, err := reader.Exec(t.Context(), statement)
				assert.ErrorContains(t, err, "permission denied", statement)
			}
		})
	}

	t.Run("the role must exist", func(t *testing.T) {
		store := newStore(t, newDatabase(t))
		require.ErrorContains(t, store.GrantReader(t.Context(), "no such role"), `role "no such role" does not exist`)
	})
}

// TestEveryStoredObjectIsClassified makes adding a table, view or function to
// the schema a decision about whether the dashboards' role may read it.
func TestEveryStoredObjectIsClassified(t *testing.T) {
	store := newStore(t, newDatabase(t))
	rows, err := store.pool.Query(t.Context(), `SELECT relname::text FROM pg_class WHERE relnamespace = 'public'::regnamespace AND relkind IN ('r', 'v')
		UNION ALL SELECT proname::text FROM pg_proc WHERE pronamespace = 'public'::regnamespace`)
	require.NoError(t, err)
	created, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	classified := append(append(append([]string{}, readerRelations...), readerFunctions...), internalRelations...)
	assert.ElementsMatch(t, classified, created)
}
