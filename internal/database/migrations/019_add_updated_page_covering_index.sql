-- Incremental sync includes deleted versions, so the partial search index
-- cannot cover it. Filter narrow index entries before fetching page payloads.
-- Prebuild CONCURRENTLY on busy databases before deploying this migration.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS idx_servers_updated_page_cover
    ON servers (server_name, version) INCLUDE (updated_at, is_latest, status);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_updated_page_cover'::regclass
          AND indisvalid AND indisready
    ) THEN
        RAISE EXCEPTION 'idx_servers_updated_page_cover is not valid; repair the concurrent build before retrying migration 019';
    END IF;
END
$$;
