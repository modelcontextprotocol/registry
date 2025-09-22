-- Migration to rename id to version_id and update JSON metadata
-- This implements the server ID consistency changes from GitHub issue #396
-- 
-- Current schema: servers(id VARCHAR, value JSONB)
-- Target schema: servers(version_id VARCHAR, value JSONB)
-- JSON metadata changes: 
--   - rename "id" to "versionId" 
--   - add "serverId" (consistent across versions of same server)

BEGIN;

-- Add the new version_id column
ALTER TABLE servers ADD COLUMN version_id VARCHAR(255);

-- Create a temporary function to:
-- 1. Migrate id -> version_id column
-- 2. Update JSON metadata to add serverId and rename id to versionId
CREATE OR REPLACE FUNCTION migrate_to_server_version_ids()
RETURNS VOID AS $$
DECLARE
    rec RECORD;
    server_name TEXT;
    server_id_map JSONB := '{}';
    new_server_id UUID;
    updated_meta JSONB;
    updated_value JSONB;
BEGIN
    -- Iterate through all records and assign server_id and version_id
    FOR rec IN SELECT id, value FROM servers ORDER BY id LOOP
        -- Extract server name from the JSONB value
        server_name := rec.value->>'name';
        
        -- Check if we already have a server_id for this server name
        IF (server_id_map ? server_name) THEN
            new_server_id := (server_id_map->>server_name)::UUID;
        ELSE
            -- Generate a new server_id for this server name
            new_server_id := uuid_generate_v4();
            server_id_map := jsonb_set(server_id_map, ARRAY[server_name], to_jsonb(new_server_id::TEXT));
        END IF;
        
        -- Update the JSON metadata
        updated_meta := rec.value->'_meta'->'io.modelcontextprotocol.registry/official';
        
        -- Add serverId and rename id to versionId
        updated_meta := updated_meta || jsonb_build_object(
            'serverId', new_server_id::TEXT,
            'versionId', updated_meta->>'id'
        );
        
        -- Remove the old 'id' field
        updated_meta := updated_meta - 'id';
        
        -- Update the full value with new metadata
        updated_value := jsonb_set(
            rec.value,
            '{_meta,io.modelcontextprotocol.registry/official}',
            updated_meta
        );
        
        -- Update the record with version_id and updated JSON
        UPDATE servers 
        SET 
            version_id = rec.id,
            value = updated_value
        WHERE id = rec.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Execute the migration function
SELECT migrate_to_server_version_ids();

-- Drop the temporary function
DROP FUNCTION migrate_to_server_version_ids();

-- Make version_id NOT NULL now that all records have values
ALTER TABLE servers ALTER COLUMN version_id SET NOT NULL;

-- Drop the old id column (replaced by version_id)
ALTER TABLE servers DROP COLUMN id;

-- Make version_id the new primary key
ALTER TABLE servers ADD CONSTRAINT servers_pkey PRIMARY KEY (version_id);

-- Update existing indexes to use version_id instead of id
DROP INDEX IF EXISTS idx_servers_id;
CREATE INDEX idx_servers_version_id ON servers(version_id);

COMMIT;