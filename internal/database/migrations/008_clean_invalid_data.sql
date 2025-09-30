-- Migration 008: Clean up invalid data before applying stricter constraints in migration 009
-- This migration removes or fixes data that would violate constraints introduced in the next migration

BEGIN;

-- Log what we're about to clean up for audit purposes
DO $$
DECLARE
    invalid_name_count INTEGER;
    empty_version_count INTEGER;
    invalid_status_count INTEGER;
    duplicate_count INTEGER;
BEGIN
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

    -- Log the cleanup operations
    IF invalid_name_count > 0 OR empty_version_count > 0 THEN
        RAISE NOTICE 'Deleting % servers with invalid names and % servers with empty versions',
            invalid_name_count, empty_version_count;
    END IF;

    IF invalid_status_count > 0 THEN
        RAISE NOTICE 'Fixing % servers with invalid status values (changing to ''active'')',
            invalid_status_count;
    END IF;

    IF duplicate_count > 0 THEN
        RAISE NOTICE 'Found % duplicate name+version combinations to clean up', duplicate_count;
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

-- Log completion
DO $$
DECLARE
    remaining_count INTEGER;
BEGIN
    SELECT COUNT(*) INTO remaining_count FROM servers;
    RAISE NOTICE 'Data cleanup complete. % servers remaining in database.', remaining_count;
END $$;

COMMIT;