-- A substring search spanning '/' must end at the namespace boundary.
-- Reversing the namespace makes that suffix an indexable prefix. Keep the
-- original ILIKE predicate as the final check of the complete search term.
--
-- Build this index CONCURRENTLY before deploying to an existing busy database;
-- see deploy/README.md. The migration runner itself uses a transaction.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS idx_servers_namespace_suffix
    ON servers (lower(reverse(split_part(server_name, '/', 1))) text_pattern_ops);

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_namespace_suffix'::regclass
          AND indisvalid AND indisready
    ) THEN
        RAISE EXCEPTION 'idx_servers_namespace_suffix is not valid; repair the concurrent build before retrying migration 016';
    END IF;
END
$$;
