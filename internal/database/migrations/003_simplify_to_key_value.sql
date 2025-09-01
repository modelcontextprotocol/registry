-- Simplify database to simple key-value table with id, value
-- This migration drops all existing data and creates a clean simple structure

-- Drop old tables and functions
DROP TRIGGER IF EXISTS update_server_extensions_on_server_update ON servers;
DROP FUNCTION IF EXISTS update_server_extensions_updated_at();
DROP TRIGGER IF EXISTS update_servers_updated_at ON servers;
DROP FUNCTION IF EXISTS update_updated_at_column();

DROP TABLE IF EXISTS server_extensions;
DROP TABLE IF EXISTS servers;

-- Create new simple servers table
CREATE TABLE servers (
    id VARCHAR(255) PRIMARY KEY, -- Use registry metadata ID as primary key
    value JSONB NOT NULL -- Complete ServerJSON as JSONB
);

-- Create essential indexes for performance
CREATE INDEX idx_servers_id ON servers (id);
CREATE INDEX idx_servers_name_latest ON servers ((value->>'name'), (value->'_meta'->'io.modelcontextprotocol.registry'->>'is_latest'))
    WHERE (value->'_meta'->'io.modelcontextprotocol.registry'->>'is_latest')::boolean = true;
CREATE INDEX idx_servers_updated_at ON servers ((value->'_meta'->'io.modelcontextprotocol.registry'->>'updated_at'));
CREATE INDEX idx_servers_remotes ON servers USING GIN((value->'remotes'));