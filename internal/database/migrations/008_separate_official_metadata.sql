-- Separate official metadata from JSON to dedicated columns for better performance and consistency
-- This migration transforms the database from JSON-heavy to columnar structure for registry-managed data

BEGIN;

-- Add new columns for official metadata and core identifiers
ALTER TABLE servers ADD COLUMN server_name VARCHAR(255);
ALTER TABLE servers ADD COLUMN version VARCHAR(255);
ALTER TABLE servers ADD COLUMN status VARCHAR(50);
ALTER TABLE servers ADD COLUMN published_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE servers ADD COLUMN updated_at TIMESTAMP WITH TIME ZONE;
ALTER TABLE servers ADD COLUMN is_latest BOOLEAN;

-- Create a function to migrate existing data from JSON to columns
CREATE OR REPLACE FUNCTION migrate_official_metadata()
RETURNS VOID AS $$
DECLARE
    rec RECORD;
    official_meta JSONB;
    updated_value JSONB;
    server_meta JSONB;
    publisher_provided JSONB;
BEGIN
    -- Iterate through all existing records
    FOR rec IN SELECT version_id, value FROM servers ORDER BY version_id LOOP
        -- Extract core identifiers
        UPDATE servers
        SET
            server_name = rec.value->>'name',
            version = rec.value->>'version'
        WHERE version_id = rec.version_id;

        -- Extract official metadata from nested JSON structure
        official_meta := rec.value->'_meta'->'io.modelcontextprotocol.registry/official';

        IF official_meta IS NOT NULL THEN
            -- Update columns with extracted metadata
            UPDATE servers
            SET
                status = (official_meta->>'status')::VARCHAR(50),
                published_at = (official_meta->>'publishedAt')::TIMESTAMP WITH TIME ZONE,
                updated_at = (official_meta->>'updatedAt')::TIMESTAMP WITH TIME ZONE,
                is_latest = (official_meta->>'isLatest')::BOOLEAN
            WHERE version_id = rec.version_id;
        ELSE
            -- Handle records without official metadata (set defaults)
            UPDATE servers
            SET
                status = COALESCE((rec.value->>'status'), 'active'),
                published_at = NOW(),
                updated_at = NOW(),
                is_latest = true
            WHERE version_id = rec.version_id;
        END IF;

        -- Clean up the JSON: remove status field and official metadata
        updated_value := rec.value - 'status';

        -- Reconstruct _meta with only publisher-provided data
        publisher_provided := rec.value->'_meta'->'io.modelcontextprotocol.registry/publisher-provided';

        IF publisher_provided IS NOT NULL THEN
            server_meta := jsonb_build_object('io.modelcontextprotocol.registry/publisher-provided', publisher_provided);
            updated_value := jsonb_set(updated_value, '{_meta}', server_meta);
        ELSE
            -- Remove _meta entirely if no publisher-provided data
            updated_value := updated_value - '_meta';
        END IF;

        -- Update the JSON with cleaned structure (keeping name and version for immutable server.json)
        UPDATE servers
        SET value = updated_value
        WHERE version_id = rec.version_id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Execute the migration
SELECT migrate_official_metadata();

-- Drop the migration function
DROP FUNCTION migrate_official_metadata();

-- Make the new columns NOT NULL now that all records have values
ALTER TABLE servers ALTER COLUMN server_name SET NOT NULL;
ALTER TABLE servers ALTER COLUMN version SET NOT NULL;
ALTER TABLE servers ALTER COLUMN status SET NOT NULL;
ALTER TABLE servers ALTER COLUMN published_at SET NOT NULL;
ALTER TABLE servers ALTER COLUMN is_latest SET NOT NULL;

-- Drop the old primary key constraint
ALTER TABLE servers DROP CONSTRAINT servers_pkey;

-- Create new composite primary key using name + version (natural key)
ALTER TABLE servers ADD CONSTRAINT servers_pkey PRIMARY KEY (server_name, version);

-- Drop the old version_id column since we're using name-based keys now
ALTER TABLE servers DROP COLUMN version_id;

-- Drop old indexes that used JSON paths and UUIDs
DROP INDEX IF EXISTS idx_servers_version_id;
DROP INDEX IF EXISTS idx_servers_name_latest;
DROP INDEX IF EXISTS idx_servers_updated_at;
DROP INDEX IF EXISTS idx_servers_remotes;
DROP INDEX IF EXISTS idx_unique_server_version;
DROP INDEX IF EXISTS idx_unique_latest_version;

-- Create new efficient indexes on the dedicated columns
CREATE INDEX idx_servers_name ON servers (server_name);
CREATE INDEX idx_servers_name_version ON servers (server_name, version);
CREATE INDEX idx_servers_name_latest ON servers (server_name, is_latest) WHERE is_latest = true;
CREATE INDEX idx_servers_status ON servers (status);
CREATE INDEX idx_servers_published_at ON servers (published_at DESC);
CREATE INDEX idx_servers_updated_at ON servers (updated_at DESC) WHERE updated_at IS NOT NULL;

-- Ensure only one version per server can be marked as latest
CREATE UNIQUE INDEX idx_unique_latest_per_server
ON servers (server_name)
WHERE is_latest = true;

-- Create GIN index for remaining JSON queries (remotes, packages, etc.)
CREATE INDEX idx_servers_json_remotes ON servers USING GIN((value->'remotes'));
CREATE INDEX idx_servers_json_packages ON servers USING GIN((value->'packages'));

-- Update $schema field to latest version for all entries
UPDATE servers
SET value = jsonb_set(value, '{$schema}', '"https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json"')
WHERE value ? '$schema' AND value IS NOT NULL;

-- Add check constraints for data integrity
ALTER TABLE servers ADD CONSTRAINT check_status_valid
CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE servers ADD CONSTRAINT check_server_name_format
CHECK (server_name ~ '^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$');

ALTER TABLE servers ADD CONSTRAINT check_version_not_empty
CHECK (length(trim(version)) > 0);

ALTER TABLE servers ADD CONSTRAINT check_published_at_reasonable
CHECK (published_at >= '2020-01-01'::timestamp AND published_at <= NOW() + interval '1 day');

COMMIT;