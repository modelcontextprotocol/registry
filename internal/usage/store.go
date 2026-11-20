package usage

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema/*.sql
var schemaFiles embed.FS

const (
	// Advisory lock keys. Any two fixed numbers that nothing else uses will do.
	schemaLockKey int64 = 0x7573616765_01
	runLockKey    int64 = 0x7573616765_02

	watermarkKey = "publishing_watermark"
)

// ErrBusy means another collector run holds the lock on the usage database.
var ErrBusy = errors.New("another usage collector run is in progress")

// Store is the database the dashboards read: one row per day, that day's
// breakdowns, and a copy of what has been published.
type Store struct {
	pool *pgxpool.Pool
}

// OpenStore connects to the usage database and brings its schema up to date.
// Schema changes only ever add; nothing existing is dropped or rewritten.
func OpenStore(ctx context.Context, url string) (*Store, error) {
	pool, err := openPool(ctx, url, 4)
	if err != nil {
		return nil, fmt.Errorf("usage database: %w", err)
	}
	store := &Store{pool: pool}
	if err := store.applySchema(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("usage database schema: %w", err)
	}
	return store, nil
}

// openPool opens a small pool. Errors never repeat the URL, which may hold a password.
func openPool(ctx context.Context, url string, connections int32) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("the connection URL is not valid")
	}
	config.MaxConns = connections
	config.ConnConfig.RuntimeParams["application_name"] = "registry-usage-collector"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("the connection could not be set up")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("could not connect: %w", err)
	}
	return pool, nil
}

// Close releases the store's connections.
func (s *Store) Close() {
	s.pool.Close()
}

type schemaFile struct {
	version int
	name    string
	sql     string
}

func loadSchema() ([]schemaFile, error) {
	found, err := schemaFiles.ReadDir("schema")
	if err != nil {
		return nil, err
	}
	var files []schemaFile
	for _, item := range found {
		name := strings.TrimSuffix(item.Name(), ".sql")
		number, _, ok := strings.Cut(name, "_")
		version, err := strconv.Atoi(number)
		if !ok || err != nil {
			return nil, fmt.Errorf("schema file %s is not named <number>_<name>.sql", item.Name())
		}
		sql, err := schemaFiles.ReadFile("schema/" + item.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, schemaFile{version: version, name: name, sql: string(sql)})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].version < files[j].version })
	return files, nil
}

// applySchema runs the schema files that have not been applied yet. Every
// statement in them is written so that running it again changes nothing, and
// the whole step holds a lock so that two collectors starting together do not
// collide.
func (s *Store) applySchema(ctx context.Context) error {
	files, err := loadSchema()
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", schemaLockKey); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS usage_schema_migrations (
		version integer PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT version FROM usage_schema_migrations")
	if err != nil {
		return err
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return err
	}
	done := map[int]bool{}
	for _, version := range applied {
		done[version] = true
	}
	for _, file := range files {
		if done[file.version] {
			continue
		}
		if _, err := tx.Exec(ctx, file.sql); err != nil {
			return fmt.Errorf("%s: %w", file.name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO usage_schema_migrations (version, name) VALUES ($1, $2)", file.version, file.name); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Lock keeps other collector runs out until the returned function is called.
// It returns ErrBusy if another run already holds the lock.
func (s *Store) Lock(ctx context.Context) (func(), error) {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	var held bool
	if err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", runLockKey).Scan(&held); err != nil {
		conn.Release()
		return nil, err
	}
	if !held {
		conn.Release()
		return nil, ErrBusy
	}
	return func() {
		// The run's context may already be cancelled; the lock is still released.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", runLockKey)
		conn.Release()
	}, nil
}

// What the dashboards read and call. Everything else the schema creates is the
// collector's own bookkeeping, which a reader has no use for.
var (
	readerRelations   = []string{"usage_daily", "usage_share", "publish_version", "usage_detail"}
	readerFunctions   = []string{"usage_buckets", "usage_series", "usage_leaderboard"}
	internalRelations = []string{"usage_collector_state", "usage_schema_migrations"}
)

// GrantReader gives an existing role read-only access to what the dashboards
// query: SELECT on the tables and the view, EXECUTE on the functions, and use
// of the schema they are in. It can be repeated, and it never takes anything
// away from the role.
func (s *Store) GrantReader(ctx context.Context, role string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("role %q does not exist; the collector grants access to a role but does not create it", role)
	}
	var schema string
	var schemaID uint32
	if err := tx.QueryRow(ctx, `SELECT n.nspname, n.oid FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE c.oid = to_regclass($1)`, readerRelations[0]).Scan(&schema, &schemaID); err != nil {
		return err
	}
	grantee := pgx.Identifier{role}.Sanitize()

	// By default every role may use the public schema; a dedicated one needs a grant.
	canUse := func() (allowed bool, err error) {
		err = tx.QueryRow(ctx, "SELECT has_schema_privilege($1, $2::oid, 'USAGE')", role, schemaID).Scan(&allowed)
		return allowed, err
	}
	allowed, err := canUse()
	if err != nil {
		return err
	}
	if !allowed {
		if _, err := tx.Exec(ctx, "GRANT USAGE ON SCHEMA "+pgx.Identifier{schema}.Sanitize()+" TO "+grantee); err != nil {
			return err
		}
		// A grant the collector is not entitled to make is skipped with a warning, not an error.
		if allowed, err = canUse(); err != nil {
			return err
		}
		if !allowed {
			return fmt.Errorf("role %q may not use schema %q, and the collector is not allowed to grant that", role, schema)
		}
	}
	for _, relation := range readerRelations {
		if _, err := tx.Exec(ctx, "GRANT SELECT ON "+pgx.Identifier{schema, relation}.Sanitize()+" TO "+grantee); err != nil {
			return fmt.Errorf("%s: %w", relation, err)
		}
	}
	rows, err := tx.Query(ctx, `SELECT p.oid::regprocedure::text FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = $1 AND p.proname = ANY($2)`, schema, readerFunctions)
	if err != nil {
		return err
	}
	// Each is a function's name with its argument types, quoted by the database.
	signatures, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	if len(signatures) < len(readerFunctions) {
		return errors.New("the dashboard functions are missing from the schema")
	}
	for _, signature := range signatures {
		if _, err := tx.Exec(ctx, "GRANT EXECUTE ON FUNCTION "+signature+" TO "+grantee); err != nil {
			return fmt.Errorf("%s: %w", signature, err)
		}
	}
	return tx.Commit(ctx)
}

// StoredDays returns the days in [from, to) that already have a row.
func (s *Store) StoredDays(ctx context.Context, from, to time.Time) (map[time.Time]bool, error) {
	rows, err := s.pool.Query(ctx, "SELECT day FROM usage_daily WHERE day >= $1 AND day < $2", from, to)
	if err != nil {
		return nil, err
	}
	days, err := pgx.CollectRows(rows, pgx.RowTo[time.Time])
	if err != nil {
		return nil, err
	}
	stored := make(map[time.Time]bool, len(days))
	for _, day := range days {
		stored[day.UTC()] = true
	}
	return stored, nil
}

// MissingDays lists the full UTC days in [from, to) that have no row, oldest first.
func (s *Store) MissingDays(ctx context.Context, from, to time.Time) ([]time.Time, error) {
	stored, err := s.StoredDays(ctx, from, to)
	if err != nil {
		return nil, err
	}
	var missing []time.Time
	for day := from; day.Before(to); day = day.AddDate(0, 0, 1) {
		if !stored[day] {
			missing = append(missing, day)
		}
	}
	return missing, nil
}

// number prepares a figure for a numeric column. The exact shortest decimal
// form of the value is stored, so reading it back gives the same number.
func number(value *float64) any {
	if value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	return *value
}

// ReplaceDay stores a day, replacing whatever was stored for it before. The
// day's totals and breakdowns change together or not at all.
func (s *Store) ReplaceDay(ctx context.Context, daily Daily, shares []Share, collectorVersion string, collectedAt time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "DELETE FROM usage_share WHERE day = $1", daily.Day); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM usage_daily WHERE day = $1", daily.Day); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO usage_daily (day, requests, gib, cost_usd, user_agents, named_orgs, named_requests_pct,
			named_data_pct, listings_compressed_pct, listings_compressed_data_pct, cache_hit_pct, records_sampled,
			log_requests_ratio, log_data_ratio, sample_fraction, listing_weight_min, listing_weight_max, background_records,
			totals_source, caveats, collected_at, collector_version)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		daily.Day, daily.Requests, number(&daily.GiB), number(&daily.CostUSD), daily.UserAgents, daily.NamedOrgs,
		number(daily.NamedRequestsPct), number(daily.NamedDataPct), number(daily.ListingsCompressedPct),
		number(daily.ListingsCompressedDataPct), number(daily.CacheHitPct), daily.RecordsSampled,
		number(daily.LogRequestsRatio), number(daily.LogDataRatio), number(&daily.SampleFraction),
		number(&daily.ListingWeightMin), number(&daily.ListingWeightMax), daily.BackgroundCount,
		daily.TotalsSource, daily.Caveats, collectedAt, collectorVersion)
	if err != nil {
		return err
	}
	columns := []string{"day", "dimension", "name", "kind", "rank", "requests_pct", "data_pct", "compressed_pct", "addresses",
		"identified_by", "category", "rollup_count", "not_found_pct", "top_client_pct"}
	_, err = tx.CopyFrom(ctx, pgx.Identifier{"usage_share"}, columns, pgx.CopyFromSlice(len(shares), func(i int) ([]any, error) {
		row := shares[i]
		return []any{daily.Day, row.Dimension, row.Name, row.Kind, int32(row.Rank), //nolint:gosec // G115: a rank is a small count
			number(row.RequestsPct), number(row.DataPct), number(row.CompressedPct), row.Addresses, row.IdentifiedBy, row.Category,
			row.RollupCount, number(row.NotFoundPct), number(row.TopClientPct)}, nil
	}))
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Catalogue returns the published server names and the type of each one's
// latest version, as last copied from the registry database.
func (s *Store) Catalogue(ctx context.Context) (*Catalogue, error) {
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (name) name, type_label FROM publish_version
		ORDER BY name, is_latest DESC, published_at DESC, version DESC`)
	if err != nil {
		return nil, err
	}
	servers, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (CatalogueServer, error) {
		var server CatalogueServer
		err := row.Scan(&server.Name, &server.Type)
		return server, err
	})
	if err != nil {
		return nil, err
	}
	// A fixed order, independent of the database's collation, decides which of
	// two names that differ only by case answers an inexact request.
	sort.Slice(servers, func(i, j int) bool { return servers[i].Name < servers[j].Name })
	return NewCatalogue(servers), nil
}

func (s *Store) publishingWatermark(ctx context.Context) (time.Time, bool, error) {
	var value string
	err := s.pool.QueryRow(ctx, "SELECT value FROM usage_collector_state WHERE key = $1", watermarkKey).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	mark, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("stored publishing watermark is not a time: %w", err)
	}
	return mark, true, nil
}

func (s *Store) forgetPublishingWatermark(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM usage_collector_state WHERE key = $1", watermarkKey)
	return err
}

// savePublishing stores one page of changed versions, refreshes the state of
// their sibling versions, and moves the watermark, all together.
func (s *Store) savePublishing(ctx context.Context, changed []registryVersion, siblings []versionState, mark time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	batch := &pgx.Batch{}
	names := map[string]bool{}
	for _, version := range changed {
		names[version.Name] = true
		namespace := namespaceOf(version.Name)
		batch.Queue(`INSERT INTO publish_version (name, version, namespace, namespace_kind, published_at, status, is_latest,
				first_for_server, type_label, offers)
			VALUES ($1, $2, $3, $4, $5, $6, $7, false, $8, $9)
			ON CONFLICT (name, version) DO UPDATE SET namespace = EXCLUDED.namespace, namespace_kind = EXCLUDED.namespace_kind,
				published_at = EXCLUDED.published_at, status = EXCLUDED.status, is_latest = EXCLUDED.is_latest,
				type_label = EXCLUDED.type_label, offers = EXCLUDED.offers`,
			version.Name, version.Version, namespace, namespaceKind(namespace), version.PublishedAt, version.Status, version.IsLatest,
			typeLabel(version.Packages, version.Remotes), offerLabels(version.Packages, version.Remotes))
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}

	// The registry moves the "latest" flag between versions of a server without
	// marking the other versions as changed, so their state is refreshed here.
	siblingNames, siblingVersions := make([]string, len(siblings)), make([]string, len(siblings))
	latest, statuses := make([]bool, len(siblings)), make([]string, len(siblings))
	for i, sibling := range siblings {
		siblingNames[i], siblingVersions[i], latest[i], statuses[i] = sibling.Name, sibling.Version, sibling.IsLatest, sibling.Status
	}
	if _, err := tx.Exec(ctx, `UPDATE publish_version p SET is_latest = s.is_latest, status = s.status
		FROM unnest($1::text[], $2::text[], $3::boolean[], $4::text[]) AS s(name, version, is_latest, status)
		WHERE p.name = s.name AND p.version = s.version
		  AND (p.is_latest IS DISTINCT FROM s.is_latest OR p.status IS DISTINCT FROM s.status)`,
		siblingNames, siblingVersions, latest, statuses); err != nil {
		return err
	}

	touched := make([]string, 0, len(names))
	for name := range names {
		touched = append(touched, name)
	}
	if _, err := tx.Exec(ctx, `WITH first AS (
			SELECT DISTINCT ON (name) name, version FROM publish_version WHERE name = ANY($1)
			ORDER BY name, published_at, version COLLATE "C")
		UPDATE publish_version p SET first_for_server = (p.version = first.version)
		FROM first
		WHERE p.name = first.name AND p.first_for_server IS DISTINCT FROM (p.version = first.version)`, touched); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO usage_collector_state (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
		watermarkKey, mark.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
