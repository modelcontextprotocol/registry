-- Heal servers whose is_latest flag is on a deleted version (or missing entirely)
-- by promoting the highest-semver non-deleted version. Falls back to published_at
-- for non-semver versions and as a tiebreaker for prereleases.
-- See https://github.com/modelcontextprotocol/registry/issues/1081.
--
-- The migration framework wraps each migration in its own transaction, so no explicit
-- BEGIN/COMMIT here.

-- Pre-compute the version to promote per affected server. A temp table is used
-- (rather than CTEs) because the two UPDATEs that follow run as separate statements:
-- the unique partial index idx_unique_latest_per_server is non-deferrable and Postgres
-- checks it row-by-row inside a single UPDATE, so the clear and the set must be split.
CREATE TEMP TABLE _heal_picks ON COMMIT DROP AS
WITH broken AS (
    -- Servers where no non-deleted version is flagged is_latest, but at least one
    -- non-deleted version exists. Catches "is_latest is on a deleted row" and the
    -- defensive case where no row has is_latest at all.
    SELECT server_name
    FROM servers
    GROUP BY server_name
    HAVING COUNT(*) FILTER (WHERE is_latest AND status <> 'deleted') = 0
       AND COUNT(*) FILTER (WHERE status <> 'deleted') > 0
),
parsed AS (
    SELECT s.server_name, s.version, s.published_at,
           (regexp_match(s.version, '^(\d+)\.(\d+)\.(\d+)'))[1]::int AS major,
           (regexp_match(s.version, '^(\d+)\.(\d+)\.(\d+)'))[2]::int AS minor,
           (regexp_match(s.version, '^(\d+)\.(\d+)\.(\d+)'))[3]::int AS patch
    FROM servers s
    JOIN broken b USING (server_name)
    WHERE s.status <> 'deleted'
)
SELECT DISTINCT ON (server_name) server_name, version
FROM parsed
ORDER BY server_name,
         major DESC NULLS LAST,
         minor DESC NULLS LAST,
         patch DESC NULLS LAST,
         published_at DESC;

-- Clear stale is_latest=true on rows of affected servers (typically the deleted row).
UPDATE servers s
SET is_latest = false
WHERE s.server_name IN (SELECT server_name FROM _heal_picks)
  AND s.is_latest;

-- Promote the chosen version.
UPDATE servers s
SET is_latest = true
FROM _heal_picks p
WHERE s.server_name = p.server_name AND s.version = p.version;
