-- Trigram indexes for the ?search= filter.
--
-- The filter matches with ILIKE '%term%' against server_name and, as of #1453,
-- against the description inside the JSON value. A leading wildcard cannot use a
-- btree index, so every search has been a sequential scan over the table — that
-- was already true for the name alone; adding the description to the same scan
-- does not change how many rows are visited, but it is a good moment to make
-- both sides indexable. See also #1252 on list latency.
--
-- pg_trgm is already enabled by 001_initial_schema.sql, but no trigram index was
-- ever created against it. GIN + gin_trgm_ops is what makes an unanchored ILIKE
-- indexable. It applies to search terms of three characters or more; shorter
-- ones still fall back to a scan, which is the pre-existing behaviour.
--
-- Built non-concurrently on purpose: the migration framework wraps each migration
-- in its own transaction, which forbids CREATE INDEX CONCURRENTLY, and the table
-- is small — the note in postgres.go measured production at ~21K rows on
-- 2026-04-28 — so the exclusive lock is momentary.

CREATE INDEX IF NOT EXISTS idx_servers_server_name_trgm
    ON servers USING GIN (server_name gin_trgm_ops);

CREATE INDEX IF NOT EXISTS idx_servers_description_trgm
    ON servers USING GIN ((value ->> 'description') gin_trgm_ops);
