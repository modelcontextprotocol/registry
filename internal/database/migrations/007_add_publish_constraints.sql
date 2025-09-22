-- Add unique constraints to prevent race conditions in concurrent publishing
-- These serve as safety nets alongside advisory locks

-- Drop redundant index (version_id is already the primary key)
DROP INDEX IF EXISTS idx_servers_version_id;

-- Prevent duplicate versions of the same server
-- Ensures (serverId, version) is unique across all server versions
CREATE UNIQUE INDEX idx_unique_server_version
ON servers (
    (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'serverId'),
    (value->>'version')
);

-- Prevent multiple versions marked as latest for the same server
-- Ensures only one version per serverId can have isLatest=true
CREATE UNIQUE INDEX idx_unique_latest_version
ON servers (
    (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'serverId')
)
WHERE (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'isLatest')::boolean = true;