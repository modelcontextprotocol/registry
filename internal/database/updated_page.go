package database

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/modelcontextprotocol/registry/internal/telemetry"
)

const (
	updatedPageProbe               = 512
	updatedPageHintLimit           = 32
	updatedPageHintLifetime        = 5 * time.Second
	updatedPageObservationInterval = 16
)

// updatedPageHints remembers query strategies, never response rows. A hint says
// a recent probe found >512 matching rows with minimum timestamp/name boundaries.
// Writes can invalidate that observation immediately: a stale hint may choose a
// slower name-ordered scan, but that scan still applies the complete predicate.
// Expiry bounds this performance risk; the fixed capacity bounds per-app memory.
// The zero value is ready to use. A PostgreSQL instance must not be copied.
type updatedPageHints struct {
	mu       sync.Mutex
	requests uint64
	entries  []updatedPageHint
}

type updatedPageHint struct {
	expires        time.Time
	before         time.Time
	nameBefore     string
	includeDeleted bool
}

type updatedPageStrategy struct {
	observe    bool
	skipProbe  bool
	nameBefore string
}

func (h *updatedPageHints) choose(now, since time.Time, cursorName string, includeDeleted bool) updatedPageStrategy {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.requests++
	strategy := updatedPageStrategy{observe: h.requests%updatedPageObservationInterval == 1}
	for _, hint := range h.entries {
		if hint.includeDeleted != includeDeleted || !now.Before(hint.expires) || !since.Before(hint.before) {
			continue
		}
		// Conservatively screen name boundaries. Production query selection
		// only consults hints for first pages; cursor pages use a direct seek.
		if cursorName != "" && cursorName >= hint.nameBefore {
			continue
		}
		if !strategy.skipProbe || hint.nameBefore > strategy.nameBefore {
			strategy.skipProbe = true
			strategy.nameBefore = hint.nameBefore
		}
	}
	return strategy
}

func (h *updatedPageHints) remember(now time.Time, observation updatedPageObservation, includeDeleted bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Do not refresh an existing hint's lifetime without a fresh probe. A
	// covered sampled query has no probe rows and does not produce a hint.
	live := h.entries[:0]
	for _, hint := range h.entries {
		if now.Before(hint.expires) {
			live = append(live, hint)
		}
	}
	h.entries = live
	for _, hint := range h.entries {
		if hint.includeDeleted == includeDeleted && hint.before.Equal(observation.before) && hint.nameBefore == observation.nameBefore {
			return
		}
	}
	entry := updatedPageHint{expires: now.Add(updatedPageHintLifetime), before: observation.before, nameBefore: observation.nameBefore, includeDeleted: includeDeleted}
	if len(h.entries) < updatedPageHintLimit {
		h.entries = append(h.entries, entry)
		return
	}
	oldest := 0
	for i := range h.entries {
		if h.entries[i].expires.Before(h.entries[oldest].expires) {
			oldest = i
		}
	}
	h.entries[oldest] = entry
}

// updatedPageObservation is returned in one private marker row on sampled
// queries. Empty server names are forbidden by the schema, so the marker cannot
// collide with a stored row. Reusing the row layout avoids widening every page
// row; these fields never reach API serialization or returned-row telemetry.
type updatedPageObservation struct {
	large      bool
	before     time.Time
	nameBefore string
}

func (o *updatedPageObservation) consume(row listingRow) bool {
	if row.name != "" {
		return false
	}
	o.large = row.metadata.IsLatest
	o.before = row.metadata.UpdatedAt
	if row.metadata.StatusMessage != nil {
		o.nameBefore = *row.metadata.StatusMessage
	}
	return true
}

// Cursor pages always retain the name-ordered seek. A timestamp probe can scan
// the entire consumed prefix again on every page, especially with limit=1.
// First-page hints are limited to the plain 100-row shape and never cache data.
func (db *PostgreSQL) chooseUpdatedPageStrategy(tx pgx.Tx, filter *ServerFilter, limit int, cursorArgs []any) updatedPageStrategy {
	if filter != nil && filter.UpdatedSince != nil && filter.SubstringName == nil && len(cursorArgs) > 0 {
		return updatedPageStrategy{skipProbe: true}
	}
	if tx != nil || limit != 100 || !cacheUpdatedPage(filter, limit) {
		return updatedPageStrategy{}
	}
	includeDeleted := filter.IncludeDeleted != nil && *filter.IncludeDeleted
	return db.updatedPageHints.choose(time.Now(), *filter.UpdatedSince, "", includeDeleted)
}

