-- Search pages select keys before fetching full records. Cover common filters
-- so broad ordered searches can reject names without reading their heap rows.
-- Prebuild CONCURRENTLY on busy databases before deploying this migration.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS idx_servers_search_cover
    ON servers (server_name, version) INCLUDE (is_latest, updated_at)
    WHERE status != 'deleted';

CREATE INDEX IF NOT EXISTS idx_servers_search_namespace_cover
    ON servers (lower(reverse(split_part(server_name, '/', 1))) text_pattern_ops)
    INCLUDE (server_name, version, is_latest, updated_at)
    WHERE status != 'deleted';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_search_cover'::regclass
          AND indisvalid AND indisready
    ) THEN
        RAISE EXCEPTION 'idx_servers_search_cover is not valid; repair the concurrent build before retrying migration 017';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_search_namespace_cover'::regclass
          AND indisvalid AND indisready
    ) THEN
        RAISE EXCEPTION 'idx_servers_search_namespace_cover is not valid; repair the concurrent build before retrying migration 017';
    END IF;
END
$$;
