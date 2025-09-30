-- Migration 008: Clean up invalid data before applying stricter constraints in migration 009
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
BEGIN
    -- Get total server count
    SELECT COUNT(*) INTO total_servers FROM servers;

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
    RAISE NOTICE 'Migration 008 Data Cleanup Plan:';
    RAISE NOTICE '  Total servers in database: %', total_servers;

    IF total_servers > 0 THEN
        RAISE NOTICE '  Servers to DELETE: % (%.2f%%)', total_to_delete, (total_to_delete::float / total_servers * 100);
    ELSE
        RAISE NOTICE '  Servers to DELETE: %', total_to_delete;
    END IF;

    RAISE NOTICE '    - Invalid names: %', invalid_name_count;
    RAISE NOTICE '    - Empty versions: %', empty_version_count;
    RAISE NOTICE '  Servers to UPDATE (fix status): %', invalid_status_count;
    RAISE NOTICE '  Duplicate name+version pairs: %', duplicate_count;

    -- SAFETY CHECK: Fail if numbers don't match what we found in production
    -- Based on comprehensive analysis of production data (2025-09-30), we expect:
    -- - 5 servers to delete (1 invalid name + 4 empty versions)
    -- - 1 server status to update
    IF total_to_delete != 5 THEN
        RAISE EXCEPTION 'Safety check failed: Expected to delete exactly 5 servers but would delete %. Aborting to prevent data loss.', total_to_delete;
    END IF;

    IF invalid_status_count != 1 THEN
        RAISE EXCEPTION 'Safety check failed: Expected to update exactly 1 server status but would update %. Aborting to prevent data corruption.', invalid_status_count;
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
-- Keep the one with the highest version_id (most recently added)
DELETE FROM servers s1
WHERE EXISTS (
  SELECT 1 FROM servers s2
  WHERE s2.value->>'name' = s1.value->>'name'
    AND s2.value->>'version' = s1.value->>'version'
    AND s2.version_id > s1.version_id
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