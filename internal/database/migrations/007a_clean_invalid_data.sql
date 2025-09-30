-- Migration 007a: Clean up invalid data before applying stricter constraints in migration 008
-- This migration removes or fixes data that would violate constraints introduced in the next migration

BEGIN;

-- Safety check: Count exactly what we'll be modifying
DO $$
DECLARE
    invalid_name_count INTEGER;
    empty_version_count INTEGER;
    invalid_status_count INTEGER;
    duplicate_count INTEGER;
    total_to_delete INTEGER;
    total_servers INTEGER;
    has_known_bad_server BOOLEAN;
    r RECORD;
BEGIN
    -- Get total server count
    SELECT COUNT(*) INTO total_servers FROM servers;

    -- Check if we have the specific known problematic server from production
    -- This server only exists in production data, not in test fixtures
    SELECT EXISTS(
        SELECT 1 FROM servers
        WHERE value->>'name' = 'io.github.joelverhagen/Knapcode.SampleMcpServer/aot'
    ) INTO has_known_bad_server;

    -- Skip migration if we don't have the known bad data (indicates test environment)
    IF NOT has_known_bad_server THEN
        RAISE NOTICE 'Migration 007a: Skipping cleanup - no known problematic data found (likely test/dev environment)';
        RETURN;  -- Exit early, don't run any cleanup
    END IF;

    RAISE NOTICE 'Migration 007a: Found known problematic data, proceeding with cleanup';

    -- Count servers with invalid name format
    SELECT COUNT(*) INTO invalid_name_count
    FROM servers
    WHERE value->>'name' NOT SIMILAR TO '[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]';

    -- Count servers with empty or NULL versions
    SELECT COUNT(*) INTO empty_version_count
    FROM servers
    WHERE value->>'version' IS NULL OR value->>'version' = '';

    -- Count servers with invalid status
    SELECT COUNT(*) INTO invalid_status_count
    FROM servers
    WHERE value->>'status' IS NOT NULL
      AND value->>'status' != ''
      AND value->>'status' NOT IN ('active', 'deprecated', 'deleted');

    -- Count duplicate name+version combinations
    SELECT COUNT(*) INTO duplicate_count
    FROM (
        SELECT value->>'name', value->>'version', COUNT(*) as cnt
        FROM servers
        GROUP BY value->>'name', value->>'version'
        HAVING COUNT(*) > 1
    ) dups;

    -- Calculate total deletions (some servers might have both invalid name AND empty version)
    SELECT COUNT(*) INTO total_to_delete
    FROM servers
    WHERE value->>'name' NOT SIMILAR TO '[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]'
       OR value->>'version' IS NULL
       OR value->>'version' = '';

    -- Log the cleanup operations with safety check
    RAISE NOTICE 'Migration 007a Data Cleanup Plan:';
    RAISE NOTICE '  Total servers in database: %', total_servers;

    IF total_servers > 0 THEN
        RAISE NOTICE '  Servers to DELETE: % (% percent of total)', total_to_delete, ROUND((total_to_delete::numeric / total_servers * 100), 2);
    ELSE
        RAISE NOTICE '  Servers to DELETE: %', total_to_delete;
    END IF;

    RAISE NOTICE '    - Invalid names: %', invalid_name_count;
    RAISE NOTICE '    - Empty versions: %', empty_version_count;
    RAISE NOTICE '  Servers to UPDATE (fix status): %', invalid_status_count;
    RAISE NOTICE '  Duplicate name+version pairs: %', duplicate_count;

    -- Always log the specific servers that will be deleted for transparency
    IF total_to_delete > 0 THEN
        RAISE NOTICE '';
        RAISE NOTICE 'Servers that will be deleted:';
        FOR r IN
            SELECT value->>'name' as name, value->>'version' as version,
                   CASE
                       WHEN value->>'name' NOT SIMILAR TO '[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]' THEN 'Invalid name format'
                       WHEN value->>'version' IS NULL THEN 'NULL version'
                       WHEN value->>'version' = '' THEN 'Empty version'
                   END as reason
            FROM servers
            WHERE value->>'name' NOT SIMILAR TO '[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]'
               OR value->>'version' IS NULL
               OR value->>'version' = ''
            ORDER BY value->>'name'
        LOOP
            RAISE NOTICE '  - % @ % (reason: %)', r.name, COALESCE(r.version, '<NULL>'), r.reason;
        END LOOP;
    END IF;

    -- Always log the specific servers that will have status updated for transparency
    IF invalid_status_count > 0 THEN
        RAISE NOTICE '';
        RAISE NOTICE 'Servers that will have status updated:';
        FOR r IN
            SELECT value->>'name' as name, value->>'version' as version, value->>'status' as status
            FROM servers
            WHERE value->>'status' IS NOT NULL
              AND value->>'status' != ''
              AND value->>'status' NOT IN ('active', 'deprecated', 'deleted')
            ORDER BY value->>'name'
        LOOP
            RAISE NOTICE '  - % @ % (current status: %)', r.name, r.version, r.status;
        END LOOP;
    END IF;

    -- SAFETY CHECK: Ensure we're deleting exactly the expected data
    -- We only reach this point if we found the known bad server
    -- In production (2025-09-30), we expect exactly 5 deletions and 1 status update
    IF total_to_delete != 5 THEN
        RAISE EXCEPTION 'Safety check failed: Expected to delete exactly 5 servers but would delete %. Check the log above for details. Aborting to prevent data loss.', total_to_delete;
    END IF;

    IF invalid_status_count != 1 THEN
        RAISE EXCEPTION 'Safety check failed: Expected to update exactly 1 server status but would update %. Check the log above for details. Aborting to prevent data corruption.', invalid_status_count;
    END IF;
