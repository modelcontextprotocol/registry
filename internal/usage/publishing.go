package usage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	// publishingOverlap is how far behind the watermark a run starts reading,
	// to pick up versions whose transaction committed after an earlier run had
	// already read past their timestamp.
	publishingOverlap = 10 * time.Minute
	// Each page is one short read-only transaction, so a run never holds a
	// long snapshot open on the registry database.
	publishingPageSize    = 500
	publishingPagePause   = 200 * time.Millisecond
	publishingPageTimeout = "15s"
)

// registryVersion is one row of the registry's servers table, reduced to what
// the dashboards need. Packages and Remotes are the distinct types it offers.
type registryVersion struct {
	Name        string
	Version     string
	PublishedAt time.Time
	UpdatedAt   time.Time
	Status      string
	IsLatest    bool
	Packages    []string
	Remotes     []string
}

type versionState struct {
	Name     string
	Version  string
	IsLatest bool
	Status   string
}

// Registry reads published versions from the registry database. It only ever
// reads, in small pages, and extracts what it needs on the server so that the
// stored documents are not transferred.
type Registry struct {
	pool *pgxpool.Pool
}

// OpenRegistry connects to the registry database with a single connection.
func OpenRegistry(ctx context.Context, url string) (*Registry, error) {
	pool, err := openPool(ctx, url, 1)
	if err != nil {
		return nil, fmt.Errorf("registry database: %w", err)
	}
	return &Registry{pool: pool}, nil
}

// Close releases the connection.
func (r *Registry) Close() {
	r.pool.Close()
}

// readOnly runs fn in a read-only transaction with a statement timeout.
func (r *Registry) readOnly(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '"+publishingPageTimeout+"'"); err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// The types are extracted in SQL. The ordering matches the index on
// (updated_at DESC, server_name, version) read backwards, so a page is one
// index range scan.
const changedVersionsQuery = `
SELECT s.server_name, s.version, s.published_at, s.updated_at, s.status, s.is_latest,
       ARRAY(SELECT DISTINCT lower(coalesce(nullif(p->>'registryType', ''), 'unknown'))
             FROM jsonb_array_elements(CASE WHEN jsonb_typeof(s.value->'packages') = 'array' THEN s.value->'packages' ELSE '[]'::jsonb END) AS p),
       ARRAY(SELECT DISTINCT lower(coalesce(nullif(r->>'type', ''), 'unknown'))
             FROM jsonb_array_elements(CASE WHEN jsonb_typeof(s.value->'remotes') = 'array' THEN s.value->'remotes' ELSE '[]'::jsonb END) AS r)
FROM servers s
WHERE %s
ORDER BY s.updated_at, s.server_name DESC, s.version DESC
LIMIT %d`

// changedAfter returns the next page of versions changed after the cursor,
// oldest change first. A cursor without a name starts after its time.
func (r *Registry) changedAfter(ctx context.Context, after registryVersion, limit int) ([]registryVersion, error) {
	condition, arguments := "s.updated_at > $1", []any{after.UpdatedAt}
	if after.Name != "" {
		condition = "s.updated_at >= $1 AND (s.updated_at > $1 OR (s.server_name, s.version) < ($2, $3))"
		arguments = append(arguments, after.Name, after.Version)
	}
	var page []registryVersion
	err := r.readOnly(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, fmt.Sprintf(changedVersionsQuery, condition, limit), arguments...)
		if err != nil {
			return err
		}
		page, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (registryVersion, error) {
			var version registryVersion
			err := row.Scan(&version.Name, &version.Version, &version.PublishedAt, &version.UpdatedAt, &version.Status,
				&version.IsLatest, &version.Packages, &version.Remotes)
			sort.Strings(version.Packages)
			sort.Strings(version.Remotes)
			return version, err
		})
		return err
	})
	return page, err
}

// states returns the current "latest" flag and status of every version of the
// named servers. It reads only an index.
func (r *Registry) states(ctx context.Context, names []string) ([]versionState, error) {
	var states []versionState
	err := r.readOnly(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT server_name, version, is_latest, status FROM servers WHERE server_name = ANY($1)", names)
		if err != nil {
			return err
		}
		states, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (versionState, error) {
			var state versionState
			err := row.Scan(&state.Name, &state.Version, &state.IsLatest, &state.Status)
			return state, err
		})
		return err
	})
	return states, err
}

// PublishingResult summarises a catalogue sync.
type PublishingResult struct {
	Changed int
	Pages   int
	Full    bool
}

// PublishingOptions tune a catalogue sync. The zero value reads changed
// versions in pages of 500 with a short pause between pages.
type PublishingOptions struct {
	// Resync reads every version again instead of only the changed ones.
	Resync   bool
	PageSize int
	Pause    time.Duration
}

// SyncPublishing copies versions that changed since the last run from the
// registry database into the store. The first run, or a run with Resync set,
// reads every version once.
func SyncPublishing(ctx context.Context, registry *Registry, store *Store, options PublishingOptions) (PublishingResult, error) {
	var result PublishingResult
	if options.PageSize <= 0 {
		options.PageSize = publishingPageSize
	}
	if options.Resync {
		if err := store.forgetPublishingWatermark(ctx); err != nil {
			return result, err
		}
	}
	mark, known, err := store.publishingWatermark(ctx)
	if err != nil {
		return result, err
	}
	cursor := registryVersion{UpdatedAt: time.Unix(0, 0).UTC()}
	if known {
		cursor.UpdatedAt = mark.Add(-publishingOverlap)
	}
	result.Full = !known
	for {
		page, err := registry.changedAfter(ctx, cursor, options.PageSize)
		if err != nil {
			return result, err
		}
		if len(page) == 0 {
			return result, nil
		}
		seen := map[string]bool{}
		var names []string
		for _, version := range page {
			if !seen[version.Name] {
				seen[version.Name] = true
				names = append(names, version.Name)
			}
		}
		states, err := registry.states(ctx, names)
		if err != nil {
			return result, err
		}
		cursor = page[len(page)-1]
		if cursor.UpdatedAt.After(mark) {
			mark = cursor.UpdatedAt
		}
		if err := store.savePublishing(ctx, page, states, mark); err != nil {
			return result, err
		}
		result.Changed += len(page)
		result.Pages++
		if len(page) < options.PageSize {
			return result, nil
		}
		timer := time.NewTimer(options.Pause)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
	}
}
