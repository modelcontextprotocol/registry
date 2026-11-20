package usage

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQueriesAreReplacedWhenTheyChange covers a database set up by an earlier
// version, whose view was created by a numbered file and counted a share of
// listings as a share of everything. The current definitions replace it
// without losing what is stored, and are not applied again until they change.
func TestQueriesAreReplacedWhenTheyChange(t *testing.T) {
	url := newDatabase(t)
	pool := newPool(t, url)
	files, _, err := loadSchema()
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), files[0].sql)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		CREATE TABLE usage_schema_migrations (version integer PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now());
		INSERT INTO usage_schema_migrations (version, name) VALUES (1, '001_tables'), (2, '002_dashboard_queries');
		INSERT INTO usage_daily (day, requests, gib) VALUES ('2026-10-06T00:00:00Z', 1000, 10);
		INSERT INTO usage_share (day, dimension, name, kind, rank, requests_pct)
		VALUES ('2026-10-06T00:00:00Z', 'listing_use', 'First page only', 'named', 1, 50);
		CREATE VIEW usage_detail AS
		SELECT s.day, s.dimension, s.name, s.kind, s.identified_by, s.category, s.rollup_count, s.compressed_pct, s.addresses,
		       s.not_found_pct, s.top_client_pct, s.requests_pct / 100 * d.requests AS requests, s.data_pct / 100 * d.gib AS gib
		FROM usage_share s JOIN usage_daily d USING (day)`)
	require.NoError(t, err)

	var requests *float64
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT requests::float8 FROM usage_detail").Scan(&requests))
	require.InDelta(t, 500, *requests, 1e-9, "the earlier view")

	// A second day from that version, stored with its endpoint shares as the collector stores them.
	_, err = pool.Exec(t.Context(), `
		INSERT INTO usage_daily (day, requests, gib, cache_hit_pct, listings_compressed_pct) VALUES ('2026-10-07T00:00:00Z', 2000, 20, 50, 40);
		INSERT INTO usage_share (day, dimension, name, kind, rank, requests_pct, data_pct) VALUES
			('2026-10-07T00:00:00Z', 'endpoint', 'List servers (v0.1)', 'named', 1, 30, 60),
			('2026-10-07T00:00:00Z', 'endpoint', 'List servers (v0)', 'named', 2, 10, 20),
			('2026-10-07T00:00:00Z', 'endpoint', 'Get one server version', 'named', 3, 60, 20),
			('2026-10-07T00:00:00Z', 'listing_use', 'First page only', 'named', 1, 50, 25)`)
	require.NoError(t, err)

	store := newStore(t, url)
	var days int
	require.NoError(t, pool.QueryRow(t.Context(), "SELECT count(*) FROM usage_daily").Scan(&days))
	assert.Equal(t, 2, days, "what was stored is kept")

	// The day with endpoint shares gets its listing totals from them: 40% of 2,000 requests and 80% of 20 GiB.
	var gib *float64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT requests::float8, gib::float8 FROM usage_detail
		WHERE dimension = 'listing_use' AND day = '2026-10-07T00:00:00Z'`).Scan(&requests, &gib))
	assert.InDelta(t, 400, *requests, 1e-9, "50% of 800 listings")
	assert.InDelta(t, 4, *gib, 1e-9, "25% of 16 GiB of listing data")
	// The day without them has no volume for a listing share, rather than a wrong one.
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT requests::float8 FROM usage_detail
		WHERE dimension = 'listing_use' AND day = '2026-10-06T00:00:00Z'`).Scan(&requests))
	assert.Nil(t, requests)

	// A period that includes a day with a gap has no total made of part of its days.
	var bucket, cache, compressed *float64
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT requests::float8, bucket_requests::float8
		FROM usage_series('listing_use', 'month', '2026-10-01', '2026-10-31')`).Scan(&requests, &bucket))
	assert.Nil(t, requests)
	assert.Nil(t, bucket)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT requests::float8, requests_pct::float8
		FROM usage_leaderboard('listing_use', '2026-10-01', '2026-10-31', 10)`).Scan(&requests, &bucket))
	assert.Nil(t, requests)
	assert.Nil(t, bucket)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT requests::float8, bucket_requests::float8
		FROM usage_series('listing_use', 'day', '2026-10-07T00:00:00Z', '2026-10-07T00:00:00Z')`).Scan(&requests, &bucket))
	assert.InDelta(t, 400, *requests, 1e-9, "a period made only of complete days is unaffected")
	assert.InDelta(t, 800, *bucket, 1e-9)
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT requests::float8, bucket_requests::float8
		FROM usage_series('endpoint', 'month', '2026-10-01', '2026-10-31') WHERE name = 'Get one server version'`).Scan(&requests, &bucket))
	assert.InDelta(t, 1200, *requests, 1e-9, "shares of all traffic never depended on the new columns")
	assert.InDelta(t, 3000, *bucket, 1e-9)
	// The cache rate has its base again. The compression rate's base cannot be recovered, so that rate is empty.
	require.NoError(t, pool.QueryRow(t.Context(), `SELECT cache_hit_pct::float8, listings_compressed_pct::float8
		FROM usage_buckets('month', '2026-10-01', '2026-10-31')`).Scan(&cache, &compressed))
	assert.InDelta(t, 50, *cache, 1e-9)
	assert.Nil(t, compressed)

	// Unchanged definitions are not applied again: a function replaced behind the collector's back
	// stays until the stored checksum no longer matches the file.
	const probe = "SELECT cache_hit_pct::float8 FROM usage_buckets('day', '2026-10-01', '2026-10-31')"
	_, err = pool.Exec(t.Context(), `CREATE OR REPLACE FUNCTION usage_buckets(period text, t_from timestamptz, t_to timestamptz)
		RETURNS TABLE (bucket timestamptz, "time" timestamptz, days bigint, days_in_period integer, requests numeric, gib numeric,
		               listings_compressed_pct numeric, listings_compressed_data_pct numeric, cache_hit_pct numeric,
		               named_orgs numeric, user_agents numeric)
		LANGUAGE sql STABLE AS $$
		  SELECT now(), now(), 1::bigint, 1, 0::numeric, 0::numeric, 0::numeric, 0::numeric, 12345::numeric, 0::numeric, 0::numeric
		$$`)
	require.NoError(t, err)
	var marker *float64
	require.NoError(t, store.applySchema(t.Context()))
	require.NoError(t, pool.QueryRow(t.Context(), probe+" LIMIT 1").Scan(&marker))
	assert.Equal(t, pointer(12345.0), marker)
	_, err = pool.Exec(t.Context(), "UPDATE usage_collector_state SET value = 'an earlier checksum' WHERE key = $1", queriesKey)
	require.NoError(t, err)
	require.NoError(t, store.applySchema(t.Context()))
	require.NoError(t, pool.QueryRow(t.Context(), probe+" LIMIT 1").Scan(&marker))
	assert.NotEqual(t, pointer(12345.0), marker, "the file's definition is back")
}

// queryDay stores a made-up day for the tests of the dashboards' arithmetic.
func queryDay(t *testing.T, store *Store, day time.Time, daily Daily, shares []Share) {
	t.Helper()
	daily.Day = day
	ranks := map[string]int{}
	for i := range shares {
		ranks[shares[i].Dimension]++
		shares[i].Rank = ranks[shares[i].Dimension]
	}
	require.NoError(t, store.ReplaceDay(t.Context(), daily, shares, "test", day.AddDate(0, 0, 1)))
}

// TestRatesAreWeightedByTheirOwnBase checks the dashboards' arithmetic on two
// days that differ in how much of their traffic is listings. Every expected
// figure is worked out by hand from the counts in the comments.
func TestRatesAreWeightedByTheirOwnBase(t *testing.T) {
	store := newStore(t, newDatabase(t))
	first := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	from, to := first.AddDate(0, 0, -1), first.AddDate(0, 0, 5)

	// Day one: 1,000 requests, 200 of them listings. 80 of the 100 listing responses with a known
	// encoding were compressed, and 160 of the 200 listings were served from cache.
	queryDay(t, store, first, Daily{Requests: 1000, GiB: 10, TotalsSource: totalsFromCounters,
		ListingRequests: pointer(200.0), ListingGiB: pointer(8.0), CompressionKnownRequests: pointer(100.0), CompressionKnownGiB: pointer(4.0),
		ListingsCompressedPct: pointer(80.0), ListingsCompressedDataPct: pointer(50.0), CacheHitPct: pointer(80.0)},
		[]Share{
			{Dimension: "listing_use", Name: "First page only", Kind: rowNamed, RequestsPct: pointer(50.0), DataPct: pointer(25.0)},
			{Dimension: "endpoint", Name: "List servers (v0.1)", Kind: rowNamed, RequestsPct: pointer(20.0), DataPct: pointer(80.0)},
			// Litware: 60 listing responses with a known encoding (6% of the day), all compressed.
			{Dimension: "client", Name: "Litware", Kind: rowNamed, RequestsPct: pointer(30.0), DataPct: pointer(30.0),
				CompressedPct: pointer(100.0), CompressionKnownPct: pointer(6.0), Addresses: pointer(3)},
			// Adatum makes most of the requests, but only 40 are listing responses with a known encoding; none compressed.
			{Dimension: "client", Name: "Adatum", Kind: rowNamed, RequestsPct: pointer(70.0), DataPct: pointer(70.0),
				CompressedPct: pointer(0.0), CompressionKnownPct: pointer(4.0), Addresses: pointer(5)},
		})
	// Day two: 1,000 requests, 800 of them listings. 80 of the 400 with a known encoding were
	// compressed, and 160 of the 800 were served from cache.
	queryDay(t, store, first.AddDate(0, 0, 1), Daily{Requests: 1000, GiB: 30, TotalsSource: totalsFromCounters,
		ListingRequests: pointer(800.0), ListingGiB: pointer(24.0), CompressionKnownRequests: pointer(400.0), CompressionKnownGiB: pointer(12.0),
		ListingsCompressedPct: pointer(20.0), ListingsCompressedDataPct: pointer(10.0), CacheHitPct: pointer(20.0)},
		[]Share{
			{Dimension: "listing_use", Name: "First page only", Kind: rowNamed, RequestsPct: pointer(25.0), DataPct: pointer(50.0)},
			{Dimension: "endpoint", Name: "List servers (v0.1)", Kind: rowNamed, RequestsPct: pointer(80.0), DataPct: pointer(80.0)},
			// Litware: 400 listing responses with a known encoding (40% of the day), 80 of them compressed.
			{Dimension: "client", Name: "Litware", Kind: rowNamed, RequestsPct: pointer(90.0), DataPct: pointer(90.0),
				CompressedPct: pointer(20.0), CompressionKnownPct: pointer(40.0), Addresses: pointer(4)},
			// Adatum made no listing request with a known encoding, so it has no rate that day.
			{Dimension: "client", Name: "Adatum", Kind: rowNamed, RequestsPct: pointer(10.0), DataPct: pointer(10.0), Addresses: pointer(1)},
		})

	t.Run("a share of listings is a share of listings", func(t *testing.T) {
		var requests, gib float64
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT requests::float8, gib::float8 FROM usage_detail
			WHERE dimension = 'listing_use' AND day = $1`, first).Scan(&requests, &gib))
		assert.InDelta(t, 100, requests, 1e-9, "50% of 200 listings, not of 1,000 requests")
		assert.InDelta(t, 2, gib, 1e-9, "25% of 8 GiB of listing data")
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT requests::float8, gib::float8 FROM usage_detail
			WHERE dimension = 'endpoint' AND day = $1`, first).Scan(&requests, &gib))
		assert.InDelta(t, 200, requests, 1e-9, "20% of all 1,000 requests")
		assert.InDelta(t, 8, gib, 1e-9)

		// The dashboards divide requests by bucket_requests. Over both days: 100 + 200 of 200 + 800 listings.
		var bucket float64
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT requests::float8, bucket_requests::float8
			FROM usage_series('listing_use', 'month', $1, $2)`, from, to).Scan(&requests, &bucket))
		assert.InDelta(t, 300, requests, 1e-9)
		assert.InDelta(t, 1000, bucket, 1e-9)
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT requests::float8, bucket_requests::float8
			FROM usage_series('endpoint', 'month', $1, $2)`, from, to).Scan(&requests, &bucket))
		assert.InDelta(t, 1000, requests, 1e-9)
		assert.InDelta(t, 2000, bucket, 1e-9)

		var requestsPct, dataPct float64
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT requests_pct::float8, data_pct::float8
			FROM usage_leaderboard('listing_use', $1, $2, 10)`, from, to).Scan(&requestsPct, &dataPct))
		assert.InDelta(t, 30, requestsPct, 1e-9, "300 of 1,000 listings")
		assert.InDelta(t, 100*14.0/32, dataPct, 1e-9, "2 + 12 of 8 + 24 GiB of listing data")
	})

	t.Run("a period's rates are weighted by what they are rates of", func(t *testing.T) {
		var compressed, compressedData, cache float64
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT listings_compressed_pct::float8, listings_compressed_data_pct::float8,
			cache_hit_pct::float8 FROM usage_buckets('month', $1, $2)`, from, to).Scan(&compressed, &compressedData, &cache))
		assert.InDelta(t, 32, compressed, 1e-9, "80 + 80 of 100 + 400 responses; weighting by all requests would give 50")
		assert.InDelta(t, 20, compressedData, 1e-9, "2 + 1.2 of 4 + 12 GiB")
		assert.InDelta(t, 32, cache, 1e-9, "160 + 160 of 200 + 800 listings")
	})

	t.Run("a name's compression rate is weighted the same way", func(t *testing.T) {
		rows, err := store.pool.Query(t.Context(), `SELECT name, compressed_pct::float8 FROM usage_leaderboard('client', $1, $2, 10) ORDER BY name`,
			from, to)
		require.NoError(t, err)
		type line struct {
			Name string
			Pct  *float64
		}
		board, err := pgx.CollectRows(rows, pgx.RowToStructByPos[line])
		require.NoError(t, err)
		require.Len(t, board, 2)
		assert.InDelta(t, 0, *board[0].Pct, 1e-9, "Adatum: 0 of 40; the day without a rate adds nothing")
		assert.InDelta(t, 100*140.0/460, *board[1].Pct, 1e-9, "Litware: 60 + 80 of 60 + 400")

		// Keeping no names folds both into one line: 60 + 0 + 80 of 60 + 40 + 400.
		var folded float64
		require.NoError(t, store.pool.QueryRow(t.Context(), `SELECT compressed_pct::float8 FROM usage_leaderboard('client', $1, $2, 0)
			WHERE kind = 'other'`, from, to).Scan(&folded))
		assert.InDelta(t, 28, folded, 1e-9)
	})
}

// TestTopClientIsKnownOrEmpty checks the "asked most by" columns. Only each
// stored row's own winner is known, so a line that combines rows shows a
// winner when the rows agree and nothing when they do not.
func TestTopClientIsKnownOrEmpty(t *testing.T) {
	store := newStore(t, newDatabase(t))
	first := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	from, to := first.AddDate(0, 0, -1), first.AddDate(0, 0, 5)
	lookup := func(name, client string, requestsPct, topPct float64) Share {
		return Share{Dimension: "server", Name: name, Kind: rowNamed, RequestsPct: pointer(requestsPct), DataPct: pointer(0.0),
			IdentifiedBy: pointer(client), TopClientPct: pointer(topPct), NotFoundPct: pointer(0.0), Category: pointer("npm")}
	}
	day := Daily{Requests: 1000, GiB: 1, TotalsSource: totalsFromCounters}
	queryDay(t, store, first, day, []Share{
		lookup("com.example/steady", "Litware", 10, 60),    // 100 lookups, 60 from Litware
		lookup("com.example/changing", "Litware", 10, 100), // 100 lookups, all from Litware
		lookup("com.example/once", "Adatum", 5, 40),
	})
	queryDay(t, store, first.AddDate(0, 0, 1), day, []Share{
		lookup("com.example/steady", "Litware", 30, 90),   // 300 lookups, 270 from Litware
		lookup("com.example/changing", "Adatum", 10, 100), // 100 lookups, all from Adatum
	})

	type line struct {
		Name, Kind string
		Client     *string
		Pct        *float64
	}
	board := func(keep int) map[string]line {
		rows, err := store.pool.Query(t.Context(), `SELECT name, kind, identified_by, top_client_pct::float8
			FROM usage_leaderboard('server', $1, $2, $3, false, true)`, from, to, keep)
		require.NoError(t, err)
		lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[line])
		require.NoError(t, err)
		byName := map[string]line{}
		for _, found := range lines {
			byName[found.Name] = found
		}
		return byName
	}

	lines := board(10)
	require.Len(t, lines, 3)
	assert.Equal(t, pointer("Litware"), lines["com.example/steady"].Client, "the same winner on both days is the winner overall")
	assert.InDelta(t, 82.5, *lines["com.example/steady"].Pct, 1e-9, "60 + 270 of 100 + 300 lookups")
	assert.Nil(t, lines["com.example/changing"].Client, "each client made half of the lookups, so neither day's winner is the answer")
	assert.Nil(t, lines["com.example/changing"].Pct, "and 100%, the average of the two days, would be wrong")
	assert.Equal(t, pointer("Adatum"), lines["com.example/once"].Client, "a single stored row is shown as it is")
	assert.InDelta(t, 40, *lines["com.example/once"].Pct, 1e-9)

	// Keeping one name folds the other two into one line, whose rows have different winners.
	lines = board(1)
	require.Len(t, lines, 2)
	assert.Equal(t, pointer("Litware"), lines["com.example/steady"].Client)
	assert.Equal(t, rowOther, lines["Other"].Kind)
	assert.Nil(t, lines["Other"].Client)
	assert.Nil(t, lines["Other"].Pct)

	// Other dimensions keep saying how a name was identified.
	queryDay(t, store, first.AddDate(0, 0, 2), day, []Share{
		{Dimension: "organization", Name: "Litware", Kind: rowNamed, RequestsPct: pointer(10.0), DataPct: pointer(10.0), IdentifiedBy: pointer("User agent")},
	})
	var how string
	require.NoError(t, store.pool.QueryRow(t.Context(), "SELECT identified_by FROM usage_leaderboard('organization', $1, $2, 10)", from, to).Scan(&how))
	assert.Equal(t, "User agent", how)
}
