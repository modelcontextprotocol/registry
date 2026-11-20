-- Prebuild both indexes CONCURRENTLY on busy databases before deployment.
-- Startup migrations are transactional; see deploy/README.md for the prebuild.
-- A new name avoids silently reusing 019's included-column timestamp layout.
SET LOCAL lock_timeout = '5s';

DO $$
BEGIN
    -- Avoid even requesting a table write lock when indexes were prebuilt.
    -- CREATE INDEX IF NOT EXISTS takes that lock before checking the name.
    IF to_regclass('idx_servers_updated_name_cover') IS NULL THEN
        CREATE INDEX idx_servers_updated_name_cover
            ON servers (server_name, version, updated_at) INCLUDE (is_latest, status);
    END IF;
    IF to_regclass('idx_servers_updated_time_cover') IS NULL THEN
        CREATE INDEX idx_servers_updated_time_cover
            ON servers (updated_at DESC, server_name, version);
    END IF;

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

-- Retain old indexes during startup. Ordinary DROP INDEX locks the table and
-- can stall readers/writers even after a successful concurrent prebuild.
-- Remove obsolete indexes separately with DROP INDEX CONCURRENTLY after rollout.
