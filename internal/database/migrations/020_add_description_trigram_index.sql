-- Accelerate substring searches on descriptions. Search matches the term
-- against server names and descriptions; without a trigram index the OR
-- predicate on (value ->> 'description') degrades every search to a heap
-- scan. pg_trgm is installed by migration 001.
--
-- For a busy existing database, build this index CONCURRENTLY before deploying
-- this migration; see deploy/README.md. The migration runner uses transactions,
-- which cannot contain CREATE INDEX CONCURRENTLY. Fresh databases build it here.
SET LOCAL lock_timeout = '5s';

CREATE INDEX IF NOT EXISTS idx_servers_description_trgm
    ON servers USING gin ((value ->> 'description') gin_trgm_ops);

-- A cancelled concurrent build can leave an invalid index. IF NOT EXISTS alone
-- would silently accept it and record the migration as successful.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_index
        WHERE indexrelid = 'idx_servers_description_trgm'::regclass
          AND indisvalid AND indisready
    ) THEN
        RAISE EXCEPTION 'idx_servers_description_trgm is not valid; repair the concurrent build before retrying migration 020';
    END IF;
END
$$;