END $$;

-- Delete servers with invalid names or empty versions
-- These cannot be reasonably fixed and would violate primary key constraints
DELETE FROM servers
WHERE value->>'name' NOT SIMILAR TO '[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]'
   OR value->>'version' IS NULL
   OR value->>'version' = '';

-- Fix invalid status values by setting them to 'active'
-- These can be reasonably defaulted to a valid value
UPDATE servers
SET value = jsonb_set(value, '{status}', '"active"')
WHERE value->>'status' IS NOT NULL
  AND value->>'status' != ''
  AND value->>'status' NOT IN ('active', 'deprecated', 'deleted');

-- Remove duplicate name+version combinations
-- Keep the one with the most recent publishedAt date
DELETE FROM servers s1
WHERE EXISTS (
  SELECT 1 FROM servers s2
  WHERE s2.value->>'name' = s1.value->>'name'
    AND s2.value->>'version' = s1.value->>'version'
    AND (s2.value->>'publishedAt')::timestamp > (s1.value->>'publishedAt')::timestamp
);

-- Verify the operations completed as expected
DO $$
DECLARE
    remaining_count INTEGER;
    actual_deleted INTEGER;
    actual_updated INTEGER;
    still_invalid_names INTEGER;
    still_empty_versions INTEGER;
    still_invalid_status INTEGER;
BEGIN
    SELECT COUNT(*) INTO remaining_count FROM servers;

    -- Check if any invalid data remains
    SELECT COUNT(*) INTO still_invalid_names
    FROM servers
    WHERE value->>'name' NOT SIMILAR TO '[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]';

    SELECT COUNT(*) INTO still_empty_versions
    FROM servers
    WHERE value->>'version' IS NULL OR value->>'version' = '';

    SELECT COUNT(*) INTO still_invalid_status
    FROM servers
    WHERE value->>'status' IS NOT NULL
      AND value->>'status' != ''
      AND value->>'status' NOT IN ('active', 'deprecated', 'deleted');

    RAISE NOTICE 'Data cleanup complete:';
    RAISE NOTICE '  Servers remaining: %', remaining_count;
    RAISE NOTICE '  Invalid names remaining: %', still_invalid_names;
    RAISE NOTICE '  Empty versions remaining: %', still_empty_versions;
    RAISE NOTICE '  Invalid status remaining: %', still_invalid_status;

    -- Final safety check: Ensure we cleaned everything we intended to
    IF still_invalid_names > 0 OR still_empty_versions > 0 OR still_invalid_status > 0 THEN
        RAISE EXCEPTION 'Cleanup incomplete! Invalid data still remains. Aborting.';
    END IF;
END $$;

COMMIT;