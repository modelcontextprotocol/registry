-- Prebuild both indexes CONCURRENTLY on busy databases before deployment.
-- Startup migrations are transactional; see deploy/README.md for the prebuild.
-- A new name avoids silently reusing 019's included-column timestamp layout.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS idx_servers_updated_name_cover
    ON servers (server_name, version, updated_at) INCLUDE (is_latest, status);
CREATE INDEX IF NOT EXISTS idx_servers_updated_time_cover
    ON servers (updated_at DESC, server_name, version);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_updated_name_cover'::regclass
          AND indrelid = 'servers'::regclass
          AND indisvalid AND indisready
          AND indnkeyatts = 3 AND indnatts = 5
          AND indoption::text = '0 0 0'
          AND indexprs IS NULL AND indpred IS NULL
          AND (SELECT am.amname FROM pg_class c JOIN pg_am am ON am.oid=c.relam WHERE c.oid=indexrelid) = 'btree'
          AND pg_get_indexdef(indexrelid, 1, true) = 'server_name'
          AND pg_get_indexdef(indexrelid, 2, true) = 'version'
          AND pg_get_indexdef(indexrelid, 3, true) = 'updated_at'
          AND pg_get_indexdef(indexrelid, 4, true) = 'is_latest'
          AND pg_get_indexdef(indexrelid, 5, true) = 'status'
    ) THEN
        RAISE EXCEPTION 'idx_servers_updated_name_cover must be valid, ready, and have the expected definition; repair the prebuild before retrying migration 020';
    END IF;
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_updated_time_cover'::regclass
          AND indrelid = 'servers'::regclass
          AND indisvalid AND indisready
          AND indnkeyatts = 3 AND indnatts = 3
          AND indoption::text = '3 0 0'
          AND indexprs IS NULL AND indpred IS NULL
          AND (SELECT am.amname FROM pg_class c JOIN pg_am am ON am.oid=c.relam WHERE c.oid=indexrelid) = 'btree'
          AND pg_get_indexdef(indexrelid, 1, true) = 'updated_at'
          AND pg_get_indexdef(indexrelid, 2, true) = 'server_name'
          AND pg_get_indexdef(indexrelid, 3, true) = 'version'
    ) THEN
        RAISE EXCEPTION 'idx_servers_updated_time_cover must be valid, ready, and have the expected definition; repair the prebuild before retrying migration 020';
    END IF;
END
$$;

-- Both replacements are usable before the old indexes are removed. The
-- transaction rolls back on lock timeout, retaining the old indexes on failure.
DROP INDEX IF EXISTS idx_servers_updated_page_cover;
DROP INDEX IF EXISTS idx_servers_updated_at;