func (db *PostgreSQL) readUpdatedPageRows(ctx context.Context, rows pgx.Rows, diagnostic *telemetry.ListingDiagnostic, prepared bool, strategy updatedPageStrategy, filter *ServerFilter) ([]listingRow, error) {
	var observation *updatedPageObservation
	if strategy.observe {
		observation = &updatedPageObservation{}
	}
	page, err := readListingRows(ctx, rows, diagnostic, prepared, observation)
	if err == nil && observation != nil && observation.large {
		db.updatedPageHints.remember(time.Now(), *observation, filter.IncludeDeleted != nil && *filter.IncludeDeleted)
	}
	return page, err
}

// updatedPageQuery selects keys before fetching payloads. A complete bounded
// timestamp probe is cheap for sparse histories; a name-ordered scan avoids
// sorting a large history. Both branches use one statement snapshot and the full
// predicate. The probe bounds returned keys, not every index entry visited.
func updatedPageQuery(whereClause, limitExpression, payload string, prepared bool, strategy updatedPageStrategy) string {
	probe := 128
	if limitExpression == "100" {
		probe = updatedPageProbe
	}
	covered := "FALSE"
	if strategy.skipProbe {
		covered = "TRUE"
	}
	columns, observation := "", ""
	probeWhere := whereClause
	choice := fmt.Sprintf("count(*) <= %d", probe)
	if strategy.observe || strategy.skipProbe {
		probeWhere += " AND NOT (" + covered + ")"
		choice += " AND NOT (" + covered + ")"
	}
	if strategy.observe {
		columns = ", updated_at"
		observation = fmt.Sprintf(", count(*) > %d AS observed_large", probe)
	}
	ctes := fmt.Sprintf(`WITH recent AS MATERIALIZED (
        SELECT server_name, version%s FROM servers
        %s
        ORDER BY updated_at DESC LIMIT %d
    ), choice AS MATERIALIZED (
        SELECT %s AS small%s FROM recent
    ), candidates AS MATERIALIZED (
        (SELECT server_name, version FROM recent
         WHERE (SELECT small FROM choice)
         ORDER BY server_name, version LIMIT %s)
        UNION ALL
        (SELECT server_name, version FROM servers
         %s AND NOT (SELECT small FROM choice)
         ORDER BY server_name, version LIMIT %s)
    )`, columns, probeWhere, probe+1, choice, observation, limitExpression, whereClause, limitExpression)
	page := fmt.Sprintf(`SELECT s.server_name, s.version, s.status, s.status_changed_at, s.status_message,
        s.published_at, s.updated_at, s.is_latest, %s
        FROM (SELECT server_name, version FROM candidates
              ORDER BY server_name, version LIMIT %s) page
        JOIN servers s USING (server_name, version)`, payload, limitExpression)
	if !strategy.observe {
		return ctes + " " + page + " ORDER BY s.server_name, s.version"
	}
	return ctes + ", response_rows AS (" + page + ") SELECT * FROM response_rows UNION ALL " +
		updatedPageMarker(prepared) + " ORDER BY server_name, version"
}

func updatedPageMarker(prepared bool) string {
	payload := "NULL::jsonb"
	if prepared {
		payload = "NULL::bytea, false"
	}
	return `SELECT ''::text AS server_name, ''::text AS version, ''::text AS status,
        'epoch'::timestamptz AS status_changed_at,
        (SELECT CASE WHEN observed_large THEN (SELECT min(server_name) FROM recent) ELSE ''::text END FROM choice) AS status_message,
        'epoch'::timestamptz AS published_at,
        (SELECT CASE WHEN observed_large THEN (SELECT min(updated_at) FROM recent) ELSE 'epoch'::timestamptz END FROM choice) AS updated_at,
        (SELECT observed_large FROM choice) AS is_latest, ` + payload
}
