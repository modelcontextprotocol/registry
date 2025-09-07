-- Update database indexes to use new MCP spec compliant _meta field paths
-- Data has been updated manually, only need to update indexes

-- Drop old indexes that reference the old paths
DROP INDEX IF EXISTS idx_servers_name_latest;
DROP INDEX IF EXISTS idx_servers_updated_at;

-- Create new indexes with updated paths
CREATE INDEX idx_servers_name_latest ON servers ((value->>'name'), (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'is_latest'))
    WHERE (value->'_meta'->'io.modelcontextprotocol.registry/official'->>'is_latest')::boolean = true;
CREATE INDEX idx_servers_updated_at ON servers ((value->'_meta'->'io.modelcontextprotocol.registry/official'->>'updated_at'));