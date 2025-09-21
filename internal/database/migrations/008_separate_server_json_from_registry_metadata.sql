-- Separate immutable server.json from registry metadata
-- This migration restructures the table to have separate columns for registry metadata
-- ensuring server.json remains completely immutable

-- Create new table structure
CREATE TABLE servers_new (
    version_id VARCHAR(255) PRIMARY KEY, -- Registry version ID
    server_id VARCHAR(255) NOT NULL,     -- Registry server ID (consistent across versions)

    -- Registry metadata (moved out of server.json)
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    published_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    is_latest BOOLEAN NOT NULL DEFAULT false,

    -- Immutable server.json (publisher-provided data only, no registry fields)
    server_json JSONB NOT NULL
);

-- Create indexes for the new structure
CREATE INDEX idx_servers_new_version_id ON servers_new (version_id);
CREATE INDEX idx_servers_new_server_id ON servers_new (server_id);
CREATE INDEX idx_servers_new_name ON servers_new ((server_json->>'name'));
CREATE INDEX idx_servers_new_name_version ON servers_new ((server_json->>'name'), (server_json->>'version'));
CREATE INDEX idx_servers_new_latest ON servers_new (is_latest) WHERE is_latest = true;
CREATE INDEX idx_servers_new_status ON servers_new (status);
CREATE INDEX idx_servers_new_updated_at ON servers_new (updated_at);
CREATE INDEX idx_servers_new_remotes ON servers_new USING GIN((server_json->'remotes'));

-- Migrate data from old structure to new structure
INSERT INTO servers_new (
    version_id,
    server_id,
    status,
    published_at,
    updated_at,
    is_latest,
    server_json
)
SELECT
    version_id,
    COALESCE(
        value->'_meta'->'io.modelcontextprotocol.registry/official'->>'serverId',
        version_id  -- fallback to version_id if serverId missing
    ) as server_id,
    COALESCE(
        value->'_meta'->'io.modelcontextprotocol.registry/official'->>'status',
        'active'
    ) as status,
    COALESCE(
        (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'publishedAt')::timestamp with time zone,
        NOW()
    ) as published_at,
    COALESCE(
        (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'updatedAt')::timestamp with time zone,
        NOW()
    ) as updated_at,
    COALESCE(
        (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'isLatest')::boolean,
        false
    ) as is_latest,
    -- Create immutable server.json: remove status + remove official metadata, keep publisher metadata
    CASE
        WHEN value ? '_meta' AND value->'_meta' ? 'io.modelcontextprotocol.registry/publisher-provided' THEN
            -- Keep only publisher-provided metadata
            jsonb_build_object(
                '$schema', value->'$schema',
                'name', value->'name',
                'description', value->'description',
                'repository', value->'repository',
                'version', value->'version',
                'websiteUrl', value->'websiteUrl',
                'packages', value->'packages',
                'remotes', value->'remotes',
                '_meta', jsonb_build_object(
                    'io.modelcontextprotocol.registry/publisher-provided',
                    value->'_meta'->'io.modelcontextprotocol.registry/publisher-provided'
                )
            ) - 'status'
        ELSE
            -- No publisher metadata, just core fields
            jsonb_build_object(
                '$schema', value->'$schema',
                'name', value->'name',
                'description', value->'description',
                'repository', value->'repository',
                'version', value->'version',
                'websiteUrl', value->'websiteUrl',
                'packages', value->'packages',
                'remotes', value->'remotes'
            ) - 'status'
    END as server_json
FROM servers
WHERE value IS NOT NULL;

-- Drop old table and rename new one
DROP TABLE servers;
ALTER TABLE servers_new RENAME TO servers;

-- Remove any null fields from server_json to keep it clean
UPDATE servers
SET server_json = (
    SELECT jsonb_object_agg(key, value)
    FROM jsonb_each(server_json)
    WHERE value IS NOT NULL AND value != 'null'::jsonb
)
WHERE server_json IS NOT NULL;