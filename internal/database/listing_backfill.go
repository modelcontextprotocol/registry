package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ListingBackfillResult describes one bounded, resumable keyset batch. A row
// changed concurrently is left for a later pass; authoritative data is untouched.
type ListingBackfillResult struct {
	Scanned         int    `json:"scanned"`
	Prepared        int64  `json:"prepared"`
	SkippedIdentity int    `json:"skipped_identity"`
	LastName        string `json:"last_name"`
	LastVersion     string `json:"last_version"`
}

type listingBackfillRow struct {
	name, version string
	value         []byte
}

const prepareListingSQL = `UPDATE servers SET listing_value = $1, listing_codec = $2
 WHERE server_name = $3 AND version = $4 AND value = $5::jsonb`

// BackfillListingJSONBatch requires migration 018. The caller controls the
// connection's statement/lock timeouts and delay between batches. No row lock is
// held while Go normalizes JSON; the conditional update rejects stale snapshots.
func BackfillListingJSONBatch(ctx context.Context, conn *pgx.Conn, afterName, afterVersion string, limit int) (ListingBackfillResult, error) {
	result := ListingBackfillResult{LastName: afterName, LastVersion: afterVersion}
	if limit < 1 || limit > 1000 {
		return result, fmt.Errorf("batch size must be 1..1000")
	}
	rows, err := conn.Query(ctx, `SELECT server_name, version, value FROM servers
 WHERE (server_name, version) > ($1, $2)
 AND (listing_codec IS DISTINCT FROM $3 OR listing_value IS NULL)
 ORDER BY server_name, version LIMIT $4`, afterName, afterVersion, listingCodecVersion, limit)
	if err != nil {
		return result, err
	}
	var page []listingBackfillRow
	for rows.Next() {
		var row listingBackfillRow
		if err := rows.Scan(&row.name, &row.version, &row.value); err != nil {
			rows.Close()
			return result, err
		}
		page = append(page, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return result, err
	}
	batch := &pgx.Batch{}
	for _, row := range page {
		result.Scanned++
		result.LastName, result.LastVersion = row.name, row.version
		server, err := normalizeListingJSON(row.value)
		if err != nil {
			return result, err
		}
		if server.name != row.name || server.version != row.version {
			result.SkippedIdentity++
			continue
		}
		batch.Queue(prepareListingSQL, []byte(server.json), listingCodecVersion, row.name, row.version, row.value)
	}
	if batch.Len() == 0 {
		return result, nil
	}
	pending := conn.SendBatch(ctx, batch)
	defer pending.Close()
	for range batch.Len() {
		tag, err := pending.Exec()
		if err != nil {
			return result, err
		}
		result.Prepared += tag.RowsAffected()
	}
	// SendBatch executes in an implicit transaction. Only claim committed counts
	// once Close confirms the entire batch completed successfully.
	if err := pending.Close(); err != nil {
		return result, err
	}
	return result, nil
}
